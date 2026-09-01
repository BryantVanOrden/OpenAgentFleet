package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// The embedding sidecar: the floor under semantic memory.
//
// BestEmbedder used to end at ErrNoEmbedder, which meant a fleet whose only
// provider is Anthropic — no embedding API — degraded to the hashed keyword
// index. The compose stack now ships a small local embedding service
// (embed/, model2vec potion-base-8M) as a default service, and this client is
// how the orchestrator reaches it. Configured providers still win on quality;
// this is what makes "no provider can embed" stop meaning "no semantic
// recall".

const defaultEmbedBase = "http://embed:8081"

// embedBase is the sidecar's address, overridable for deployments that run it
// elsewhere. Empty string disables it entirely (EMBED_BASE_URL=off works too).
func embedBase() string {
	v := strings.TrimSpace(os.Getenv("EMBED_BASE_URL"))
	if strings.EqualFold(v, "off") || strings.EqualFold(v, "none") {
		return ""
	}
	if v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultEmbedBase
}

type localEmbedder struct {
	base  string
	model string
	dim   int
}

func (e *localEmbedder) Model() string { return e.model }
func (e *localEmbedder) Dim() int      { return e.dim }

type localEmbedRequest struct {
	Texts []string `json:"texts"`
}

type localEmbedResponse struct {
	Model   string      `json:"model"`
	Dim     int         `json:"dim"`
	Vectors [][]float32 `json:"vectors"`
	Detail  string      `json:"detail,omitempty"`
}

func (e *localEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(localEmbedRequest{Texts: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding sidecar unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("embedding sidecar returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	var out localEmbedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("embedding sidecar sent an unreadable response: %w", err)
	}
	if len(out.Vectors) != len(texts) {
		// A mismatch silently mis-files every memory in the batch under the
		// wrong vector, which is worse than an error.
		return nil, fmt.Errorf("embedding sidecar returned %d vectors for %d texts",
			len(out.Vectors), len(texts))
	}
	return out.Vectors, nil
}

// newLocalEmbedder probes the sidecar and returns an embedder over it.
func newLocalEmbedder(ctx context.Context) (Embedder, error) {
	base := embedBase()
	if base == "" {
		return nil, errors.New("the embedding sidecar is disabled (EMBED_BASE_URL=off)")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding sidecar unreachable at %s: %w", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding sidecar returned %d to /info", resp.StatusCode)
	}
	var info struct {
		Model string `json:"model"`
		Dim   int    `json:"dim"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info); err != nil {
		return nil, fmt.Errorf("embedding sidecar sent an unreadable /info: %w", err)
	}
	if info.Dim <= 0 || info.Model == "" {
		return nil, fmt.Errorf("embedding sidecar /info is incomplete: %+v", info)
	}
	return &localEmbedder{base: base, model: info.Model, dim: info.Dim}, nil
}
