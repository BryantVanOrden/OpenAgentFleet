package artifacts

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) (*FSStore, string) {
	t.Helper()
	root := t.TempDir()
	s, err := NewFSStore(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatalf("NewFSStore() error = %v", err)
	}
	return s, root
}

func TestFSStoreRoundTrip(t *testing.T) {
	cases := []struct {
		name        string
		key         string
		contentType string
		body        string
		wantType    string
	}{
		{"explicit content type wins", "tasks/abc/step-0007.webp", "image/webp", "\x00\x01binary", "image/webp"},
		{"content type is preserved verbatim", "runs/1/notes.md", "text/markdown; charset=utf-8", "# hi", "text/markdown; charset=utf-8"},
		{"extension is used when no content type is given", "runs/1/trace.json", "", `{"a":1}`, "application/json"},
		{"unknown extension falls back to octet-stream", "runs/1/blob.bin", "", "xyz", "application/octet-stream"},
		{"png is recognised", "a.png", "", "p", "image/png"},
		{"jpeg is recognised", "a.JPEG", "", "j", "image/jpeg"},
		{"mp4 is recognised", "session.mp4", "", "m", "video/mp4"},
		{"nested key creates directories", "a/b/c/d/e.webp", "", "deep", "image/webp"},
		{"empty body round-trips", "empty.bin", "application/octet-stream", "", "application/octet-stream"},
	}

	ctx := context.Background()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			if err := s.Put(ctx, tc.key, tc.contentType, []byte(tc.body)); err != nil {
				t.Fatalf("Put() error = %v", err)
			}
			body, ct, err := s.Get(ctx, tc.key)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if string(body) != tc.body {
				t.Errorf("body = %q, want %q", body, tc.body)
			}
			if ct != tc.wantType {
				t.Errorf("content type = %q, want %q", ct, tc.wantType)
			}
			if err := s.Delete(ctx, tc.key); err != nil {
				t.Fatalf("Delete() error = %v", err)
			}
			if _, _, err := s.Get(ctx, tc.key); !errors.Is(err, ErrNotFound) {
				t.Errorf("Get() after Delete = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestFSStorePutOverwrites(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	if err := s.Put(ctx, "k.bin", "text/plain", []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "k.bin", "text/html", []byte("second")); err != nil {
		t.Fatal(err)
	}
	body, ct, err := s.Get(ctx, "k.bin")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "second" || ct != "text/html" {
		t.Errorf("got %q/%q, want second/text/html", body, ct)
	}
}

func TestFSStoreGetMissing(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.Get(context.Background(), "nope/missing.webp"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestFSStoreDeleteMissingIsNotAnError(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Delete(context.Background(), "nope/missing.webp"); err != nil {
		t.Errorf("Delete(missing) = %v, want nil", err)
	}
}

func TestFSStoreDeleteRemovesTheTypeSidecar(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)
	if err := s.Put(ctx, "k.bin", "text/plain", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "k.bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "k.bin.type")); !os.IsNotExist(err) {
		t.Errorf("the .type sidecar survived Delete: %v", err)
	}
}

func TestFSStorePutBase64(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)

	if err := s.PutBase64(ctx, "shot.webp", "image/webp", "aGVsbG8="); err != nil {
		t.Fatalf("PutBase64() error = %v", err)
	}
	body, _, err := s.Get(ctx, "shot.webp")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want hello", body)
	}

	for _, bad := range []string{"not base64!!!", "aGVsbG8", "###"} {
		err := s.PutBase64(ctx, "bad.webp", "image/webp", bad)
		if err == nil {
			t.Errorf("PutBase64(%q) = nil error, want a decode error", bad)
		}
		if err != nil && !strings.Contains(err.Error(), "bad.webp") {
			t.Errorf("error %q should name the key", err)
		}
		if _, _, err := s.Get(ctx, "bad.webp"); !errors.Is(err, ErrNotFound) {
			t.Errorf("a failed PutBase64 must not create the object, got %v", err)
		}
	}
}

// filesUnder lists every regular file below dir, as paths relative to dir.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

// TestFSStorePathTraversal pins the real contract: a crafted key is either
// rejected outright or clamped back inside the root. Under no circumstances may
// a byte land outside the store root.
func TestFSStorePathTraversal(t *testing.T) {
	keys := []string{
		"../../etc/passwd",
		"../escape.txt",
		"../../../../../../../../tmp/pwned.txt",
		`..\..\x`,
		`..\..\..\Windows\System32\evil.dll`,
		"/absolute/x",
		"//absolute/y",
		"a/../../../b",
		"a/./../../c",
		"",
		".",
		"..",
		"/",
	}

	ctx := context.Background()
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			// Two levels of nesting so ../.. would still land inside tmp and be
			// observable, rather than escaping into the OS temp dir at large.
			tmp := t.TempDir()
			root := filepath.Join(tmp, "a", "b", "artifacts")
			s, err := NewFSStore(root)
			if err != nil {
				t.Fatalf("NewFSStore() error = %v", err)
			}
			before := filesUnder(t, tmp)

			putErr := s.Put(ctx, key, "text/plain", []byte("owned"))

			// Nothing may exist outside the root, whether Put succeeded or not.
			for _, f := range filesUnder(t, tmp) {
				if !strings.HasPrefix(f, "a/b/artifacts/") {
					t.Errorf("key %q escaped the root: wrote %s (put err %v)", key, f, putErr)
				}
			}
			if putErr != nil {
				// Rejected keys must leave the tree untouched.
				if got := len(filesUnder(t, tmp)); got != len(before) {
					t.Errorf("key %q was rejected but still wrote %d file(s)", key, got-len(before))
				}
				return
			}
			// Accepted keys must be readable back through the same clamping.
			body, _, err := s.Get(ctx, key)
			if err != nil {
				t.Errorf("key %q was accepted by Put but Get failed: %v", key, err)
			} else if string(body) != "owned" {
				t.Errorf("key %q round-trip body = %q", key, body)
			}
		})
	}
}

func TestFSStoreTraversalCannotClobberASibling(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "artifacts")
	secret := filepath.Join(tmp, "secret.txt")
	if err := os.WriteFile(secret, []byte("do not touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewFSStore(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put(ctx, "../secret.txt", "text/plain", []byte("clobbered"))

	got, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "do not touch" {
		t.Fatalf("a sibling file outside the root was overwritten: %q", got)
	}
}

func TestNewFSStoreCreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deep", "nested", "root")
	if _, err := NewFSStore(root); err != nil {
		t.Fatalf("NewFSStore() error = %v", err)
	}
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		t.Fatalf("root was not created: %v", err)
	}
}

func TestGuessType(t *testing.T) {
	cases := map[string]string{
		"a.webp": "image/webp", "a.PNG": "image/png", "a.jpg": "image/jpeg",
		"a.jpeg": "image/jpeg", "a.json": "application/json", "a.jsonl": "application/json",
		"a.md": "text/markdown", "a.mp4": "video/mp4", "a": "application/octet-stream",
		"a.tar.gz": "application/octet-stream",
	}
	for key, want := range cases {
		if got := guessType(key); got != want {
			t.Errorf("guessType(%q) = %q, want %q", key, got, want)
		}
	}
}
