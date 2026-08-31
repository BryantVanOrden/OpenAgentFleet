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
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Text embeddings, for episodic memory search.
//
// The memory index scored with a 128-dimensional hashed bag of words. That
// finds a memory when the query reuses its words and misses it entirely
// otherwise: "how do I sign in to the billing portal" does not match "logged
// into the invoicing site with the shared credential", which is exactly the
// recall a fleet memory exists to serve.
//
// Embeddings are optional. A deployment with no embedding-capable provider
// keeps the hashed vector, which is honest and works for keyword-ish recall;
// see memory.Engine, which reports which one it is using.

// EmbedTimeout bounds one embedding request. Recall happens inside an agent's
// turn, so a slow embedding endpoint must not hold the loop open.
const EmbedTimeout = 20 * time.Second

// Embedder turns text into a vector.
type Embedder interface {
	// Embed returns one vector per input, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model names the model, so a stored vector from a different one can be
	// recognised and recomputed rather than compared against nonsense.
	Model() string
	// Dim is the vector length this embedder produces.
	Dim() int
}

// ErrNoEmbedder means nothing configured can produce embeddings.
var ErrNoEmbedder = errors.New("no embedding-capable provider is configured")

// defaultEmbedModels are the per-kind defaults, used when the operator has not
// named an embedding model. They are the small, cheap ones: memory search
// wants many short vectors, not the best possible ranking.
var defaultEmbedModels = map[protocol.ProviderKind]string{
	protocol.ProviderOpenAI:     "text-embedding-3-small",
	protocol.ProviderCompatible: "text-embedding-3-small",
	protocol.ProviderOllama:     "nomic-embed-text",
	protocol.ProviderGemini:     "text-embedding-004",
}

// SupportsEmbedding reports whether a provider kind has an embedding endpoint
// this package knows how to call.
//
// Anthropic is absent because it has no embedding API at all — a fleet on
// Anthropic alone keeps the hashed fallback, and saying so is better than
// failing every recall.
func SupportsEmbedding(kind protocol.ProviderKind) bool {
	_, ok := defaultEmbedModels[kind]
	return ok
}

// NewEmbedder builds an embedder for a provider row. apiKey has already been
// resolved from the vault by the caller, exactly as Build expects it.
func NewEmbedder(p protocol.Provider, apiKey, embedModel string) (Embedder, error) {
	if strings.TrimSpace(embedModel) == "" {
		embedModel = defaultEmbedModels[p.Kind]
	}
	if embedModel == "" {
		return nil, fmt.Errorf("%w: %s has no embedding endpoint", ErrNoEmbedder, p.Kind)
	}

	switch p.Kind {
	case protocol.ProviderOpenAI, protocol.ProviderCompatible:
		base := firstNonEmpty(p.BaseURL, "https://api.openai.com/v1")
		return &openAIEmbedder{base: strings.TrimRight(base, "/"), key: apiKey, model: embedModel}, nil
	case protocol.ProviderOllama:
		base := firstNonEmpty(p.BaseURL, "http://localhost:11434")
		return &ollamaEmbedder{base: strings.TrimRight(base, "/"), model: embedModel}, nil
	case protocol.ProviderGemini:
		base := firstNonEmpty(p.BaseURL, "https://generativelanguage.googleapis.com")
		return &geminiEmbedder{base: strings.TrimRight(base, "/"), key: apiKey, model: embedModel}, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNoEmbedder, p.Kind)
}

var embedClient = &http.Client{Timeout: EmbedTimeout}

// BestEmbedder picks an embedder from the configured providers.
//
// Preference order is deliberate: a local Ollama first, because embedding every
// memory in the fleet against a paid API is a real cost for a feature the
// operator did not explicitly ask to pay for, and a local model is free and
// good enough for recall. Then OpenAI, then Gemini.
//
// Returns ErrNoEmbedder when nothing configured can embed, which is not a
// failure — the memory engine keeps its hashed vectors and says so.
func (r *Registry) BestEmbedder(ctx context.Context) (Embedder, error) {
	providers, err := r.src.ListProviders(ctx, true)
	if err != nil {
		return nil, err
	}

	preference := []protocol.ProviderKind{
		protocol.ProviderOllama,
		protocol.ProviderOpenAI,
		protocol.ProviderCompatible,
		protocol.ProviderGemini,
	}
	for _, want := range preference {
		for _, p := range providers {
			if p.Kind != want {
				continue
			}
			key := ""
			if p.APIKeyRef != "" && r.keys != nil {
				// A provider whose key cannot be opened is skipped rather than
				// failing the whole search: another provider may work.
				resolved, err := r.keys.Open(ctx, p.APIKeyRef)
				if err != nil {
					continue
				}
				key = resolved
			}
			// The model comes from the per-kind default, overridable with
			// EMBED_MODEL. A dedicated provider column would mean a migration
			// and a form field for something almost nobody changes, and the
			// env var covers the operator who does.
			emb, err := NewEmbedder(p, key, strings.TrimSpace(os.Getenv("EMBED_MODEL")))
			if err != nil {
				continue
			}
			// Probed with one trivial input before being handed over. An
			// embedder that cannot embed would otherwise be attached, trigger a
			// re-embed of every memory in the fleet, and fail on all of them.
			probeCtx, cancel := context.WithTimeout(ctx, EmbedTimeout)
			_, err = emb.Embed(probeCtx, []string{"agentfleet embedding probe"})
			cancel()
			if err != nil {
				if r.log != nil {
					r.log.Debug("provider cannot embed; trying the next",
						"provider", p.Name, "kind", p.Kind, "err", err)
				}
				continue
			}
			return emb, nil
		}
	}
	return nil, ErrNoEmbedder
}

// ------------------------------------------------------------------ OpenAI ---

type openAIEmbedder struct {
	base, key, model string
	dim              int
}

func (e *openAIEmbedder) Model() string { return e.model }
func (e *openAIEmbedder) Dim() int      { return e.dim }

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}

	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := doEmbed(req, e.key, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, redactKey(errors.New(out.Error.Message), e.key)
	}

	// Ordered by the index the API reports, not by arrival: the response is
	// allowed to come back in any order, and silently mismatching a vector to
	// the wrong memory would corrupt every future search against it.
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			continue
		}
		vecs[d.Index] = d.Embedding
	}
	return checkComplete(vecs, &e.dim)
}

// ------------------------------------------------------------------ Ollama ---

type ollamaEmbedder struct {
	base, model string
	dim         int
}

func (e *ollamaEmbedder) Model() string { return e.model }
func (e *ollamaEmbedder) Dim() int      { return e.dim }

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	// /api/embed takes a batch and is the current endpoint; /api/embeddings is
	// the older single-input one. The batch form is tried first and the old one
	// is the fallback, because an operator's Ollama may predate it.
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := doEmbed(req, "", &out); err != nil {
		return e.embedOneAtATime(ctx, texts)
	}
	if out.Error != "" || len(out.Embeddings) == 0 {
		return e.embedOneAtATime(ctx, texts)
	}
	return checkComplete(out.Embeddings, &e.dim)
}

func (e *ollamaEmbedder) embedOneAtATime(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, text := range texts {
		body, err := json.Marshal(map[string]any{"model": e.model, "prompt": text})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			e.base+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		var out struct {
			Embedding []float32 `json:"embedding"`
			Error     string    `json:"error"`
		}
		if err := doEmbed(req, "", &out); err != nil {
			return nil, err
		}
		if out.Error != "" {
			return nil, errors.New(out.Error)
		}
		vecs[i] = out.Embedding
	}
	return checkComplete(vecs, &e.dim)
}

// ------------------------------------------------------------------ Gemini ---

type geminiEmbedder struct {
	base, key, model string
	dim              int
}

func (e *geminiEmbedder) Model() string { return e.model }
func (e *geminiEmbedder) Dim() int      { return e.dim }

func (e *geminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqs := make([]map[string]any, 0, len(texts))
	model := e.model
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	for _, t := range texts {
		reqs = append(reqs, map[string]any{
			"model":   model,
			"content": map[string]any{"parts": []map[string]string{{"text": t}}},
		})
	}
	body, err := json.Marshal(map[string]any{"requests": reqs})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1beta/%s:batchEmbedContents", e.base, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// In a header, never the query string: a key in a URL ends up in the task's
	// error column, the event bus and the operator's phone the first time this
	// fails. That has happened twice in this codebase already.
	req.Header.Set("x-goog-api-key", e.key)

	var out struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := doEmbed(req, e.key, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, redactKey(errors.New(out.Error.Message), e.key)
	}

	vecs := make([][]float32, 0, len(out.Embeddings))
	for _, emb := range out.Embeddings {
		vecs = append(vecs, emb.Values)
	}
	return checkComplete(vecs, &e.dim)
}

// ------------------------------------------------------------------ shared ---

func doEmbed(req *http.Request, key string, into any) error {
	resp, err := embedClient.Do(req)
	if err != nil {
		return redactKey(err, key)
	}
	defer resp.Body.Close()

	// Bounded: a batch of vectors is large but not unbounded, and 64 MB is far
	// more than any sane embedding response.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return redactKey(fmt.Errorf("embedding request failed (%d): %s",
			resp.StatusCode, strings.TrimSpace(clipStr(string(raw), 400))), key)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("unreadable embedding response: %w", err)
	}
	return nil
}

// checkComplete rejects a partial batch and records the dimension.
//
// A missing vector cannot be tolerated: the caller pairs vectors with inputs by
// position, so a short or gappy result would attach one memory's embedding to
// another memory's text.
func checkComplete(vecs [][]float32, dim *int) ([][]float32, error) {
	if len(vecs) == 0 {
		return nil, errors.New("the embedding provider returned no vectors")
	}
	for i, v := range vecs {
		if len(v) == 0 {
			return nil, fmt.Errorf("the embedding provider returned no vector for input %d", i)
		}
		if *dim == 0 {
			*dim = len(v)
		}
		if len(v) != *dim {
			return nil, fmt.Errorf("the embedding provider returned %d dimensions for input %d "+
				"but %d for the others", len(v), i, *dim)
		}
	}
	return vecs, nil
}

func clipStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
