package httpapi

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Long-lived access keys, so a script or a CI job can call this API without
// being handed somebody's password.

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.db.ListAPIKeys(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		// UserID is whose authority the key acts with. Empty means the
		// administrator creating it, which is the common case.
		UserID string `json:"user_id"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		// Named on purpose: an unnamed key is one nobody dares revoke, because
		// nothing says what would break.
		fail(w, http.StatusBadRequest, "a key needs a name saying what it is for")
		return
	}

	me := userFrom(r.Context())
	owner := req.UserID
	if owner == "" {
		owner = me.Subject
	}
	// A key acts with its owner's role, so issuing one against another account
	// must not be a way to mint authority that account does not have. It is
	// still an admin-only route; this stops a key outliving a demotion.
	u, err := s.db.UserByID(r.Context(), owner)
	if err != nil {
		fail(w, http.StatusBadRequest, "no such user")
		return
	}
	if u.Disabled() {
		fail(w, http.StatusBadRequest, "that account is disabled")
		return
	}

	key, err := s.db.NewAPIKey(r.Context(), req.Name, owner, me.Subject)
	if err != nil {
		failErr(w, err)
		return
	}
	// The only time the secret exists outside the caller's hands.
	writeJSON(w, http.StatusCreated, key)
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := s.db.RevokeAPIKey(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ users ---

func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Password) < 12 {
		fail(w, http.StatusBadRequest, "a password must be at least 12 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.db.SetUserPassword(r.Context(), r.PathValue("id"), string(hash)); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetUserDisabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	// Locking yourself out of your own deployment is not a thing to let
	// someone do by tapping the wrong row.
	if id == userFrom(r.Context()).Subject && req.Disabled {
		fail(w, http.StatusBadRequest, "you cannot disable your own account")
		return
	}
	if err := s.db.SetUserDisabled(r.Context(), id, req.Disabled); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
