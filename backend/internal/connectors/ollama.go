package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// ollama uses the native /api/chat endpoint rather than the OpenAI shim: it
// reports real token counts, accepts images as a plain base64 array, and honours
// `format: json` on every model rather than only the tuned ones.
type ollama struct {
	p    protocol.Provider
	base string
	hc   *http.Client
}

func (c *ollama) ID() string   { return c.p.ID }
func (c *ollama) Name() string { return c.p.Name + " (" + c.p.Model + ")" }
func (c *ollama) Vision() bool { return c.p.Vision }

type olMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // raw base64, no data: prefix
}

// thinkDisabled is addressable so it can be sent as an explicit `false` rather
// than being dropped by omitempty.
var thinkDisabled = false

type olRequest struct {
	Model    string      `json:"model"`
	Messages []olMessage `json:"messages"`
	Stream   bool        `json:"stream"`
	Format   string      `json:"format,omitempty"`
	// Thinking models (qwen3, deepseek-r1, ornith, ...) emit their reasoning
	// into a separate `thinking` field and only then start `content`. With a
	// tiny token budget the whole budget goes to reasoning and `content` comes
	// back empty, which the caller below correctly treats as a failure — that
	// is why Probe's 8-token ping left the health dot red for a model that was
	// working fine. Only send the flag when the caller asks for it: for a real
	// agent turn the reasoning is what produces a well-formed action, so
	// disabling it globally to fix a health check would be a bad trade.
	Think   *bool `json:"think,omitempty"`
	Options struct {
		Temperature float64 `json:"temperature,omitempty"`
		NumPredict  int     `json:"num_predict,omitempty"`
	} `json:"options"`
}

type olResponse struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	Error           string `json:"error"`
}

func (c *ollama) Complete(ctx context.Context, req Request) (*Response, error) {
	body := olRequest{Model: c.p.Model, Stream: false}
	if req.DisableThinking {
		body.Think = &thinkDisabled
	}
	body.Options.Temperature = pick(req.Temperature, c.p.Temperature)
	body.Options.NumPredict = pickInt(req.MaxTokens, c.p.MaxTokens)
	if req.JSONOnly {
		body.Format = "json"
	}
	if req.System != "" {
		body.Messages = append(body.Messages, olMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msg := olMessage{Role: m.Role, Content: m.Text}
		if m.Image != "" {
			msg.Images = []string{m.Image}
		}
		body.Messages = append(body.Messages, msg)
	}

	start := time.Now()
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
	var out olResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", c.p.Name, err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("%s: %s", c.p.Name, out.Error)
	}
	// An answer with no text is a failure, not a result: returning it silently
	// stops the registry's fallback chain and burns the agent loop's parse
	// retries against an endpoint that is not going to produce an action.
	if strings.TrimSpace(out.Message.Content) == "" {
		return nil, fmt.Errorf("%s: %w (done %v)", c.p.Name, ErrEmptyCompletion, out.Done)
	}
	return &Response{
		Text:         out.Message.Content,
		Model:        firstNonEmpty(out.Model, c.p.Model),
		PromptTokens: out.PromptEvalCount,
		OutputTokens: out.EvalCount,
		Provider:     c.p.ID,
		Latency:      time.Since(start),
	}, nil
}

// ListOllamaModels is used by the admin panel to populate the model dropdown of
// a local Ollama host without the operator having to type names by hand.
func ListOllamaModels(ctx context.Context, base string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
