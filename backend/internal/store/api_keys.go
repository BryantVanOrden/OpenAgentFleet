package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// keyPrefix marks a string as one of ours, so a key pasted into the wrong
// field is recognisable on sight and greppable in a log that should not have
// it.
const keyPrefix = "af"

// ErrKeyInvalid is returned for anything that is not a live key: malformed,
// unknown, revoked, or belonging to a disabled user. Deliberately one error —
// telling a caller which of those it was tells an attacker which key ids exist.
var ErrKeyInvalid = errors.New("invalid api key")

// NewAPIKey issues a key and returns it with the one and only copy of its
// secret.
//
// Format is af_<id>_<secret>: the id is the public half so a request finds its
// row directly instead of hashing against every key, and only the secret half
// is compared.
func (s *Store) NewAPIKey(ctx context.Context, name, userID, createdBy string) (protocol.APIKey, error) {
	id := NewID()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return protocol.APIKey{}, fmt.Errorf("could not generate a key: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)

	k := protocol.APIKey{
		ID:        id,
		Name:      strings.TrimSpace(name),
		UserID:    userID,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
		Secret:    fmt.Sprintf("%s_%s_%s", keyPrefix, id, secret),
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_keys(id,name,user_id,secret_hash,created_by,created_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		k.ID, k.Name, k.UserID, hashSecret(secret), k.CreatedBy, k.CreatedAt)
	if err != nil {
		return protocol.APIKey{}, norm(err)
	}
	return k, nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// UserForAPIKey resolves a presented key to the user it acts as, and records
// that it was used.
//
// Returns ErrKeyInvalid for every failure mode without distinguishing them.
func (s *Store) UserForAPIKey(ctx context.Context, presented string) (*protocol.User, error) {
	// SplitN, not Split: the secret is base64url, whose alphabet includes "_".
	// Splitting on every underscore turned any secret containing one into four
	// parts and rejected it as malformed -- so roughly half of all issued keys
	// never worked, intermittently enough to look like a fluke.
	parts := strings.SplitN(presented, "_", 3)
	if len(parts) != 3 || parts[0] != keyPrefix {
		return nil, ErrKeyInvalid
	}
	id, secret := parts[1], parts[2]

	var storedHash, userID string
	var revoked *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT secret_hash,user_id,revoked_at FROM api_keys WHERE id=$1`, id).
		Scan(&storedHash, &userID, &revoked)
	if err != nil {
		return nil, ErrKeyInvalid
	}
	if revoked != nil {
		return nil, ErrKeyInvalid
	}
	// Constant time: a byte-by-byte comparison leaks how much of a guessed
	// secret was right.
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(hashSecret(secret))) != 1 {
		return nil, ErrKeyInvalid
	}

	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return nil, ErrKeyInvalid
	}
	if u.Disabled() {
		// Disabling a user must take their keys with them, or revoking
		// someone's access would leave every key they hold working.
		return nil, ErrKeyInvalid
	}

	// Best effort: a key that works should not fail because the bookkeeping
	// did. Detached from the request context so it survives the response.
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = s.pool.Exec(bg, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	}()

	return u, nil
}

// ListAPIKeys returns every key with its owner's email, newest first. Never
// the secret: there is no copy of it to return.
func (s *Store) ListAPIKeys(ctx context.Context) ([]protocol.APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT k.id,k.name,k.user_id,COALESCE(u.email,''),k.created_by,
		        k.created_at,k.last_used_at,k.revoked_at
		   FROM api_keys k LEFT JOIN users u ON u.id = k.user_id
		  ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := []protocol.APIKey{}
	for rows.Next() {
		var k protocol.APIKey
		var lastUsed, revoked *time.Time
		if err := rows.Scan(&k.ID, &k.Name, &k.UserID, &k.UserEmail, &k.CreatedBy,
			&k.CreatedAt, &lastUsed, &revoked); err != nil {
			return nil, err
		}
		if lastUsed != nil {
			k.LastUsedAt = *lastUsed
		}
		if revoked != nil {
			k.RevokedAt = *revoked
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey turns a key off, keeping the row so past use stays traceable.
func (s *Store) RevokeAPIKey(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
