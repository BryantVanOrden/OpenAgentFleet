// Package connectors normalises multimodal chat completion across every model
// backend the platform supports: OpenAI, Anthropic, Google Gemini, Ollama and
// any OpenAI-compatible gateway (vLLM, LocalAI, LiteLLM, OpenRouter...).
//
// The agent loop needs exactly one thing from a model: given a screenshot plus
// some text, return a single JSON action object. Rather than depend on each
// vendor's tool-calling dialect — which is where portability usually dies — the
// connectors request plain text and the loop parses strict JSON out of it. Where
// a provider offers a native JSON mode we switch it on as an extra guard rail.
package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Role values used in Request.Messages.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn. Image is base64 without a data: prefix.
type Message struct {
	Role      string
	Text      string
	Image     string
	ImageMime string // defaults to image/webp
}

type Request struct {
	System      string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	// JSONOnly asks the provider for strict JSON output where it supports it.
	JSONOnly bool
	// DisableThinking suppresses a reasoning model's separate thinking pass.
	// Only set it where the reasoning is genuinely unwanted — a health ping
	// with a tiny token budget, say. Leave it off for agent turns: the
	// reasoning is what produces a well-formed action.
	DisableThinking bool
}

type Response struct {
	Text         string
	Model        string
	PromptTokens int
	OutputTokens int
	Provider     string
	Latency      time.Duration
}

// Connector is one configured model endpoint.
type Connector interface {
	// ID is the provider row id, for attribution on task steps.
	ID() string
	Name() string
	Vision() bool
	Complete(ctx context.Context, req Request) (*Response, error)
}

// ErrNoProvider is returned when nothing is configured or every provider failed.
var ErrNoProvider = errors.New("no usable model provider")

// Build turns a stored provider row into a live connector. apiKey has already
// been resolved from the vault by the caller.
func Build(p protocol.Provider, apiKey string, hc *http.Client) (Connector, error) {
	if hc == nil {
		hc = defaultClient()
	}
	base := strings.TrimSuffix(p.BaseURL, "/")
	switch p.Kind {
	case protocol.ProviderOpenAI:
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		return &openAICompatible{p: p, base: base, key: apiKey, hc: hc}, nil

	case protocol.ProviderCompatible:
		if base == "" {
			return nil, fmt.Errorf("provider %s: base_url is required for openai-compatible", p.Name)
		}
		return &openAICompatible{p: p, base: base, key: apiKey, hc: hc}, nil

	case protocol.ProviderOllama:
		if base == "" {
			base = "http://localhost:11434"
		}
		return &ollama{p: p, base: base, hc: hc}, nil

	case protocol.ProviderAnthropic:
		if base == "" {
			base = "https://api.anthropic.com"
		}
		return &anthropic{p: p, base: base, key: apiKey, hc: hc}, nil

	case protocol.ProviderGemini, protocol.ProviderAntigravity:
		if base == "" {
			base = "https://generativelanguage.googleapis.com"
		}
		return &gemini{p: p, base: base, key: apiKey, hc: hc}, nil

	default:
		return nil, fmt.Errorf("unknown provider kind %q", p.Kind)
	}
}

func defaultClient() *http.Client {
	return &http.Client{Timeout: 3 * time.Minute}
}

func mimeOr(m string) string {
	if m == "" {
		return "image/webp"
	}
	return m
}
