package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

const anthropicVersion = "2023-06-01"

// vertexAnthropicVersion is what Vertex expects in the body. It is a distinct
// constant from anthropicVersion on purpose: they are versions of different
// things and have moved independently.
const vertexAnthropicVersion = "vertex-2023-10-16"

// anthropic talks to the Messages API. Note the two shape differences from
// OpenAI that trip people up: the system prompt is a top-level field rather than
// a message, and images arrive as a base64 source block rather than a data URL.
type anthropic struct {
	p    protocol.Provider
	base string
	key  string
	hc   *http.Client
	// tokens is set when the provider is signed in with an account rather than
	// carrying an API key.
	tokens *GoogleTokenSource
	// vertex switches to Google's envelope for the same models.
	vertex bool
}

func (c *anthropic) ID() string   { return c.p.ID }
func (c *anthropic) Name() string { return c.p.Name + " (" + c.p.Model + ")" }
func (c *anthropic) Vision() bool { return c.p.Vision }

type anBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Source *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source,omitempty"`
}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

type anRequest struct {
	Model string `json:"model,omitempty"`
	// AnthropicVersion is required by Vertex and must not be sent to
	// Anthropic's own API, where the version travels as a header instead.
	AnthropicVersion string      `json:"anthropic_version,omitempty"`
	System           string      `json:"system,omitempty"`
	Messages         []anMessage `json:"messages"`
	MaxTokens        int         `json:"max_tokens"`
	Temperature      float64     `json:"temperature,omitempty"`
}

type anResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	body := anRequest{
		Model:       c.p.Model,
		System:      req.System,
		MaxTokens:   pickInt(req.MaxTokens, c.p.MaxTokens),
		Temperature: pick(req.Temperature, c.p.Temperature),
	}
	for _, m := range req.Messages {
		msg := anMessage{Role: m.Role}
		// Images first: the model attends to the instruction that follows them.
		if m.Image != "" {
			blk := anBlock{Type: "image"}
			blk.Source = &struct {
				Type      string `json:"type"`
				MediaType string `json:"media_type"`
				Data      string `json:"data"`
			}{Type: "base64", MediaType: mimeOr(m.ImageMime), Data: m.Image}
			msg.Content = append(msg.Content, blk)
		}
		if m.Text != "" {
			msg.Content = append(msg.Content, anBlock{Type: "text", Text: m.Text})
		}
		if len(msg.Content) == 0 {
			continue
		}
		body.Messages = append(body.Messages, msg)
	}

	// Vertex serves the same models behind a different envelope: the model is
	// named in the path rather than the body, and the API version travels in
	// the body rather than a header. Sending either in the wrong place is
	// rejected, so the two shapes are built explicitly rather than hoping one
	// request works for both.
	endpoint := c.base + "/v1/messages"
	if c.vertex {
		body.AnthropicVersion = vertexAnthropicVersion
		body.Model = ""
		endpoint = fmt.Sprintf("%s/publishers/anthropic/models/%s:rawPredict",
			strings.TrimSuffix(c.base, "/"), url.PathEscape(c.p.Model))
	}

	start := time.Now()
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if !c.vertex {
		httpReq.Header.Set("anthropic-version", anthropicVersion)
	}

	// A sign-in and a key are alternatives, not a fallback pair: sending both
	// lets a stale key mask a working sign-in.
	if c.tokens != nil {
		token, err := c.tokens.Token(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.p.Name, err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
	} else {
		httpReq.Header.Set("x-api-key", c.key)
	}

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.p.Name, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: http %d: %s", c.p.Name, resp.StatusCode, trim(raw))
	}
	var out anResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", c.p.Name, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", c.p.Name, out.Error.Message)
	}
	var text string
	for _, b := range out.Content {
		if b.Type == "text" {
			text += b.Text
		}
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%s: empty completion (stop_reason %s)", c.p.Name, out.StopReason)
	}
	return &Response{
		Text:         text,
		Model:        firstNonEmpty(out.Model, c.p.Model),
		PromptTokens: out.Usage.InputTokens,
		OutputTokens: out.Usage.OutputTokens,
		Provider:     c.p.ID,
		Latency:      time.Since(start),
	}, nil
}
