// Package vault seals operator credentials with AES-256-GCM so API keys and
// sandbox logins are never stored, logged or prompt-embedded in plaintext.
//
// Secrets are addressed by ref (e.g. "openai.api_key"). Everything else in the
// system passes refs around; only the connector layer and the sandbox injector
// ever call Open, and neither writes the result to a log.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

type Backend interface {
	PutSecret(ctx context.Context, ref string, sealed []byte, note string) error
	GetSecret(ctx context.Context, ref string) ([]byte, error)
	DeleteSecret(ctx context.Context, ref string) error
	SecretRefs(ctx context.Context) ([]map[string]any, error)
}

type Vault struct {
	aead  cipher.AEAD
	store Backend

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	value string
	until time.Time
}

const cacheTTL = 5 * time.Minute

func New(masterKey []byte, store Backend) (*Vault, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, store: store, cache: map[string]cacheEntry{}}, nil
}

// Put seals and stores a secret, replacing any previous value for the ref.
func (v *Vault) Put(ctx context.Context, ref, value, note string) error {
	if ref == "" {
		return errors.New("ref required")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := v.aead.Seal(nonce, nonce, []byte(value), []byte(ref))
	if err := v.store.PutSecret(ctx, ref, sealed, note); err != nil {
		return err
	}
	v.mu.Lock()
	delete(v.cache, ref)
	v.mu.Unlock()
	return nil
}

// Open resolves a ref to plaintext. An empty ref resolves to an empty string so
// callers can treat "no key configured" uniformly.
func (v *Vault) Open(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	v.mu.RLock()
	if e, ok := v.cache[ref]; ok && time.Now().Before(e.until) {
		v.mu.RUnlock()
		return e.value, nil
	}
	v.mu.RUnlock()

	sealed, err := v.store.GetSecret(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("vault: %s: %w", ref, err)
	}
	if len(sealed) < v.aead.NonceSize() {
		return "", errors.New("vault: ciphertext too short")
	}
	nonce, body := sealed[:v.aead.NonceSize()], sealed[v.aead.NonceSize():]
	plain, err := v.aead.Open(nil, nonce, body, []byte(ref))
	if err != nil {
		return "", fmt.Errorf("vault: %s: decrypt failed (wrong MASTER_KEY?)", ref)
	}
	v.mu.Lock()
	v.cache[ref] = cacheEntry{value: string(plain), until: time.Now().Add(cacheTTL)}
	v.mu.Unlock()
	return string(plain), nil
}

func (v *Vault) Delete(ctx context.Context, ref string) error {
	v.mu.Lock()
	delete(v.cache, ref)
	v.mu.Unlock()
	return v.store.DeleteSecret(ctx, ref)
}

// Refs lists secret names and notes — never values.
func (v *Vault) Refs(ctx context.Context) ([]map[string]any, error) {
	return v.store.SecretRefs(ctx)
}
