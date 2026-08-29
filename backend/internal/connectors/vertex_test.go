package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Vertex names the model in the path and the API version in the body; the
// direct API does the opposite. Sending either in the wrong place is rejected,
// so the two shapes have to stay distinct.
func TestVertexUsesRawPredictAndBodyVersion(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotVersionHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVersionHeader = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hi"}},
		})
	}))
	defer srv.Close()

	c := &anthropic{
		p: protocol.Provider{
			ID: "p", Name: "Claude", Kind: protocol.ProviderAnthropicVertex,
			Model: "claude-opus-4-5@20251101", MaxTokens: 64,
		},
		base:   srv.URL + "/v1/projects/proj/locations/us-east5",
		key:    "unused",
		hc:     srv.Client(),
		vertex: true,
	}

	if _, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Text: "hello"}},
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/publishers/anthropic/models/claude-opus-4-5@20251101:rawPredict") {
		t.Errorf("wrong Vertex path: %s", gotPath)
	}
	if gotBody["anthropic_version"] != vertexAnthropicVersion {
		t.Errorf("anthropic_version in body = %v, want %q",
			gotBody["anthropic_version"], vertexAnthropicVersion)
	}
	if _, present := gotBody["model"]; present {
		t.Error("model was sent in the body; Vertex takes it in the path")
	}
	if gotVersionHeader != "" {
		t.Errorf("anthropic-version header sent to Vertex: %q", gotVersionHeader)
	}
}

// The direct API must be untouched by any of the above.
func TestDirectAnthropicKeepsHeaderAndBodyModel(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotVersionHeader, gotKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotVersionHeader = r.Header.Get("anthropic-version")
		gotKey = r.Header.Get("x-api-key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": "hi"}},
		})
	}))
	defer srv.Close()

	c := &anthropic{
		p: protocol.Provider{
			ID: "p", Name: "Claude", Kind: protocol.ProviderAnthropic,
			Model: "claude-opus-5", MaxTokens: 64,
		},
		base: srv.URL,
		key:  "sk-ant-test",
		hc:   srv.Client(),
	}

	if _, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Text: "hello"}},
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %s, want /v1/messages", gotPath)
	}
	if gotBody["model"] != "claude-opus-5" {
		t.Errorf("model missing from body: %v", gotBody["model"])
	}
	if _, present := gotBody["anthropic_version"]; present {
		t.Error("Vertex's body version was sent to the direct API")
	}
	if gotVersionHeader != anthropicVersion {
		t.Errorf("anthropic-version header = %q", gotVersionHeader)
	}
	if gotKey != "sk-ant-test" {
		t.Errorf("x-api-key = %q", gotKey)
	}
}

// Vertex with no address would 404 in a way that reads like the model does not
// exist, so it fails at build with something actionable instead.
func TestVertexRequiresAnAddress(t *testing.T) {
	_, err := Build(protocol.Provider{
		ID: "p", Name: "Claude Vertex", Kind: protocol.ProviderAnthropicVertex,
		Model: "claude-opus-4-5",
	}, "", nil)
	if err == nil {
		t.Fatal("built a Vertex provider with no address")
	}
	if !strings.Contains(err.Error(), "aiplatform.googleapis.com") {
		t.Errorf("error does not show the expected shape: %v", err)
	}
}

// Signing in with Google is the point of this kind: it must carry a bearer
// token, never an API key.
func TestVertexSignedInUsesBearer(t *testing.T) {
	blob, _ := EncodeGoogleCredentials(GoogleCredentials{RefreshToken: "rt"})
	c, err := Build(protocol.Provider{
		ID: "p", Name: "Claude Vertex", Kind: protocol.ProviderAnthropicVertex,
		Model: "claude-opus-4-5", AuthMode: "oauth", OAuthClientID: "cid",
		BaseURL: "https://us-east5-aiplatform.googleapis.com/v1/projects/x/locations/us-east5",
	}, blob, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a := c.(*anthropic)
	if !a.vertex {
		t.Error("vertex envelope not enabled for the vertex kind")
	}
	if a.tokens == nil {
		t.Fatal("no token source on a signed-in Vertex provider")
	}
	if a.key != "" {
		t.Error("an API key was left set alongside a sign-in")
	}
}
