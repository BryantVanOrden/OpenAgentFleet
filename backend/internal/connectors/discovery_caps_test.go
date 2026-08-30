package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The default model in .env.example reports "vision" and was being offered as
// text-only, because the old check looked for "vl" in the name.
func TestOllamaVisionComesFromCapabilitiesNotTheName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "qwen3.5:4b"}, {"name": "ornith:latest"}},
			})
		case "/api/show":
			var body struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			caps := []string{"completion"}
			if body.Model == "qwen3.5:4b" {
				caps = []string{"completion", "vision", "tools", "thinking"}
			}
			json.NewEncoder(w).Encode(map[string]any{"capabilities": caps})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := listOllamaDynamic(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 models, got %d", len(got))
	}
	if !got[0].Vision {
		t.Errorf("qwen3.5:4b reports vision but was listed as text-only")
	}
	if got[0].Thinking != "Reasoning" {
		t.Errorf("qwen3.5:4b reports thinking, got Thinking=%q", got[0].Thinking)
	}
	if got[1].Vision {
		t.Errorf("ornith:latest does not report vision but was listed as sighted")
	}
	// Order must survive the concurrent lookups.
	if got[0].ID != "qwen3.5:4b" || got[1].ID != "ornith:latest" {
		t.Errorf("model order scrambled: %q, %q", got[0].ID, got[1].ID)
	}
}

// When /api/show cannot be reached the name heuristic still has to answer.
func TestOllamaFallsBackToNameWhenShowIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "llama3.2-vision:11b"}, {"name": "ornith:latest"}},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := listOllamaDynamic(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}
	if !got[0].Vision {
		t.Errorf("llama3.2-vision should fall back to sighted by name")
	}
	if got[1].Vision {
		t.Errorf("ornith:latest should fall back to text-only by name")
	}
}
