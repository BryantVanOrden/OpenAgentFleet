package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A pending approval is the normal state for most of this flow and must be
// distinguishable from a real failure, or the UI reports an error while the
// operator is still reading the consent screen.
func TestPollDeviceAuthReportsPendingSeparately(t *testing.T) {
	cases := []struct {
		body string
		want error
	}{
		{`{"error":"authorization_pending"}`, ErrAuthPending},
		{`{"error":"slow_down"}`, ErrAuthPending},
		{`{"error":"access_denied"}`, ErrAuthDeclined},
		{`{"error":"expired_token"}`, ErrAuthDeclined},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(tc.body))
		}))
		hc := srv.Client()

		_, err := pollAt(context.Background(), hc, srv.URL, "cid", "secret", "dc")
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.body, err, tc.want)
		}
		srv.Close()
	}
}

// A sign-in with no refresh token works for an hour and then silently stops.
// Failing at the point of sign-in is the only place it can be explained.
func TestPollDeviceAuthRejectsMissingRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"ya29.short-lived"}`))
	}))
	defer srv.Close()

	_, err := pollAt(context.Background(), srv.Client(), srv.URL, "cid", "secret", "dc")
	if err == nil {
		t.Fatal("accepted a sign-in with no refresh token")
	}
}

// A cached access token is reused rather than refreshed on every request.
func TestTokenSourceCachesUntilNearExpiry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-1", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	ts := &GoogleTokenSource{ClientID: "cid", RefreshToken: "rt", HC: srv.Client()}
	ts.tokenURL = srv.URL

	for i := 0; i < 3; i++ {
		got, err := ts.Token(context.Background())
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		if got != "token-1" {
			t.Fatalf("got %q", got)
		}
	}
	if calls != 1 {
		t.Errorf("refreshed %d times, want 1", calls)
	}
}

// A token about to expire is renewed early: expiring mid-request reads as an
// auth failure and sends the operator looking for a revoked sign-in.
func TestTokenSourceRenewsBeforeExpiry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	ts := &GoogleTokenSource{ClientID: "cid", RefreshToken: "rt", HC: srv.Client()}
	ts.tokenURL = srv.URL
	ts.token = "stale"
	// Inside the one-minute headroom, so it must not be handed out.
	ts.expires = time.Now().Add(30 * time.Second)

	got, err := ts.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "fresh" || calls != 1 {
		t.Errorf("got %q after %d refreshes; want a fresh token", got, calls)
	}
}

// An expired refresh token has to say so plainly — this is the error an
// operator sees months later when Google invalidates the grant.
func TestTokenSourceReportsRevokedGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	defer srv.Close()

	ts := &GoogleTokenSource{ClientID: "cid", RefreshToken: "rt", HC: srv.Client()}
	ts.tokenURL = srv.URL

	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("a revoked grant was reported as success")
	}
}

func TestGoogleCredentialsRoundTrip(t *testing.T) {
	blob, err := EncodeGoogleCredentials(GoogleCredentials{
		RefreshToken: "1//rt", ClientSecret: "cs",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := DecodeGoogleCredentials(blob)
	if got.RefreshToken != "1//rt" || got.ClientSecret != "cs" {
		t.Errorf("round trip lost data: %+v", got)
	}

	// A bare token, as written before this format existed, still loads.
	if got := DecodeGoogleCredentials("1//legacy"); got.RefreshToken != "1//legacy" {
		t.Errorf("legacy credential not read: %+v", got)
	}
	if got := DecodeGoogleCredentials(""); got.RefreshToken != "" {
		t.Error("empty credential produced a token")
	}
}

// A signed-in provider with no stored credential must fail to build rather
// than quietly falling back to unauthenticated requests.
func TestBuildRefusesSignedInProviderWithNoCredential(t *testing.T) {
	_, err := Build(protocol.Provider{
		ID: "p1", Name: "Gemini", Kind: protocol.ProviderGemini,
		Model: "gemini-2.0-flash", AuthMode: "oauth",
	}, "", nil)
	if err == nil {
		t.Fatal("built a signed-in provider with no credential")
	}
}

// Building with a credential produces a bearer-token connector, not a
// key-authenticated one.
func TestBuildSignedInProviderUsesTokenSource(t *testing.T) {
	blob, _ := EncodeGoogleCredentials(GoogleCredentials{RefreshToken: "rt", ClientSecret: "cs"})
	c, err := Build(protocol.Provider{
		ID: "p1", Name: "Gemini", Kind: protocol.ProviderGemini,
		Model: "gemini-2.0-flash", AuthMode: "oauth", OAuthClientID: "cid",
	}, blob, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	g, ok := c.(*gemini)
	if !ok {
		t.Fatalf("got %T", c)
	}
	if g.tokens == nil {
		t.Fatal("no token source on a signed-in provider")
	}
	if g.key != "" {
		t.Error("an API key was left set alongside a sign-in")
	}
}

// A provider with its own endpoints must not be sent to Google's.
func TestDeviceEndpointsFallBackToGoogleOnlyWhenEmpty(t *testing.T) {
	custom := DeviceEndpoints{
		DeviceURL: "https://example.test/device",
		TokenURL:  "https://example.test/token",
		Scope:     "custom-scope",
	}
	if custom.device() != "https://example.test/device" {
		t.Error("custom device URL was overridden by Google's")
	}
	if custom.token() != "https://example.test/token" {
		t.Error("custom token URL was overridden by Google's")
	}
	if custom.scope() != "custom-scope" {
		t.Error("custom scope was overridden")
	}

	var empty DeviceEndpoints
	if empty.device() != googleDeviceCodeURL || empty.token() != googleTokenURL {
		t.Error("an unset provider did not fall back to Google")
	}
	if empty.scope() != GoogleCloudScope {
		t.Error("an unset scope did not fall back")
	}
}

// A signed-in Anthropic provider sends a bearer token, never x-api-key.
func TestSignedInAnthropicUsesBearer(t *testing.T) {
	blob, _ := EncodeGoogleCredentials(GoogleCredentials{RefreshToken: "rt"})
	c, err := Build(protocol.Provider{
		ID: "p1", Name: "Claude", Kind: protocol.ProviderAnthropic,
		Model: "claude-opus-5", AuthMode: "oauth", OAuthClientID: "cid",
		OAuthTokenURL: "https://example.test/token",
	}, blob, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a, ok := c.(*anthropic)
	if !ok {
		t.Fatalf("got %T", c)
	}
	if a.tokens == nil {
		t.Fatal("no token source on a signed-in Anthropic provider")
	}
	if a.key != "" {
		t.Error("an API key was left set alongside a sign-in")
	}
	if a.tokens.TokenURL != "https://example.test/token" {
		t.Errorf("token source ignored the provider's endpoint: %q", a.tokens.TokenURL)
	}
}
