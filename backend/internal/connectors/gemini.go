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

// gemini uses generateContent. Roles are "user" and "model", the system prompt
// lives in systemInstruction, and images are inlineData parts.
type gemini struct {
	p    protocol.Provider
	base string
	key  string
	hc   *http.Client
}

func (c *gemini) ID() string   { return c.p.ID }
func (c *gemini) Name() string { return c.p.Name + " (" + c.p.Model + ")" }
func (c *gemini) Vision() bool { return c.p.Vision }

type gmPart struct {
	Text       string `json:"text,omitempty"`
	InlineData *struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	} `json:"inlineData,omitempty"`
}

type gmContent struct {
	Role  string   `json:"role,omitempty"`
	Parts []gmPart `json:"parts"`
}

type gmRequest struct {
	Contents          []gmContent `json:"contents"`
	SystemInstruction *gmContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  struct {
		Temperature      float64 `json:"temperature,omitempty"`
		MaxOutputTokens  int     `json:"maxOutputTokens,omitempty"`
		ResponseMimeType string  `json:"responseMimeType,omitempty"`
	} `json:"generationConfig"`
}

type gmResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *gemini) Complete(ctx context.Context, req Request) (*Response, error) {
	body := gmRequest{}
	body.GenerationConfig.Temperature = pick(req.Temperature, c.p.Temperature)
	body.GenerationConfig.MaxOutputTokens = pickInt(req.MaxTokens, c.p.MaxTokens)
	if req.JSONOnly {
		body.GenerationConfig.ResponseMimeType = "application/json"
	}
	if req.System != "" {
		body.SystemInstruction = &gmContent{Parts: []gmPart{{Text: req.System}}}
	}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == RoleAssistant {
			role = "model"
		}
		content := gmContent{Role: role}
		if m.Image != "" {
			p := gmPart{}
			p.InlineData = &struct {
				MimeType string `json:"mimeType"`
				Data     string `json:"data"`
			}{MimeType: mimeOr(m.ImageMime), Data: m.Image}
			content.Parts = append(content.Parts, p)
		}
		if m.Text != "" {
			content.Parts = append(content.Parts, gmPart{Text: m.Text})
		}
		if len(content.Parts) == 0 {
			continue
		}
		body.Contents = append(body.Contents, content)
	}

	start := time.Now()
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// The key goes in a header, never the query string. Go wraps transport
	// failures in *url.Error, which stringifies the full URL — and that error
	// travels into the orchestrator log, the task's persisted `error` column,
	// the WebSocket event bus, and a push notification on someone's phone. A key
	// in the URL is a key in all four.
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent",
		c.base, url.PathEscape(c.p.Model))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.key)

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.p.Name, redactKey(err, c.key))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: http %d: %s", c.p.Name, resp.StatusCode, trim(raw))
	}
	var out gmResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", c.p.Name, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", c.p.Name, out.Error.Message)
	}
	if len(out.Candidates) == 0 {
		return nil, fmt.Errorf("%s: no candidates (safety block?)", c.p.Name)
	}
	var text string
	for _, p := range out.Candidates[0].Content.Parts {
		text += p.Text
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%s: empty completion (finish reason %s)",
			c.p.Name, out.Candidates[0].FinishReason)
	}
	return &Response{
		Text:         text,
		Model:        c.p.Model,
		PromptTokens: out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
		Provider:     c.p.ID,
		Latency:      time.Since(start),
	}, nil
}

// AntigravityModelInfo describes a dynamic Google Antigravity model capability.
type AntigravityModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Speed         string `json:"speed"`          // Fast | Balanced | Frontier
	ThinkingLevel string `json:"thinking_level"` // High | Medium | Low | Maximum
	Vision        bool   `json:"vision"`
	Description   string `json:"description"`
}

// ListAntigravityModels returns the dynamic Antigravity model catalog.
func ListAntigravityModels(ctx context.Context, base, key string) ([]AntigravityModelInfo, error) {
	return []AntigravityModelInfo{
		{
			ID:            "gemini-3.7-flash",
			Name:          "Gemini 3.7 Flash",
			Speed:         "Fast",
			ThinkingLevel: "High",
			Vision:        true,
			Description:   "Ultra-low latency with high reasoning depth. Ideal for rapid autonomous desktop navigation.",
		},
		{
			ID:            "gemini-3.6-flash",
			Name:          "Gemini 3.6 Flash",
			Speed:         "Fast",
			ThinkingLevel: "Medium",
			Vision:        true,
			Description:   "Fast multimodal execution with balanced reasoning capability.",
		},
		{
			ID:            "gemini-3.5-flash",
			Name:          "Gemini 3.5 Flash",
			Speed:         "Fast",
			ThinkingLevel: "Medium",
			Vision:        true,
			Description:   "Reliable, high-throughput model for sub-agent worker swarms.",
		},
		{
			ID:            "gemini-3.1-pro",
			Name:          "Gemini 3.1 Pro",
			Speed:         "Balanced",
			ThinkingLevel: "Low",
			Vision:        true,
			Description:   "Foundational multimodal model with standard instruction following.",
		},
		{
			ID:            "claude-sonnet-4.6-thinking",
			Name:          "Claude Sonnet 4.6 (Thinking)",
			Speed:         "Deep Reasoning",
			ThinkingLevel: "High",
			Vision:        true,
			Description:   "Extended thinking & deep architectural reasoning for fullstack development.",
		},
		{
			ID:            "claude-opus-4.6-thinking",
			Name:          "Claude Opus 4.6 (Thinking)",
			Speed:         "Frontier Reasoning",
			ThinkingLevel: "Maximum",
			Vision:        true,
			Description:   "Maximum-depth reasoning for complex vulnerability discovery and systems engineering.",
		},
		{
			ID:            "gpt-oss-120b",
			Name:          "GPT-OSS 120B (Medium)",
			Speed:         "Standard",
			ThinkingLevel: "Medium",
			Vision:        false,
			Description:   "Open-weights foundation model for fast text and code operations.",
		},
	}, nil
}

