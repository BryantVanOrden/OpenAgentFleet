package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// normaliseKey turns whatever the operator put in MASTER_KEY into exactly 32
// bytes. Base64 and hex encodings of a 32-byte key are used verbatim; anything
// else is stretched with SHA-256 so a passphrase still works.
func normaliseKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("empty key")
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}
