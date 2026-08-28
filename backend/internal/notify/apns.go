package notify

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// APNs talks to Apple directly with a token-based (.p8) credential. Only needed
// for builds that do not route iOS through Firebase.
type APNs struct {
	teamID string
	keyID  string
	topic  string
	key    *ecdsa.PrivateKey
	host   string
	hc     *http.Client

	mu       sync.Mutex
	jwt      string
	issuedAt time.Time
}

func NewAPNs(teamID, keyID, keyPath, topic string, production bool) (*APNs, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read APNs key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("APNs key is not valid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("APNs key must be ECDSA (.p8)")
	}
	host := "https://api.sandbox.push.apple.com"
	if production {
		host = "https://api.push.apple.com"
	}
	return &APNs{
		teamID: teamID, keyID: keyID, topic: topic, key: key, host: host,
		// Go negotiates HTTP/2 over TLS automatically, which APNs requires.
		hc: &http.Client{Timeout: 20 * time.Second},
	}, nil
}

// providerToken is valid for an hour; Apple rejects tokens refreshed more than
// once every 20 minutes, so cache aggressively.
func (a *APNs) providerToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jwt != "" && time.Since(a.issuedAt) < 45*time.Minute {
		return a.jwt, nil
	}
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": a.teamID,
		"iat": now.Unix(),
	})
	tok.Header["kid"] = a.keyID
	signed, err := tok.SignedString(a.key)
	if err != nil {
		return "", err
	}
	a.jwt, a.issuedAt = signed, now
	return signed, nil
}

func (a *APNs) Send(ctx context.Context, deviceToken, title, body string, data map[string]string, needsReply bool) error {
	token, err := a.providerToken()
	if err != nil {
		return err
	}

	aps := map[string]any{
		"alert": map[string]string{"title": title, "body": truncate(body, 240)},
		"sound": "default",
	}
	if needsReply {
		// Lets the app show Approve / Take over buttons straight from the banner.
		aps["category"] = "AGENT_NEEDS_INPUT"
		aps["interruption-level"] = "time-sensitive"
	}
	payload := map[string]any{"aps": aps}
	for k, v := range data {
		payload[k] = v
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.host+"/3/device/"+deviceToken, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-topic", a.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	if id := data["alert_id"]; id != "" {
		req.Header.Set("apns-collapse-id", id)
	}

	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return &PushError{Status: resp.StatusCode, Body: string(raw)}
	}
	return nil
}
