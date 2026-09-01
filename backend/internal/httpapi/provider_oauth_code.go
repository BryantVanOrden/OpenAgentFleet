package httpapi

import (
	"encoding/json"
	"html"
	"net/http"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
)

// Signing in without leaving the app.
//
// The app opens the provider's own consent page in a webview, the provider
// redirects back to this server, and the code on that redirect is exchanged
// here. The app never holds a client secret, a code, or a token — it displays a
// page and is told at the end whether it worked.

type pendingAuthCode struct {
	providerID   string
	clientID     string
	clientSecret string
	redirectURI  string
	endpoints    connectors.AuthCodeEndpoints
	pkce         connectors.PKCE
	expiresAt    time.Time

	// done is closed once the callback has been handled, so the app's poll can
	// report the outcome rather than guessing from a redirect it cannot read.
	done bool
	err  string
}

var (
	authCodeMu sync.Mutex
	authCodes  = map[string]*pendingAuthCode{} // keyed by state
)

// oauthCallbackPath is where providers redirect back to. It is registered with
// the provider as-is, so it must not carry the provider ID: a redirect URI has
// to match exactly, and one URI for every connection means registering it once.
const oauthCallbackPath = "/api/providers/oauth/callback"

// handleOAuthRedirectURI reports the exact URI providers must be told to
// redirect to.
//
// The app could build this from its own server address, but a redirect URI has
// to match what the server actually sends character for character, and a
// mismatch is the single most common way an OAuth setup fails. Better to state
// it than to have two places derive it and hope they agree.
func (s *Server) handleOAuthRedirectURI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"redirect_uri": s.cfg.PublicURL + oauthCallbackPath,
	})
}

type startAuthCodeReq struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
	AuthURL      string `json:"auth_url"`
	TokenURL     string `json:"token_url"`
	// RedirectURI overrides the server's own callback, for a provider that
	// insists on a URI you cannot host here.
	RedirectURI string `json:"redirect_uri"`
}

// handleStartAuthCodeSignIn returns the consent URL for the app to load.
func (s *Server) handleStartAuthCodeSignIn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.db.Provider(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}

	var req startAuthCodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ClientID == "" {
		req.ClientID = p.OAuthClientID
	}
	if req.ClientID == "" {
		fail(w, http.StatusBadRequest, "an OAuth client ID is required")
		return
	}

	pkce, err := connectors.NewPKCE()
	if err != nil {
		failErr(w, err)
		return
	}
	state, err := connectors.NewState()
	if err != nil {
		failErr(w, err)
		return
	}

	redirect := req.RedirectURI
	if redirect == "" {
		redirect = s.cfg.PublicURL + oauthCallbackPath
	}
	endpoints := connectors.AuthCodeEndpoints{
		AuthURL:  firstNonEmptyStr(req.AuthURL, p.OAuthAuthURL),
		TokenURL: firstNonEmptyStr(req.TokenURL, p.OAuthTokenURL),
		Scope:    firstNonEmptyStr(req.Scope, p.OAuthScope),
	}

	authCodeMu.Lock()
	// A sign-in left half-finished should not pin memory forever.
	for k, v := range authCodes {
		if time.Now().After(v.expiresAt) {
			delete(authCodes, k)
		}
	}
	authCodes[state] = &pendingAuthCode{
		providerID:   id,
		clientID:     req.ClientID,
		clientSecret: req.ClientSecret,
		redirectURI:  redirect,
		endpoints:    endpoints,
		pkce:         pkce,
		expiresAt:    time.Now().Add(15 * time.Minute),
	}
	authCodeMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"authorize_url": connectors.AuthCodeURL(endpoints, req.ClientID, redirect, state, pkce),
		"state":         state,
		"redirect_uri":  redirect,
	})
}

// handleOAuthCallback receives the provider's redirect.
//
// Unauthenticated on purpose: the browser or webview arriving here carries the
// provider's redirect, not the operator's session. The state parameter is what
// authorises it — it is unguessable, single-use, and only ever issued to a
// signed-in operator who started this sign-in.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")

	authCodeMu.Lock()
	pending, ok := authCodes[state]
	authCodeMu.Unlock()
	if !ok {
		oauthResultPage(w, false, "This sign-in link is not one this server started, or it has expired.")
		return
	}
	if time.Now().After(pending.expiresAt) {
		authCodeMu.Lock()
		delete(authCodes, state)
		authCodeMu.Unlock()
		oauthResultPage(w, false, "The sign-in took too long. Start it again.")
		return
	}

	if e := r.URL.Query().Get("error"); e != "" {
		s.finishAuthCode(state, pending, "", "the sign-in was declined: "+e)
		oauthResultPage(w, false, "The sign-in was declined.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		s.finishAuthCode(state, pending, "", "the provider returned no code")
		oauthResultPage(w, false, "The provider did not return a sign-in code.")
		return
	}

	refresh, err := connectors.ExchangeCode(r.Context(), nil, pending.endpoints,
		pending.clientID, pending.clientSecret, pending.redirectURI, code, pending.pkce)
	if err != nil {
		s.finishAuthCode(state, pending, "", err.Error())
		oauthResultPage(w, false, err.Error())
		return
	}

	if err := s.storeSignIn(r, pending, refresh); err != nil {
		s.finishAuthCode(state, pending, "", err.Error())
		oauthResultPage(w, false, err.Error())
		return
	}
	s.finishAuthCode(state, pending, refresh, "")
	oauthResultPage(w, true, "")
}

// storeSignIn seals the credential and switches the provider to it.
func (s *Server) storeSignIn(r *http.Request, pending *pendingAuthCode, refresh string) error {
	p, err := s.db.Provider(r.Context(), pending.providerID)
	if err != nil {
		return err
	}
	blob, err := connectors.EncodeGoogleCredentials(connectors.GoogleCredentials{
		RefreshToken: refresh,
		ClientSecret: pending.clientSecret,
	})
	if err != nil {
		return err
	}
	ref := p.OAuthTokenRef
	if ref == "" {
		ref = "provider/" + slug(p.Name) + "/oauth"
	}
	if err := s.vault.Put(r.Context(), ref, blob, "Sign-in for "+p.Name); err != nil {
		return err
	}

	p.AuthMode = "oauth"
	p.OAuthClientID = pending.clientID
	p.OAuthTokenRef = ref
	p.OAuthAuthURL = pending.endpoints.AuthURL
	p.OAuthTokenURL = pending.endpoints.TokenURL
	p.OAuthScope = pending.endpoints.Scope
	return s.db.UpsertProvider(r.Context(), p)
}

func (s *Server) finishAuthCode(state string, pending *pendingAuthCode, _ string, errMsg string) {
	authCodeMu.Lock()
	defer authCodeMu.Unlock()
	pending.done = true
	pending.err = errMsg
	// The verifier is single-use; keeping it after the exchange only widens
	// what a leak of this map would give up.
	pending.pkce = connectors.PKCE{}
	pending.clientSecret = ""
}

// handleAuthCodeStatus is polled by the app while the webview is open.
func (s *Server) handleAuthCodeStatus(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")

	authCodeMu.Lock()
	pending, ok := authCodes[state]
	var done bool
	var errMsg string
	if ok {
		done, errMsg = pending.done, pending.err
	}
	authCodeMu.Unlock()

	if !ok {
		fail(w, http.StatusNotFound, "no sign-in is in progress")
		return
	}
	switch {
	case !done:
		writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
	case errMsg != "":
		authCodeMu.Lock()
		delete(authCodes, state)
		authCodeMu.Unlock()
		fail(w, http.StatusBadGateway, errMsg)
	default:
		authCodeMu.Lock()
		delete(authCodes, state)
		authCodeMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"status": "signed_in"})
	}
}

// oauthResultPage is what the webview shows when the redirect lands. It also
// sets a marker the app can detect without reading the URL's query string.
func oauthResultPage(w http.ResponseWriter, ok bool, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-AgentFleet-SignIn", map[bool]string{true: "ok", false: "failed"}[ok])
	status := http.StatusOK
	if !ok {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)

	title, body, colour := "Signed in", "You can close this and go back to AgentFleet.", "#2e7d32"
	if !ok {
		title, body, colour = "Sign-in failed", detail, "#c62828"
	}
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + title + `</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;
background:#0f1115;color:#e6e8ee;font:16px/1.5 system-ui,sans-serif;text-align:center}
.c{padding:32px;max-width:34ch}h1{font-size:19px;margin:0 0 8px;color:` + colour + `}
p{color:#8b93a7;font-size:13px;margin:0}</style>
<div class="c"><h1>` + title + `</h1><p>` + html.EscapeString(body) + `</p></div>`))
}
