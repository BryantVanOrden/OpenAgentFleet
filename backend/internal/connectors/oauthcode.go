package connectors

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Authorization-code sign-in with PKCE.
//
// This is the flow that can complete inside the app: the consent page loads in
// a webview, the provider redirects back, and the code on that redirect is
// exchanged for tokens. The device-code flow in googleauth.go stays for
// headless use, but it always meant leaving the app to type a code somewhere
// else, which is not signing in so much as describing how to.
//
// PKCE rather than a client secret in the app: the app never holds either. It
// only ever sees a URL to display and, at the end, whether it worked.

// AuthCodeEndpoints describes a provider's authorization-code flow.
type AuthCodeEndpoints struct {
	AuthURL  string
	TokenURL string
	Scope    string
}

const (
	googleAuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
)

func (e AuthCodeEndpoints) auth() string {
	if e.AuthURL != "" {
		return e.AuthURL
	}
	return googleAuthURL
}

func (e AuthCodeEndpoints) token() string {
	if e.TokenURL != "" {
		return e.TokenURL
	}
	return googleTokenURL
}

func (e AuthCodeEndpoints) scope() string {
	if e.Scope != "" {
		return e.Scope
	}
	return GoogleCloudScope
}

// PKCE holds one sign-in attempt's proof.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a verifier and its challenge.
func NewPKCE() (PKCE, error) {
	raw := make([]byte, 48)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

// NewState generates the anti-forgery value tying a redirect back to the
// sign-in that started it.
func NewState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// AuthCodeURL builds the consent page URL to load in the webview.
func AuthCodeURL(ep AuthCodeEndpoints, clientID, redirectURI, state string, p PKCE) string {
	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {ep.scope()},
		"state":                 {state},
		"code_challenge":        {p.Challenge},
		"code_challenge_method": {"S256"},
		// Without these Google returns no refresh token on a repeat sign-in,
		// and the connection silently stops working an hour later.
		"access_type": {"offline"},
		"prompt":      {"consent"},
	}
	sep := "?"
	if strings.Contains(ep.auth(), "?") {
		sep = "&"
	}
	return ep.auth() + sep + q.Encode()
}

// ExchangeCode turns the redirect's code into a refresh token.
func ExchangeCode(ctx context.Context, hc *http.Client, ep AuthCodeEndpoints,
	clientID, clientSecret, redirectURI, code string, p PKCE) (refreshToken string, err error) {

	form := url.Values{
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
		"code_verifier": {p.Verifier},
	}
	// Confidential clients still send a secret alongside PKCE; public ones have
	// none, and sending an empty one is rejected outright by some providers.
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	var out struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := postForm(ctx, hc, ep.token(), form, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("the provider rejected the sign-in: %s",
			firstNonEmpty(out.ErrorDesc, out.Error))
	}
	if out.RefreshToken == "" {
		return "", errors.New(
			"the provider returned no refresh token; the sign-in would stop working within the hour")
	}
	return out.RefreshToken, nil
}
