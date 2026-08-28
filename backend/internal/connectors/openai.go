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

// openAICompatible covers api.openai.com and every gateway that speaks the same
// /chat/completions shape: vLLM, LocalAI, LiteLLM, OpenRouter, Ollama's /v1.
type openAICompatible struct {
	p    protocol.Provider
	base string
	key  string
	hc   *http.Client
}

func (c *openAICompatible) ID() string   { return c.p.ID }
func (c *openAICompatible) Name() string { return c.p.Name + " (" + c.p.Model + ")" }
func (c *openAICompatible) Vision() bool { return c.p.Vision }

type oaContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url,omitempty"`
}

type oaMessage struct {
	Role string `json:"role"`
	// Either a plain string or a content-part array, so this stays `any`.
	Content any `json:"content"`
}

type oaRequest struct {
	Model          string      `json:"model"`
	Messages       []oaMessage `json:"messages"`
	Temperature    float64     `json:"temperature,omitempty"`
	MaxTokens      int         `json:"max_tokens,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

type oaResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (c *openAICompatible) Complete(ctx context.Context, req Request) (*Response, error) {
	body := oaRequest{
		Model:       c.p.Model,
		Temperature: pick(req.Temperature, c.p.Temperature),
		MaxTokens:   pickInt(req.MaxTokens, c.p.MaxTokens),
	}
	if req.System != "" {
		body.Messages = append(body.Messages, oaMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		if m.Image == "" {
			body.Messages = append(body.Messages, oaMessage{Role: m.Role, Content: m.Text})
			continue
		}
		parts := []oaContent{}
		if m.Text != "" {
			parts = append(parts, oaContent{Type: "text", Text: m.Text})
		}
		img := oaContent{Type: "image_url"}
		img.ImageURL = &struct {
			URL    string `json:"url"`
			Detail string `json:"detail,omitempty"`
		}{URL: "data:" + mimeOr(m.ImageMime) + ";base64," + m.Image, Detail: "high"}
		parts = append(parts, img)
		body.Messages = append(body.Messages, oaMessage{Role: m.Role, Content: parts})
	}
	if req.JSONOnly {
		body.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}

	start := time.Now()
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.key)
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

	var out oaResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", c.p.Name, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", c.p.Name, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty completion", c.p.Name)
	}
	if strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("%s: empty completion (finish_reason %s)",
			c.p.Name, out.Choices[0].FinishReason)
	}
	return &Response{
		Text:         out.Choices[0].Message.Content,
		Model:        firstNonEmpty(out.Model, c.p.Model),
		PromptTokens: out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		Provider:     c.p.ID,
		Latency:      time.Since(start),
	}, nil
}

func pick(a, b float64) float64 {
	if a > 0 {
		return a
	}
	return b
}

func pickInt(a, b int) int {
	if a > 0 {
		return a
	}
	if b > 0 {
		return b
	}
	return 1024
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func trim(b []byte) string {
	const max = 400
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}
