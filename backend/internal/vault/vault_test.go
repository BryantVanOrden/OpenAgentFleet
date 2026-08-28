package vault

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// ------------------------------------------------------------ fake backend ---

var errNoSuchSecret = errors.New("secret not found")

// memBackend is an in-memory Backend. It also lets a test move a sealed blob
// between refs, which is how the AAD binding is exercised.
type memBackend struct {
	mu      sync.Mutex
	secrets map[string][]byte
	notes   map[string]string
	puts    int
}

func newMemBackend() *memBackend {
	return &memBackend{secrets: map[string][]byte{}, notes: map[string]string{}}
}

func (m *memBackend) PutSecret(_ context.Context, ref string, sealed []byte, note string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(sealed))
	copy(cp, sealed)
	m.secrets[ref] = cp
	m.notes[ref] = note
	m.puts++
	return nil
}

func (m *memBackend) GetSecret(_ context.Context, ref string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.secrets[ref]
	if !ok {
		return nil, errNoSuchSecret
	}
	return b, nil
}

func (m *memBackend) DeleteSecret(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.secrets, ref)
	delete(m.notes, ref)
	return nil
}

func (m *memBackend) SecretRefs(_ context.Context) ([]map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, 0, len(m.secrets))
	for ref := range m.secrets {
		out = append(out, map[string]any{"ref": ref, "note": m.notes[ref]})
	}
	return out, nil
}

// raw returns the sealed bytes stored under ref, or nil.
func (m *memBackend) raw(ref string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.secrets[ref]
}

// setRaw plants sealed bytes under a ref without going through Vault.Put.
func (m *memBackend) setRaw(ref string, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[ref] = b
}

func key(fill byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = fill + byte(i)
	}
	return k
}

func newVault(t *testing.T, k []byte, b Backend) *Vault {
	t.Helper()
	v, err := New(k, b)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return v
}

// ---------------------------------------------------------------------- New ---

func TestNewRejectsBadKeyLength(t *testing.T) {
	cases := []struct {
		name    string
		keyLen  int
		wantErr bool
	}{
		{"nil key", -1, true},
		{"empty key", 0, true},
		{"16 bytes (AES-128)", 16, true},
		{"31 bytes", 31, true},
		{"33 bytes", 33, true},
		{"64 bytes", 64, true},
		{"32 bytes", 32, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var k []byte
			if tc.keyLen >= 0 {
				k = make([]byte, tc.keyLen)
			}
			_, err := New(k, newMemBackend())
			if tc.wantErr && err == nil {
				t.Fatal("New() = nil error, want a length error")
			}
			if tc.wantErr && !strings.Contains(err.Error(), "32 bytes") {
				t.Errorf("error = %q, want it to mention 32 bytes", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("New() error = %v", err)
			}
		})
	}
}

// -------------------------------------------------------------- round trip ---

func TestPutOpenRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		ref   string
		value string
	}{
		{"api key", "openai.api_key", "sk-proj-0123456789"},
		{"empty value", "empty.secret", ""},
		{"unicode", "note.secret", "pässwörd — ünïcode ✓"},
		{"long value", "long.secret", strings.Repeat("x", 8192)},
		{"newlines", "pem.secret", "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"},
	}
	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newMemBackend()
			v := newVault(t, key(1), b)
			if err := v.Put(ctx, tc.ref, tc.value, "a note"); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			// The plaintext must never touch the backend.
			if sealed := b.raw(tc.ref); tc.value != "" && strings.Contains(string(sealed), tc.value) {
				t.Error("plaintext leaked into the sealed blob")
			}
			got, err := v.Open(ctx, tc.ref)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if got != tc.value {
				t.Errorf("Open() = %q, want %q", got, tc.value)
			}
		})
	}
}

func TestPutRequiresARef(t *testing.T) {
	b := newMemBackend()
	v := newVault(t, key(1), b)
	if err := v.Put(context.Background(), "", "value", ""); err == nil {
		t.Fatal("Put() with an empty ref = nil error, want an error")
	}
	if b.puts != 0 {
		t.Error("Put() with an empty ref reached the backend")
	}
}

func TestOpenEmptyRefIsNotAnError(t *testing.T) {
	b := newMemBackend()
	v := newVault(t, key(1), b)
	got, err := v.Open(context.Background(), "")
	if err != nil {
		t.Fatalf("Open(\"\") error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("Open(\"\") = %q, want empty", got)
	}
}

func TestOpenUnknownRef(t *testing.T) {
	v := newVault(t, key(1), newMemBackend())
	if _, err := v.Open(context.Background(), "nope"); err == nil {
		t.Fatal("Open(unknown) = nil error, want an error")
	}
}

func TestPutReplacesAndInvalidatesTheCache(t *testing.T) {
	ctx := context.Background()
	v := newVault(t, key(1), newMemBackend())
	if err := v.Put(ctx, "r", "first", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Open(ctx, "r"); got != "first" { // warms the cache
		t.Fatalf("Open() = %q, want first", got)
	}
	if err := v.Put(ctx, "r", "second", ""); err != nil {
		t.Fatal(err)
	}
	got, err := v.Open(ctx, "r")
	if err != nil {
		t.Fatal(err)
	}
	if got != "second" {
		t.Errorf("Open() after replace = %q, want second (stale cache)", got)
	}
}

// --------------------------------------------------------------- AAD / keys ---

// TestRefIsBoundAsAAD moves a sealed blob from one ref to another. Because the
// ref is the additional authenticated data, the relocated blob must not open —
// otherwise an operator who can write the secrets table could swap a low-value
// credential into a high-value slot.
func TestRefIsBoundAsAAD(t *testing.T) {
	ctx := context.Background()
	b := newMemBackend()
	v := newVault(t, key(1), b)

	if err := v.Put(ctx, "openai.api_key", "sk-secret", ""); err != nil {
		t.Fatal(err)
	}
	b.setRaw("anthropic.api_key", b.raw("openai.api_key"))

	if _, err := v.Open(ctx, "anthropic.api_key"); err == nil {
		t.Fatal("a blob sealed under one ref opened under another; AAD binding is broken")
	} else if !strings.Contains(err.Error(), "decrypt failed") {
		t.Errorf("error = %q, want a decrypt failure", err)
	}
	// The original ref still works.
	if got, err := v.Open(ctx, "openai.api_key"); err != nil || got != "sk-secret" {
		t.Errorf("Open(original) = %q, %v", got, err)
	}
}

func TestWrongMasterKeyFailsToDecrypt(t *testing.T) {
	ctx := context.Background()
	b := newMemBackend()

	good := newVault(t, key(1), b)
	if err := good.Put(ctx, "r", "sk-secret", ""); err != nil {
		t.Fatal(err)
	}

	bad := newVault(t, key(9), b) // different MASTER_KEY, same store
	if _, err := bad.Open(ctx, "r"); err == nil {
		t.Fatal("Open() with the wrong master key succeeded")
	} else if !strings.Contains(err.Error(), "MASTER_KEY") {
		t.Errorf("error = %q, want it to point at MASTER_KEY", err)
	}
}

func TestTruncatedCiphertextIsRejected(t *testing.T) {
	ctx := context.Background()
	b := newMemBackend()
	v := newVault(t, key(1), b)
	if err := v.Put(ctx, "r", "sk-secret", ""); err != nil {
		t.Fatal(err)
	}
	b.setRaw("r", b.raw("r")[:4]) // shorter than the nonce
	if _, err := v.Open(ctx, "r"); err == nil {
		t.Fatal("a truncated blob opened without error")
	}
}

func TestTamperedCiphertextIsRejected(t *testing.T) {
	ctx := context.Background()
	b := newMemBackend()
	v := newVault(t, key(1), b)
	if err := v.Put(ctx, "r", "sk-secret", ""); err != nil {
		t.Fatal(err)
	}
	sealed := append([]byte(nil), b.raw("r")...)
	sealed[len(sealed)-1] ^= 0xff
	b.setRaw("r", sealed)
	if _, err := v.Open(ctx, "r"); err == nil {
		t.Fatal("a tampered blob opened without error")
	}
}

func TestNonceIsFreshPerPut(t *testing.T) {
	ctx := context.Background()
	b := newMemBackend()
	v := newVault(t, key(1), b)
	if err := v.Put(ctx, "a", "same-value", ""); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "b", "same-value", ""); err != nil {
		t.Fatal(err)
	}
	if string(b.raw("a")) == string(b.raw("b")) {
		t.Error("identical plaintexts produced identical ciphertexts; the nonce is not fresh")
	}
}

// ------------------------------------------------------------------ delete ---

func TestDeleteRemovesAndInvalidatesTheCache(t *testing.T) {
	ctx := context.Background()
	b := newMemBackend()
	v := newVault(t, key(1), b)

	if err := v.Put(ctx, "r", "sk-secret", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Open(ctx, "r"); got != "sk-secret" { // warms the cache
		t.Fatalf("Open() = %q", got)
	}
	if err := v.Delete(ctx, "r"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if b.raw("r") != nil {
		t.Error("Delete() left the blob in the backend")
	}
	if got, err := v.Open(ctx, "r"); err == nil {
		t.Fatalf("Open() after Delete returned %q from a stale cache", got)
	}
}

func TestRefsListsNamesAndNotesOnly(t *testing.T) {
	ctx := context.Background()
	v := newVault(t, key(1), newMemBackend())
	if err := v.Put(ctx, "openai.api_key", "sk-secret", "prod key"); err != nil {
		t.Fatal(err)
	}
	refs, err := v.Refs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("got %d refs, want 1", len(refs))
	}
	if refs[0]["ref"] != "openai.api_key" || refs[0]["note"] != "prod key" {
		t.Errorf("refs = %v", refs[0])
	}
	for k, val := range refs[0] {
		if s, ok := val.(string); ok && strings.Contains(s, "sk-secret") {
			t.Errorf("Refs() leaked the secret value in field %q", k)
		}
	}
}

func TestConcurrentOpenIsSafe(t *testing.T) {
	ctx := context.Background()
	v := newVault(t, key(1), newMemBackend())
	if err := v.Put(ctx, "r", "sk-secret", ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := v.Open(ctx, "r")
			if err != nil {
				errs <- err
			} else if got != "sk-secret" {
				errs <- errors.New("wrong value: " + got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Open: %v", err)
	}
}
