package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNormaliseKeyVerbatimEncodings(t *testing.T) {
	raw32 := make([]byte, 32)
	for i := range raw32 {
		raw32[i] = byte(i * 7)
	}

	cases := []struct {
		name string
		in   string
		want []byte
	}{
		{
			name: "32-byte standard base64 is used verbatim",
			in:   base64.StdEncoding.EncodeToString(raw32),
			want: raw32,
		},
		{
			name: "64-char lowercase hex is used verbatim",
			in:   hex.EncodeToString(raw32),
			want: raw32,
		},
		{
			name: "64-char uppercase hex is used verbatim",
			in:   strings.ToUpper(hex.EncodeToString(raw32)),
			want: raw32,
		},
		{
			name: "all-zero base64 key is used verbatim",
			in:   base64.StdEncoding.EncodeToString(make([]byte, 32)),
			want: make([]byte, 32),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseKey(tc.in)
			if err != nil {
				t.Fatalf("normaliseKey() error = %v", err)
			}
			if len(got) != 32 {
				t.Fatalf("len = %d, want 32", len(got))
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("normaliseKey() = %x, want %x", got, tc.want)
			}
		})
	}
}

func TestNormaliseKeyStretchesEverythingElse(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"passphrase with spaces", "correct horse battery staple"},
		{"short passphrase", "hunter2"},
		{"single character", "x"},
		{"the shipped development default", "dev-insecure-master-key-0123456789abcdef"},
		{"base64 that decodes to the wrong length", base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"hex that decodes to the wrong length", hex.EncodeToString(make([]byte, 16))},
		{"a very long passphrase", strings.Repeat("long-passphrase-", 100)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseKey(tc.in)
			if err != nil {
				t.Fatalf("normaliseKey() error = %v", err)
			}
			if len(got) != 32 {
				t.Fatalf("len = %d, want 32", len(got))
			}
			want := sha256.Sum256([]byte(tc.in))
			if !bytes.Equal(got, want[:]) {
				t.Errorf("normaliseKey() = %x, want the SHA-256 of the input %x", got, want)
			}
			// Deterministic: the same MASTER_KEY must reproduce the same key
			// across restarts or every sealed secret becomes unreadable.
			again, err := normaliseKey(tc.in)
			if err != nil {
				t.Fatalf("second normaliseKey() error = %v", err)
			}
			if !bytes.Equal(got, again) {
				t.Error("normaliseKey() is not deterministic")
			}
		})
	}
}

func TestNormaliseKeyDistinctInputsGiveDistinctKeys(t *testing.T) {
	a, err := normaliseKey("passphrase-one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := normaliseKey("passphrase-two")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("two different passphrases produced the same key")
	}
}

func TestNormaliseKeyRejectsEmpty(t *testing.T) {
	if _, err := normaliseKey(""); err == nil {
		t.Fatal("normaliseKey(\"\") = nil error, want an error")
	}
}

func TestNormaliseKeyAlwaysReturns32Bytes(t *testing.T) {
	// Whatever an operator puts in MASTER_KEY, aes.NewCipher must accept it.
	inputs := []string{
		"a", "ab", "abc", "!!!", "  spaces  ", "\n", "0", "==",
		base64.StdEncoding.EncodeToString(make([]byte, 32)),
		hex.EncodeToString(make([]byte, 32)),
		strings.Repeat("z", 1000),
	}
	for _, in := range inputs {
		got, err := normaliseKey(in)
		if err != nil {
			t.Errorf("normaliseKey(%q) error = %v", in, err)
			continue
		}
		if len(got) != 32 {
			t.Errorf("normaliseKey(%q) len = %d, want 32", in, len(got))
		}
	}
}
