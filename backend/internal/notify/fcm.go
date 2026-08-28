package notify

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// FCM implements the HTTP v1 API. The legacy server-key endpoint is dead, so we
// mint an OAuth token from the service account on demand and cache it.
type FCM struct {
	projectID string
	email     string
	tokenURI  string
	key       *rsa.PrivateKey
	hc        *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

type serviceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

func NewFCM(projectID, serviceAccountPath string) (*FCM, error) {
	raw, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return nil, fmt.Errorf("read service account: %w", err)
	}
	var sa serviceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, fmt.Errorf("parse service account: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("service account is missing client_email or private_key")
	}
	key, err := parseRSAKey(sa.PrivateKey)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		projectID = sa.ProjectID
	}
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	return &FCM{
		projectID: projectID,
		email:     sa.ClientEmail,
		tokenURI:  tokenURI,
		key:       key,
		hc:        &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(pemStr, "\\n", "\n")))
	if block == nil {
		return nil, errors.New("private_key is not valid PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("private_key is not RSA")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func (f *FCM) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Now().Before(f.tokenExp.Add(-60*time.Second)) {
		return f.token, nil
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   f.email,
		"scope": fcmScope,
		"aud":   f.tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(f.key)
	if err != nil {
		return "", err
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.tokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("oauth token: %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	f.token = out.AccessToken
	f.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return f.token, nil
}

// Send delivers one notification. Critical alerts get high priority and, on
// Android, a channel that bypasses Do Not Disturb if the app requested it.
func (f *FCM) Send(ctx context.Context, deviceToken, title, body string, data map[string]string, severity string) error {
	token, err := f.accessToken(ctx)
	if err != nil {
		return err
	}

	channel := "agentfleet_alerts"
	priority := "high"
	if severity == "info" {
		priority = "normal"
	}

	msg := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]any{
				"title": title,
				"body":  truncate(body, 240),
			},
			"data": data,
			"android": map[string]any{
				"priority": priority,
				"notification": map[string]any{
					"channel_id": channel,
					"tag":        data["alert_id"],
				},
			},
			"apns": map[string]any{
				"headers": map[string]string{"apns-priority": "10"},
				"payload": map[string]any{
					"aps": map[string]any{
						"sound":              "default",
						"interruption-level": interruption(severity),
					},
				},
			},
		},
	}

	buf, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", f.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return &PushError{Status: resp.StatusCode, Body: string(raw)}
	}
	return nil
}

// PushError carries the provider status so dead tokens can be pruned.
type PushError struct {
	Status int
	Body   string
}

func (e *PushError) Error() string {
	return fmt.Sprintf("push: %d: %s", e.Status, truncate(e.Body, 300))
}

// IsUnregistered reports whether the device token should be deleted.
func IsUnregistered(err error) bool {
	var pe *PushError
	if !errors.As(err, &pe) {
		return false
	}
	if pe.Status == http.StatusGone || pe.Status == http.StatusNotFound {
		return true
	}
	return strings.Contains(pe.Body, "UNREGISTERED") ||
		strings.Contains(pe.Body, "BadDeviceToken") ||
		strings.Contains(pe.Body, "Unregistered")
}

func interruption(severity string) string {
	if severity == "critical" {
		return "time-sensitive"
	}
	return "active"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
