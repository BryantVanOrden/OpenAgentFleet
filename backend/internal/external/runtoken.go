package external

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Run tokens let an external agent call back about one run -- report
// progress, finish, comment, hand part of its ticket on -- and nothing else.
// They are HMACs over the task id and an expiry, so there is no table to
// clean up and no token that outlives its run by more than its expiry.

const runTokenPrefix = "afr_"

// MintRunToken signs a token for one task.
func MintRunToken(secret []byte, taskID string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	body := taskID + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("run:" + body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return runTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(body)) + "." + sig
}

// VerifyRunToken returns the task a token is for.
func VerifyRunToken(secret []byte, tok string) (string, error) {
	tok = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tok), "Bearer "))
	if !strings.HasPrefix(tok, runTokenPrefix) {
		return "", fmt.Errorf("not a run token")
	}
	parts := strings.SplitN(strings.TrimPrefix(tok, runTokenPrefix), ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("malformed run token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("malformed run token")
	}
	body := string(raw)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("run:" + body))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return "", fmt.Errorf("bad run token signature")
	}
	i := strings.LastIndexByte(body, '.')
	if i <= 0 {
		return "", fmt.Errorf("malformed run token")
	}
	exp, err := strconv.ParseInt(body[i+1:], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", fmt.Errorf("run token expired")
	}
	return body[:i], nil
}
