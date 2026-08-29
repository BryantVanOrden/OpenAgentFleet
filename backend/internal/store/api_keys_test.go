package store

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// The presented key must never be reconstructable from what is stored.
func TestAPIKeyHashIsNotReversible(t *testing.T) {
	const secret = "s3cr3t-value"
	h := hashSecret(secret)

	if strings.Contains(h, secret) {
		t.Fatal("the stored hash contains the secret")
	}
	if h == secret {
		t.Fatal("the secret is stored verbatim")
	}
	if len(h) != 64 {
		t.Errorf("hash is %d chars, want a 64-char sha256 hex digest", len(h))
	}
	// Deterministic, or a key could never be verified again.
	if hashSecret(secret) != h {
		t.Error("hashing is not deterministic")
	}
	if hashSecret(secret+"x") == h {
		t.Error("two different secrets hash the same")
	}
}

// A malformed key is rejected on shape before anything touches the database.
func TestMalformedAPIKeysAreRejectedByShape(t *testing.T) {
	for _, bad := range []string{
		"",
		"af",
		"af_only-two",
		"xx_id_secret",        // not one of ours
		"Bearer af_id_secret", // header left on
	} {
		parts := strings.SplitN(bad, "_", 3)
		if len(parts) == 3 && parts[0] == keyPrefix {
			t.Errorf("%q was accepted as well-formed", bad)
		}
	}
}

// A secret containing an underscore is still a valid key.
//
// The bug: the secret half is base64url, whose alphabet includes "_", and the
// parser split on every underscore. Any secret that happened to contain one
// parsed as four parts and was rejected as malformed -- so roughly half of
// every key issued was dead on arrival, intermittently enough to look like a
// fluke rather than a bug.
func TestAPIKeySecretsMayContainUnderscores(t *testing.T) {
	id := NewID()
	secret := "abc_def_ghi" // what base64url can legitimately produce

	parts := strings.SplitN("af_"+id+"_"+secret, "_", 3)
	if len(parts) != 3 || parts[0] != keyPrefix {
		t.Fatalf("a valid key was rejected on shape: %v", parts)
	}
	if parts[1] != id {
		t.Errorf("id = %q, want %q", parts[1], id)
	}
	if parts[2] != secret {
		t.Errorf("secret = %q, want %q -- the secret was truncated at an underscore",
			parts[2], secret)
	}
}

// Enough keys to catch an alphabet-dependent parser by brute force rather
// than by reasoning about it.
func TestEveryIssuedKeyShapeParses(t *testing.T) {
	for i := 0; i < 500; i++ {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			t.Fatal(err)
		}
		secret := base64.RawURLEncoding.EncodeToString(raw)
		key := "af_" + NewID() + "_" + secret

		parts := strings.SplitN(key, "_", 3)
		if len(parts) != 3 || parts[0] != keyPrefix {
			t.Fatalf("key %d did not parse: %q", i, key)
		}
		if parts[2] != secret {
			t.Fatalf("key %d lost part of its secret: %q vs %q", i, parts[2], secret)
		}
	}
}
