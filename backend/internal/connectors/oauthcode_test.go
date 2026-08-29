package connectors

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The challenge must be the S256 hash of the verifier, or the exchange is
// rejected at the end of a flow the operator has already completed.
func TestPKCEChallengeMatchesVerifier(t *testing.T) {
	p, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge %q does not hash from the verifier", p.Challenge)
	}
	if strings.ContainsAny(p.Verifier, "+/=") {
		t.Error("verifier is not URL-safe")
	}
}

func TestPKCEAndStateAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p, _ := NewPKCE()
		s, _ := NewState()
		if seen[p.Verifier] || seen[s] {
			t.Fatal("generated a duplicate verifier or state")
		}
		seen[p.Verifier], seen[s] = true, true
	}
}

// Without offline access and a forced consent prompt, a repeat sign-in returns
// no refresh token and the connection dies an hour later.
func TestAuthCodeURLAsksForOfflineAccess(t *testing.T) {
	p := PKCE{Challenge: "chal"}
	raw := AuthCodeURL(AuthCodeEndpoints{}, "cid", "https://host/cb", "st", p)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	checks := map[string]string{
		"client_id":             "cid",
		"redirect_uri":          "https://host/cb",
		"response_type":         "code",
		"state":                 "st",
		"code_challenge":        "chal",
		"code_challenge_method": "S256",
		"access_type":           "offline",
		"prompt":                "consent",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if q.Get("scope") != GoogleCloudScope {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if !strings.HasPrefix(raw, googleAuthURL) {
		t.Errorf("unset endpoint did not fall back to Google: %s", raw)
	}
}

func TestAuthCodeURLUsesProviderEndpoint(t *testing.T) {
	raw := AuthCodeURL(AuthCodeEndpoints{
		AuthURL: "https://auth.example.test/authorize",
		Scope:   "read",
	}, "cid", "https://host/cb", "st", PKCE{})

	if !strings.HasPrefix(raw, "https://auth.example.test/authorize?") {
		t.Errorf("provider endpoint ignored: %s", raw)
	}
	u, _ := url.Parse(raw)
	if u.Query().Get("scope") != "read" {
		t.Error("provider scope ignored")
	}
}

// The verifier has to reach the token endpoint, or PKCE proves nothing.
func TestExchangeCodeSendsVerifier(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "1//rt"})
	}))
	defer srv.Close()

	refresh, err := ExchangeCode(context.Background(), srv.Client(),
		AuthCodeEndpoints{TokenURL: srv.URL},
		"cid", "secret", "https://host/cb", "the-code", PKCE{Verifier: "ver"})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if refresh != "1//rt" {
		t.Errorf("refresh token = %q", refresh)
	}
	if got.Get("code_verifier") != "ver" {
		t.Error("the verifier was not sent; PKCE proves nothing")
	}
	if got.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", got.Get("grant_type"))
	}
	if got.Get("client_secret") != "secret" {
		t.Error("a confidential client's secret was dropped")
	}
}

// A public client has no secret, and sending an empty one is rejected outright
// by some providers.
func TestExchangeCodeOmitsEmptySecret(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{"refresh_token": "1//rt"})
	}))
	defer srv.Close()

	if _, err := ExchangeCode(context.Background(), srv.Client(),
		AuthCodeEndpoints{TokenURL: srv.URL},
		"cid", "", "https://host/cb", "code", PKCE{Verifier: "v"}); err != nil {
		t.Fatal(err)
	}
	if _, present := got["client_secret"]; present {
		t.Error("an empty client_secret was sent")
	}
}

// An exchange that yields no refresh token must fail loudly: it would appear to
// work and stop within the hour.
func TestExchangeCodeRejectsMissingRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"ya29.only"}`))
	}))
	defer srv.Close()

	if _, err := ExchangeCode(context.Background(), srv.Client(),
		AuthCodeEndpoints{TokenURL: srv.URL},
		"cid", "", "https://host/cb", "code", PKCE{Verifier: "v"}); err == nil {
		t.Fatal("accepted an exchange with no refresh token")
	}
}

func TestExchangeCodeSurfacesProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Bad redirect URI"}`))
	}))
	defer srv.Close()

	_, err := ExchangeCode(context.Background(), srv.Client(),
		AuthCodeEndpoints{TokenURL: srv.URL},
		"cid", "", "https://host/cb", "code", PKCE{Verifier: "v"})
	if err == nil || !strings.Contains(err.Error(), "Bad redirect URI") {
		t.Errorf("provider's reason was lost: %v", err)
	}
}
