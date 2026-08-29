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
	id := r.PathValue("id")

	// A browser sends the token only on the URL we handed it. Every asset
	// vnc.html then pulls — its stylesheet, its JavaScript modules, its icons —
	// is a relative URL with no token on it, so all of them used to 401. The
	// page loaded, its code did not, and noVNC rendered as unstyled text under
	// "noVNC encountered an error". That is the whole bug: the desktop was
	// never broken, its client just never got its own source.
	//
	// So the token is accepted from the URL as before and, when it is valid,
	// echoed back as a cookie scoped to this instance's proxy path. Subsequent
	// asset requests carry it automatically. Nothing is made public: assets
	// still require a valid credential, and the cookie is confined to
	// /vnc/{id}/ so it cannot authenticate anything else.
	claims, err := s.parseToken(vncTokenFrom(r, id))
	if err != nil {
		fail(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if q := r.URL.Query().Get("token"); q != "" {
		setVNCCookie(w, id, q)
	}
	if !roleAllows(claims.Role, roleAny) {
		fail(w, http.StatusForbidden, "insufficient role")
		return
	}

	inst, err := s.db.Instance(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}

	// This route is registered outside requireAuth — a browser loading noVNC
	// cannot set headers — so the caller's org access is resolved here rather
	// than read off the context.
	acc, err := s.accessForClaims(r.Context(), claims)
	if err != nil {
		failErr(w, err)
		return
	}
	if !acc.Can(protocol.PermView, inst.OrgIDs, inst.ID) {
		// 404, not 403: confirming a bot exists but is not yours is itself a
		// disclosure, and a desktop URL is easy to guess at.
		fail(w, http.StatusNotFound, "no such instance")
		return
	}

	// Watching and driving are different permissions. Enforced by which SERVER
	// the connection is spliced to — the sandbox runs a second, `-viewonly`
	// x11vnc for exactly this. noVNC's own `view_only` parameter is
	// client-side and survives only until someone edits the URL, so it would
	// not make the claim in docs/SECURITY.md true.
	readOnly := !acc.Can(protocol.PermDesktop, inst.OrgIDs, inst.ID)
	if inst.State != protocol.InstanceRunning {
		fail(w, http.StatusConflict, "instance is "+string(inst.State))
		return
	}
	upstream := inst.VNCURL
	if readOnly {
		if inst.VNCViewURL == "" {
			// Fail closed. An instance provisioned before the view-only server
			// existed has no read-only endpoint, and silently falling back to
			// the interactive one would hand an auditor a keyboard.
			fail(w, http.StatusConflict,
				"this instance predates the read-only desktop; recreate it to grant auditor access")
			return
		}
		upstream = inst.VNCViewURL
	}

	target, err := url.Parse(upstream)
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

// vncCookieName is per instance so a session for one desktop cannot be
// replayed against another.
func vncCookieName(instanceID string) string {
	return "af_vnc_" + strings.ReplaceAll(instanceID, "-", "")
}

// vncTokenFrom takes the token from the URL, an Authorization header, or the
// cookie set when the page was served — in that order, so an explicit token
// always wins over a stale cookie.
func vncTokenFrom(r *http.Request, instanceID string) string {
	if t := tokenFrom(r); t != "" {
		return t
	}
	if c, err := r.Cookie(vncCookieName(instanceID)); err == nil {
		return c.Value
	}
	return ""
}

func setVNCCookie(w http.ResponseWriter, instanceID, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:  vncCookieName(instanceID),
		Value: token,
		// Scoped to this desktop only.
		Path:     "/vnc/" + instanceID + "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Session cookie: it should not outlive the browser, and the JWT it
		// carries has its own expiry regardless.
	})
}
