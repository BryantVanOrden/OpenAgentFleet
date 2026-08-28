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

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

const anthropicVersion = "2023-06-01"

// anthropic talks to the Messages API. Note the two shape differences from
// OpenAI that trip people up: the system prompt is a top-level field rather than
// a message, and images arrive as a base64 source block rather than a data URL.
type anthropic struct {
	p    protocol.Provider
	base string
	key  string
	hc   *http.Client
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
	Model       string      `json:"model"`
	System      string      `json:"system,omitempty"`
	Messages    []anMessage `json:"messages"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature float64     `json:"temperature,omitempty"`
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

	start := time.Now()
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("x-api-key", c.key)

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
