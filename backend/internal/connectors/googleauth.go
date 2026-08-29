package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Signing in to Google with an account rather than an API key.
//
// This is the flow the Gemini extension in an editor uses from the operator's
// point of view: you are sent to a Google page, you approve, and the tool holds
// a token afterwards. It uses the device flow rather than a loopback redirect
// because the API runs in a container and the client is often a phone —
// neither can reliably receive a redirect on localhost, and a code you type on
// whatever device is handy works in both cases.
//
// You supply your own OAuth client, created in Google Cloud Console. That is
// the sanctioned way to do this: reusing another product's client credentials
// to inherit its entitlements would be impersonating that product, and would
// break the moment those credentials rotate.

const (
	googleDeviceCodeURL = "https://oauth2.googleapis.com/device/code"
	googleTokenURL      = "https://oauth2.googleapis.com/token"

	// GoogleCloudScope is what a Gemini or Vertex call needs. Anything narrower
	// cannot reach the generative endpoints; anything broader is more access
	// than this needs.
	GoogleCloudScope = "https://www.googleapis.com/auth/cloud-platform"
)

// DeviceAuth is what the operator has to act on: show the code, open the URL.
type DeviceAuth struct {
	DeviceCode      string `json:"-"` // never leaves the server
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// ErrAuthPending means the operator has not finished approving yet. It is the
// normal state for most of this flow, not a failure.
var ErrAuthPending = errors.New("waiting for you to approve the sign-in")

// ErrAuthDeclined means they said no, or the code expired.
var ErrAuthDeclined = errors.New("sign-in was declined or the code expired")

// DeviceEndpoints is where a provider's device flow lives. Empty fields fall
// back to Google's, which is what the google-flavoured provider kinds use.
type DeviceEndpoints struct {
	DeviceURL string
	TokenURL  string
	Scope     string
}

func (e DeviceEndpoints) device() string {
	if e.DeviceURL != "" {
		return e.DeviceURL
	}
	return googleDeviceCodeURL
}

func (e DeviceEndpoints) token() string {
	if e.TokenURL != "" {
		return e.TokenURL
	}
	return googleTokenURL
}

func (e DeviceEndpoints) scope() string {
	if e.Scope != "" {
		return e.Scope
	}
	return GoogleCloudScope
}

// StartDeviceAuth asks the provider for a code to show the operator.
func StartDeviceAuth(ctx context.Context, hc *http.Client, clientID, scope string) (*DeviceAuth, error) {
	return StartDeviceAuthAt(ctx, hc, DeviceEndpoints{Scope: scope}, clientID)
}

// StartDeviceAuthAt is StartDeviceAuth against a specific provider's endpoints.
func StartDeviceAuthAt(ctx context.Context, hc *http.Client, ep DeviceEndpoints, clientID string) (*DeviceAuth, error) {
	if clientID == "" {
		return nil, errors.New("an OAuth client ID is required to sign in")
	}
	scope := ep.scope()

	form := url.Values{"client_id": {clientID}, "scope": {scope}}
	var out struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		ErrorDesc       string `json:"error_description"`
	}
	if err := postForm(ctx, hc, ep.device(), form, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("the provider refused the sign-in request: %s", firstNonEmpty(out.ErrorDesc, out.Error))
	}

	verify := firstNonEmpty(out.VerificationURL, out.VerificationURI)
	if out.DeviceCode == "" || out.UserCode == "" || verify == "" {
		return nil, errors.New("the provider returned an incomplete device code response")
	}
	if out.Interval <= 0 {
		out.Interval = 5
	}
	return &DeviceAuth{
		DeviceCode:      out.DeviceCode,
		UserCode:        out.UserCode,
		VerificationURL: verify,
		ExpiresIn:       out.ExpiresIn,
		Interval:        out.Interval,
	}, nil
}

// PollDeviceAuth checks once whether the operator has approved.
//
// One attempt rather than a blocking loop: the caller is an HTTP handler being
// polled by a phone that may close the app mid-flow, and a goroutine blocking
// for fifteen minutes on a sign-in nobody is going to finish is a leak.
func PollDeviceAuth(ctx context.Context, hc *http.Client, clientID, clientSecret, deviceCode string) (refreshToken string, err error) {
	return pollAt(ctx, hc, googleTokenURL, clientID, clientSecret, deviceCode)
}

// PollDeviceAuthAt is PollDeviceAuth against a specific provider's endpoints.
func PollDeviceAuthAt(ctx context.Context, hc *http.Client, ep DeviceEndpoints, clientID, clientSecret, deviceCode string) (string, error) {
	return pollAt(ctx, hc, ep.token(), clientID, clientSecret, deviceCode)
}

// pollAt is PollDeviceAuth against a given endpoint, so the flow can be tested
// without reaching Google.
func pollAt(ctx context.Context, hc *http.Client, endpoint, clientID, clientSecret, deviceCode string) (string, error) {
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	var out struct {
		RefreshToken string `json:"refresh_token"`
		AccessToken  string `json:"access_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := postForm(ctx, hc, endpoint, form, &out); err != nil {
		return "", err
	}

	switch out.Error {
	case "":
	case "authorization_pending", "slow_down":
		return "", ErrAuthPending
	case "access_denied", "expired_token":
		return "", ErrAuthDeclined
	default:
		return "", fmt.Errorf("google rejected the sign-in: %s", firstNonEmpty(out.ErrorDesc, out.Error))
	}

	if out.RefreshToken == "" {
		// Without one, the sign-in lasts an hour and then silently stops
		// working — worse than failing here, where it can be explained.
		return "", errors.New("google did not return a refresh token; the OAuth client must be a Desktop or TV/limited-input app")
	}
	return out.RefreshToken, nil
}

// TokenSource yields a currently-valid bearer token.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// GoogleTokenSource turns a stored refresh token into usable access tokens.
//
// Access tokens last about an hour, so this caches one and renews it slightly
// early: an agent mid-run must not fail because its token expired between
// deciding on an action and sending it.
type GoogleTokenSource struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	HC           *http.Client

	// TokenURL overrides Google's endpoint, for providers with their own.
	TokenURL string

	// tokenURL overrides everything. Set only by tests.
	tokenURL string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// Token returns a valid access token, refreshing when necessary.
func (g *GoogleTokenSource) Token(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// A minute of headroom: a token that expires mid-flight reads as an auth
	// failure and sends the operator looking for a revoked sign-in.
	if g.token != "" && time.Now().Before(g.expires.Add(-time.Minute)) {
		return g.token, nil
	}

	form := url.Values{
		"client_id":     {g.ClientID},
		"refresh_token": {g.RefreshToken},
		"grant_type":    {"refresh_token"},
	}
	if g.ClientSecret != "" {
		form.Set("client_secret", g.ClientSecret)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	endpoint := firstNonEmpty(g.tokenURL, g.TokenURL, googleTokenURL)
	if err := postForm(ctx, g.HC, endpoint, form, &out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("the sign-in expired, sign in again: %s",
			firstNonEmpty(out.ErrorDesc, out.Error))
	}
	if out.AccessToken == "" {
		return "", errors.New("the provider returned no access token")
	}

	g.token = out.AccessToken
	g.expires = time.Now().Add(time.Duration(max(out.ExpiresIn, 60)) * time.Second)
	return g.token, nil
}

// GoogleCredentials is what is sealed in the vault for a signed-in provider.
//
// The client secret is stored alongside the refresh token because refreshing
// needs both, and splitting them across the vault and a database column would
// mean a provider row that looks complete but cannot actually authenticate.
type GoogleCredentials struct {
	RefreshToken string `json:"refresh_token"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// EncodeGoogleCredentials seals credentials for the vault.
func EncodeGoogleCredentials(c GoogleCredentials) (string, error) {
	blob, err := json.Marshal(c)
	return string(blob), err
}

// DecodeGoogleCredentials reads them back.
//
// A plain string is accepted as a bare refresh token so a credential written
// before this format existed still works.
func DecodeGoogleCredentials(s string) GoogleCredentials {
	s = strings.TrimSpace(s)
	if s == "" {
		return GoogleCredentials{}
	}
	if !strings.HasPrefix(s, "{") {
		return GoogleCredentials{RefreshToken: s}
	}
	var c GoogleCredentials
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return GoogleCredentials{RefreshToken: s}
	}
	return c
}

func postForm(ctx context.Context, hc *http.Client, endpoint string, form url.Values, out any) error {
	if hc == nil {
		hc = defaultClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Decoded regardless of status: these endpoints report their real reason in
	// the body, and "400 Bad Request" alone tells the operator nothing.
	return json.NewDecoder(res.Body).Decode(out)
}
