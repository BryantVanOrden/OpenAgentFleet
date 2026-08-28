package httpapi

import (
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// handleVNCProxy fronts the sandbox's noVNC server so the desktop stream is
// reachable without publishing container ports, and so it inherits the platform's
// authentication instead of noVNC's own password prompt.
//
// Two paths matter: ordinary HTTP for the noVNC assets, and a WebSocket upgrade
// for the RFB stream itself, which needs a raw byte tunnel rather than a
// buffering proxy.
func (s *Server) handleVNCProxy(w http.ResponseWriter, r *http.Request) {
	claims, err := s.parseToken(tokenFrom(r))
	if err != nil {
		fail(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// Auditors may watch; only operators and admins get input.
	if !roleAllows(claims.Role, roleAny) {
		fail(w, http.StatusForbidden, "insufficient role")
		return
	}

	id := r.PathValue("id")
	inst, err := s.db.Instance(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}
	if inst.State != protocol.InstanceRunning {
		fail(w, http.StatusConflict, "instance is "+string(inst.State))
		return
	}
	target, err := url.Parse(inst.VNCURL)
	if err != nil || target.Host == "" {
		fail(w, http.StatusBadGateway, "instance has no desktop endpoint")
		return
	}

	// Strip the /vnc/{id} prefix; noVNC serves from the container root.
	prefix := "/vnc/" + id
	upstreamPath := strings.TrimPrefix(r.URL.Path, prefix)
	if upstreamPath == "" {
		upstreamPath = "/"
	}

	if isWebSocketUpgrade(r) {
		s.tunnelWebSocket(w, r, target.Host, upstreamPath)
		return
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.URL.Path = upstreamPath
			pr.Out.Host = target.Host
			// The token belongs to us, not to the sandbox.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.log.Warn("desktop proxy error", "instance", id, "err", err)
			fail(w, http.StatusBadGateway, "desktop unreachable: "+err.Error())
		},
		FlushInterval: 100 * time.Millisecond,
	}
	proxy.ServeHTTP(w, r)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// tunnelWebSocket hijacks the client connection and splices it to the sandbox,
// forwarding the handshake verbatim. Doing it at the byte level keeps noVNC's
// subprotocol negotiation (binary vs base64) intact.
func (s *Server) tunnelWebSocket(w http.ResponseWriter, r *http.Request, upstreamHost, path string) {
	upstream, err := net.DialTimeout("tcp", upstreamHost, 10*time.Second)
	if err != nil {
		fail(w, http.StatusBadGateway, "desktop unreachable: "+err.Error())
		return
	}
	defer upstream.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming is not supported by this server")
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer client.Close()

	// Replay the handshake upstream with the platform's own auth stripped.
	out := r.Clone(r.Context())
	out.URL.Path = path
	out.Host = upstreamHost
	out.Header.Del("Authorization")
	out.Header.Del("Cookie")
	q := out.URL.Query()
	q.Del("token")
	out.URL.RawQuery = q.Encode()

	if err := out.Write(upstream); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, buffered)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}
