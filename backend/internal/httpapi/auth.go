package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Role gates. Auditors read; operators drive; admins configure.
const (
	roleAny      = "any"
	roleOperator = "operator"
	roleAdmin    = "admin"
)

type ctxKey int

const userKey ctxKey = 1

type claims struct {
	Role  string `json:"role"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func (s *Server) issueToken(u *protocol.User) (string, time.Time, error) {
	exp := time.Now().Add(s.cfg.TokenTTL)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role:  string(u.Role),
		Email: u.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "agentfleet",
		},
	})
	signed, err := tok.SignedString(s.cfg.JWTSecret)
	return signed, exp, err
}

func (s *Server) parseToken(raw string) (*claims, error) {
	tok, err := jwt.ParseWithClaims(raw, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.cfg.JWTSecret, nil
	}, jwt.WithIssuer("agentfleet"), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	c, ok := tok.Claims.(*claims)
	if !ok || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

// tokenFrom pulls the bearer token from the header, or from ?token= for the
// WebSocket and desktop-proxy endpoints where headers are not available.
func tokenFrom(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return r.URL.Query().Get("token")
}

func (s *Server) requireAuth(minRole string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := s.parseToken(tokenFrom(r))
		if err != nil {
			fail(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !roleAllows(c.Role, minRole) {
			fail(w, http.StatusForbidden, "your role ("+c.Role+") cannot perform this action")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, c)))
	})
}

func roleAllows(have, need string) bool {
	rank := map[string]int{
		string(protocol.RoleAuditor):  1,
		string(protocol.RoleOperator): 2,
		string(protocol.RoleAdmin):    3,
	}
	// Fail closed on an unrecognised gate. Without the comma-ok this yields 0,
	// and `rank[have] >= 0` is true for every caller including one with a
	// garbage role — so a typo in a route's gate name would silently open it to
	// anyone. Not reachable today, since Routes() only passes the constants
	// above, but the cost of getting it wrong is total.
	needed, ok := map[string]int{roleAny: 1, roleOperator: 2, roleAdmin: 3}[need]
	if !ok {
		return false
	}
	return rank[have] >= needed
}

func userFrom(ctx context.Context) *claims {
	c, _ := ctx.Value(userKey).(*claims)
	return c
}

// ---------------------------------------------------------------- endpoints ---

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	u, hash, err := s.db.UserByEmail(r.Context(), req.Email)
	if err != nil {
		// Same response either way: do not leak which accounts exist.
		fail(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		fail(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, exp, err := s.issueToken(u)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token, "expires_at": exp, "user": u,
	})
}

// handleBootstrap creates the first admin. It is a no-op once any user exists,
// so leaving it routed is not a standing hole.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	n, err := s.db.CountUsers(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	if n > 0 {
		fail(w, http.StatusConflict, "this deployment is already initialised")
		return
	}
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Password) < 12 {
		fail(w, http.StatusBadRequest, "the first admin password must be at least 12 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failErr(w, err)
		return
	}
	u, err := s.db.CreateUser(r.Context(), req.Email, string(hash), protocol.RoleAdmin)
	if err != nil {
		failErr(w, err)
		return
	}
	token, exp, err := s.issueToken(u)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "expires_at": exp, "user": u})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := userFrom(r.Context())
	u, err := s.db.UserByID(r.Context(), c.Subject)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListUsers(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Password) < 12 {
		fail(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	role := protocol.Role(req.Role)
	if role != protocol.RoleAdmin && role != protocol.RoleOperator && role != protocol.RoleAuditor {
		fail(w, http.StatusBadRequest, "role must be admin, operator or auditor")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		failErr(w, err)
		return
	}
	u, err := s.db.CreateUser(r.Context(), req.Email, string(hash), role)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleSetRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.SetUserRole(r.Context(), r.PathValue("id"), protocol.Role(req.Role)); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EnsureBootstrapUser seeds an admin from the environment on first boot, so an
// unattended deployment is usable without a manual bootstrap call.
func EnsureBootstrapUser(ctx context.Context, db *store.Store, email, password string) (bool, error) {
	if email == "" || password == "" {
		return false, nil
	}
	n, err := db.CountUsers(ctx)
	if err != nil || n > 0 {
		return false, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	_, err = db.CreateUser(ctx, email, string(hash), protocol.RoleAdmin)
	return err == nil, err
}
