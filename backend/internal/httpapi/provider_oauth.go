package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
)

// Signing in to a provider with a Google account.
//
// The operator starts a sign-in, is shown a short code and a URL, approves on
// whatever device is convenient, and the server holds a refresh token
// afterwards. The token never reaches the client — the app only ever learns
// whether a sign-in succeeded.

// pendingSignIn is a sign-in waiting for approval.
type pendingSignIn struct {
	deviceCode   string
	clientID     string
	clientSecret string
	endpoints    connectors.DeviceEndpoints
	interval     int
	expiresAt    time.Time
}

// Held in memory, not the database: a half-finished sign-in is worth nothing
// after a restart, and the device code is a bearer credential for the few
// minutes it lives.
var (
	signInMu sync.Mutex
	signIns  = map[string]pendingSignIn{}
)

type startSignInReq struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
}

func (s *Server) handleStartProviderSignIn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.db.Provider(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}

	var req startSignInReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Re-signing in to a provider that already has a client should not require
	// typing the client ID again.
	if req.ClientID == "" {
		req.ClientID = p.OAuthClientID
	}
	if req.ClientID == "" {
		fail(w, http.StatusBadRequest, "an OAuth client ID is required")
		return
	}

	endpoints := connectors.DeviceEndpoints{
		DeviceURL: p.OAuthDeviceURL,
		TokenURL:  p.OAuthTokenURL,
		Scope:     firstNonEmptyStr(req.Scope, p.OAuthScope),
	}
	auth, err := connectors.StartDeviceAuthAt(r.Context(), nil, endpoints, req.ClientID)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}

	signInMu.Lock()
	signIns[id] = pendingSignIn{
		deviceCode:   auth.DeviceCode,
		clientID:     req.ClientID,
		clientSecret: req.ClientSecret,
		endpoints:    endpoints,
		interval:     auth.Interval,
		expiresAt:    time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second),
	}
	signInMu.Unlock()

	// The device code is deliberately absent: the client needs the user code to
	// display and nothing else.
	writeJSON(w, http.StatusOK, auth)
}

// handleProviderSignInStatus is polled while the operator approves.
func (s *Server) handleProviderSignInStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	signInMu.Lock()
	pending, ok := signIns[id]
	signInMu.Unlock()
	if !ok {
		fail(w, http.StatusNotFound, "no sign-in is in progress")
		return
	}
	if time.Now().After(pending.expiresAt) {
		signInMu.Lock()
		delete(signIns, id)
		signInMu.Unlock()
		fail(w, http.StatusGone, "the sign-in code expired; start again")
		return
	}

	refresh, err := connectors.PollDeviceAuthAt(r.Context(), nil, pending.endpoints,
		pending.clientID, pending.clientSecret, pending.deviceCode)
	switch {
	case errors.Is(err, connectors.ErrAuthPending):
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "pending", "interval": pending.interval,
		})
		return
	case errors.Is(err, connectors.ErrAuthDeclined):
		signInMu.Lock()
		delete(signIns, id)
		signInMu.Unlock()
		fail(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		fail(w, http.StatusBadGateway, err.Error())
		return
	}

	p, err := s.db.Provider(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}

	blob, err := connectors.EncodeGoogleCredentials(connectors.GoogleCredentials{
		RefreshToken: refresh,
		ClientSecret: pending.clientSecret,
	})
	if err != nil {
		failErr(w, err)
		return
	}

	ref := p.OAuthTokenRef
	if ref == "" {
		ref = "provider/" + slug(p.Name) + "/oauth"
	}
	if err := s.vault.Put(r.Context(), ref, blob, "Google sign-in for "+p.Name); err != nil {
		failErr(w, err)
		return
	}

	p.AuthMode = "oauth"
	p.OAuthClientID = pending.clientID
	p.OAuthTokenRef = ref
	if err := s.db.UpsertProvider(r.Context(), p); err != nil {
		failErr(w, err)
		return
	}

	signInMu.Lock()
	delete(signIns, id)
	signInMu.Unlock()

	p.SignedIn = true
	writeJSON(w, http.StatusOK, map[string]any{"status": "signed_in", "provider": p})
}

// handleProviderSignOut forgets a stored sign-in.
//
// The provider is switched back to key authentication rather than left in a
// signed-out OAuth state, which would fail every request while looking
// configured.
func (s *Server) handleProviderSignOut(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.db.Provider(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}

	if p.OAuthTokenRef != "" {
		if err := s.vault.Delete(r.Context(), p.OAuthTokenRef); err != nil {
			failErr(w, err)
			return
		}
	}
	p.AuthMode = "api_key"
	p.OAuthTokenRef = ""
	if err := s.db.UpsertProvider(r.Context(), p); err != nil {
		failErr(w, err)
		return
	}

	signInMu.Lock()
	delete(signIns, id)
	signInMu.Unlock()

	p.SignedIn = false
	writeJSON(w, http.StatusOK, p)
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
