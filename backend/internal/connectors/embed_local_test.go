package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The local embedding sidecar is the floor under semantic memory: it is what
// stops "no provider can embed" from meaning "keyword search only". These pin
// its client against a fake sidecar speaking the real wire shape.

func fakeSidecar(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_, _ = w.Write([]byte(`{"model":"minishlab/potion-base-8M","dim":256}`))
		case "/embed":
			// Two fixed vectors, enough to check order and count survive.
			_, _ = w.Write([]byte(`{"model":"minishlab/potion-base-8M","dim":256,` +
				`"vectors":[[0.1,0.2],[0.3,0.4]]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLocalEmbedderProbesInfoAndEmbeds(t *testing.T) {
	srv := fakeSidecar(t, 256)
	defer srv.Close()
	t.Setenv("EMBED_BASE_URL", srv.URL)

	emb, err := newLocalEmbedder(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if emb.Model() != "minishlab/potion-base-8M" || emb.Dim() != 256 {
		t.Errorf("identity = %s/%d, want the sidecar's own answer", emb.Model(), emb.Dim())
	}

	vecs, err := emb.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][1] != 0.4 {
		t.Errorf("vectors = %v", vecs)
	}
}

func TestLocalEmbedderRefusesAMiscountedBatch(t *testing.T) {
	// One vector for two texts would silently mis-file every memory in the
	// batch under the wrong vector — an error is the only honest answer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			_, _ = w.Write([]byte(`{"model":"m","dim":2}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","dim":2,"vectors":[[0.1,0.2]]}`))
	}))
	defer srv.Close()
	t.Setenv("EMBED_BASE_URL", srv.URL)

	emb, err := newLocalEmbedder(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emb.Embed(context.Background(), []string{"a", "b"}); err == nil ||
		!strings.Contains(err.Error(), "1 vectors for 2 texts") {
		t.Fatalf("a miscounted batch was accepted: %v", err)
	}
}

func TestTheSidecarCanBeSwitchedOff(t *testing.T) {
	t.Setenv("EMBED_BASE_URL", "off")
	if _, err := newLocalEmbedder(context.Background()); err == nil {
		t.Fatal("EMBED_BASE_URL=off still produced an embedder")
	}
}
