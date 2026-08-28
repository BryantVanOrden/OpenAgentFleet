package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
	"github.com/golang-jwt/jwt/v5"
)

func testServer(secret string, ttl time.Duration) *Server {
	return &Server{cfg: &config.Config{
		JWTSecret: []byte(secret),
		TokenTTL:  ttl,
	}}
}

// -------------------------------------------------------------- roleAllows ---

func TestRoleAllows(t *testing.T) {
	cases := []struct {
		have string
		need string
		want bool
	}{
		// Auditors read and nothing else.
		{string(protocol.RoleAuditor), roleAny, true},
		{string(protocol.RoleAuditor), roleOperator, false},
		{string(protocol.RoleAuditor), roleAdmin, false},

		// Operators drive instances but do not configure the deployment.
		{string(protocol.RoleOperator), roleAny, true},
		{string(protocol.RoleOperator), roleOperator, true},
		{string(protocol.RoleOperator), roleAdmin, false},

		// Admins do everything.
		{string(protocol.RoleAdmin), roleAny, true},
		{string(protocol.RoleAdmin), roleOperator, true},
		{string(protocol.RoleAdmin), roleAdmin, true},

		// Anything unrecognised ranks zero and is denied everything, including
		// the read-only gate.
		{"", roleAny, false},
		{"", roleOperator, false},
		{"", roleAdmin, false},
		{"root", roleAny, false},
		{"superuser", roleAdmin, false},
		{"Admin", roleAny, false},    // case-sensitive on purpose
		{"admin ", roleAdmin, false}, // no trimming
		{"viewer", roleAny, false},
	}

	for _, tc := range cases {
		t.Run(tc.have+"/"+tc.need, func(t *testing.T) {
			if got := roleAllows(tc.have, tc.need); got != tc.want {
				t.Errorf("roleAllows(%q, %q) = %v, want %v", tc.have, tc.need, got, tc.want)
			}
		})
	}
}

// TestRoleAllowsUnknownGateFailsClosed pins the safe half of the sharp edge this
// test used to document. An unrecognised gate name no longer resolves to rank 0
// — it is refused outright, for every role including admin. That matters because
// the failure it guards against is a typo in a route's gate name, which would
// otherwise silently open that route to anyone with any role at all.
func TestRoleAllowsUnknownGateFailsClosed(t *testing.T) {
	for _, role := range []string{
		string(protocol.RoleAuditor), string(protocol.RoleOperator),
		string(protocol.RoleAdmin), "nobody", "",
	} {
		if roleAllows(role, "typo-gate") {
			t.Errorf("roleAllows(%q, \"typo-gate\") = true; an unknown gate must be refused", role)
		}
	}
}

// TestKnownGatesAreRanked is the guard that makes the fail-open above harmless:
// each of the three gate constants requireAuth is actually called with must
// carry a rank above zero.
func TestKnownGatesAreRanked(t *testing.T) {
	for _, gate := range []string{roleAny, roleOperator, roleAdmin} {
		if roleAllows("", gate) {
			t.Errorf("gate %q is unranked: an empty role cleared it", gate)
		}
	}
}

// ------------------------------------------------------------------ tokens ---

func TestIssueAndParseToken(t *testing.T) {
	s := testServer("test-secret", time.Hour)
	u := &protocol.User{ID: "user-123", Email: "ops@example.com", Role: protocol.RoleOperator}

	tok, exp, err := s.issueToken(u)
	if err != nil {
		t.Fatalf("issueToken() error = %v", err)
	}
	if tok == "" {
		t.Fatal("issueToken() returned an empty token")
	}
	if d := time.Until(exp); d < 55*time.Minute || d > time.Hour+time.Minute {
		t.Errorf("expiry = %v from now, want about an hour", d)
	}

	c, err := s.parseToken(tok)
	if err != nil {
		t.Fatalf("parseToken() error = %v", err)
	}
	if c.Subject != "user-123" {
		t.Errorf("Subject = %q, want user-123", c.Subject)
	}
	if c.Email != "ops@example.com" {
		t.Errorf("Email = %q", c.Email)
	}
	if c.Role != string(protocol.RoleOperator) {
		t.Errorf("Role = %q, want operator", c.Role)
	}
	if c.Issuer != "agentfleet" {
		t.Errorf("Issuer = %q, want agentfleet", c.Issuer)
	}
}

func TestParseTokenRejects(t *testing.T) {
	good := testServer("secret-a", time.Hour)
	u := &protocol.User{ID: "u1", Email: "a@b.c", Role: protocol.RoleAdmin}
	valid, _, err := good.issueToken(u)
	if err != nil {
		t.Fatal(err)
	}

	// A token minted with a different signing secret.
	other := testServer("secret-b", time.Hour)
	foreign, _, err := other.issueToken(u)
	if err != nil {
		t.Fatal(err)
	}

	// An already-expired token.
	past := testServer("secret-a", -time.Hour)
	expired, _, err := past.issueToken(u)
	if err != nil {
		t.Fatal(err)
	}

	// A token with no expiry at all: parseToken requires one.
	noExp, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role:             "admin",
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u1", Issuer: "agentfleet"},
	}).SignedString([]byte("secret-a"))
	if err != nil {
		t.Fatal(err)
	}

	// A correctly signed token from a different issuer.
	wrongIssuer, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			Issuer:    "somebody-else",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte("secret-a"))
	if err != nil {
		t.Fatal(err)
	}

	// The classic alg:none downgrade.
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "u1",
			Issuer:    "agentfleet",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		tok  string
	}{
		{"empty", ""},
		{"garbage", "not-a-token"},
		{"signed with another secret", foreign},
		{"expired", expired},
		{"no expiry claim", noExp},
		{"wrong issuer", wrongIssuer},
		{"alg none", none},
		{"tampered payload", tamper(valid)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := good.parseToken(tc.tok); err == nil {
				t.Errorf("parseToken(%s) = nil error, want a rejection", tc.name)
			}
		})
	}

	// Sanity: the untouched token still parses.
	if _, err := good.parseToken(valid); err != nil {
		t.Fatalf("the control token failed to parse: %v", err)
	}
}

// tamper flips a character in the token payload, invalidating the signature.
func tamper(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || len(parts[1]) == 0 {
		return tok + "x"
	}
	b := []byte(parts[1])
	if b[len(b)-1] == 'A' {
		b[len(b)-1] = 'B'
	} else {
		b[len(b)-1] = 'A'
	}
	parts[1] = string(b)
	return strings.Join(parts, ".")
}

// --------------------------------------------------------------- tokenFrom ---

func TestTokenFrom(t *testing.T) {
	cases := []struct {
		name   string
		header string
		url    string
		want   string
	}{
		{"bearer header", "Bearer abc123", "/api/me", "abc123"},
		{"bearer header with padding", "Bearer   abc123  ", "/api/me", "abc123"},
		{"query fallback for websockets", "", "/api/ws?token=abc123", "abc123"},
		{"header wins over query", "Bearer fromheader", "/api/ws?token=fromquery", "fromheader"},
		{"no credentials at all", "", "/api/me", ""},
		{"wrong scheme is ignored", "Basic abc123", "/api/me", ""},
		{"lowercase bearer is not accepted", "bearer abc123", "/api/me", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := tokenFrom(r); got != tc.want {
				t.Errorf("tokenFrom() = %q, want %q", got, tc.want)
			}
		})
	}
}

// -------------------------------------------------------------- requireAuth ---

func TestRequireAuth(t *testing.T) {
	s := testServer("test-secret", time.Hour)
	token := func(role protocol.Role) string {
		tok, _, err := s.issueToken(&protocol.User{ID: "u1", Email: "a@b.c", Role: role})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	cases := []struct {
		name     string
		gate     string
		auth     string
		wantCode int
	}{
		{"no token", roleAny, "", http.StatusUnauthorized},
		{"garbage token", roleAny, "Bearer nonsense", http.StatusUnauthorized},
		{"auditor reads", roleAny, "Bearer " + token(protocol.RoleAuditor), http.StatusOK},
		{"auditor cannot operate", roleOperator, "Bearer " + token(protocol.RoleAuditor), http.StatusForbidden},
		{"auditor cannot administer", roleAdmin, "Bearer " + token(protocol.RoleAuditor), http.StatusForbidden},
		{"operator operates", roleOperator, "Bearer " + token(protocol.RoleOperator), http.StatusOK},
		{"operator cannot administer", roleAdmin, "Bearer " + token(protocol.RoleOperator), http.StatusForbidden},
		{"admin administers", roleAdmin, "Bearer " + token(protocol.RoleAdmin), http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := s.requireAuth(tc.gate, func(w http.ResponseWriter, r *http.Request) {
				called = true
				if c := userFrom(r.Context()); c == nil || c.Subject != "u1" {
					t.Error("the handler did not receive the claims in its context")
				}
				w.WriteHeader(http.StatusOK)
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if wantCalled := tc.wantCode == http.StatusOK; called != wantCalled {
				t.Errorf("handler called = %v, want %v", called, wantCalled)
			}
		})
	}
}

func TestUserFromWithoutClaims(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if c := userFrom(r.Context()); c != nil {
		t.Errorf("userFrom(empty context) = %v, want nil", c)
	}
}
