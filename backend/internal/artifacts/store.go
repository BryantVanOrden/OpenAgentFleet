// Package artifacts stores the binary side of a run: step screenshots, session
// recordings, recorder key frames and build outputs the agent produced.
//
// Two backends ship: a filesystem store (default, zero setup) and an S3 store
// that works against MinIO or AWS. Both are addressed by opaque keys such as
// "tasks/<id>/step-0007.webp".
package artifacts

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("artifact not found")

type Store interface {
	Put(ctx context.Context, key, contentType string, body []byte) error
	PutBase64(ctx context.Context, key, contentType, b64 string) error
	Get(ctx context.Context, key string) ([]byte, string, error)
	Delete(ctx context.Context, key string) error
}

// ---------------------------------------------------------------- filesystem ---

type FSStore struct{ root string }

func NewFSStore(root string) (*FSStore, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &FSStore{root: root}, nil
}

func (s *FSStore) path(key string) (string, error) {
	clean := filepath.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
	full := filepath.Join(s.root, clean)
	// Defence in depth against a crafted key climbing out of the root.
	if !strings.HasPrefix(full, filepath.Clean(s.root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal artifact key %q", key)
	}
	return full, nil
}

func (s *FSStore) Put(_ context.Context, key, contentType string, body []byte) error {
	full, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(full, body, 0o640); err != nil {
		return err
	}
	if contentType != "" {
		_ = os.WriteFile(full+".type", []byte(contentType), 0o640)
	}
	return nil
}

func (s *FSStore) PutBase64(ctx context.Context, key, contentType, b64 string) error {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("artifact %s: %w", key, err)
	}
	return s.Put(ctx, key, contentType, raw)
}

func (s *FSStore) Get(_ context.Context, key string) ([]byte, string, error) {
	full, err := s.path(key)
	if err != nil {
		return nil, "", err
	}
	body, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	ct := guessType(key)
	if t, err := os.ReadFile(full + ".type"); err == nil {
		ct = strings.TrimSpace(string(t))
	}
	return body, ct, nil
}

func (s *FSStore) Delete(_ context.Context, key string) error {
	full, err := s.path(key)
	if err != nil {
		return err
	}
	_ = os.Remove(full + ".type")
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func guessType(key string) string {
	switch strings.ToLower(filepath.Ext(key)) {
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".json", ".jsonl":
		return "application/json"
	case ".md":
		return "text/markdown"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// drain is a small helper shared by the S3 backend.
func drain(r io.Reader) []byte {
	b, _ := io.ReadAll(io.LimitReader(r, 64<<20))
	return b
}
