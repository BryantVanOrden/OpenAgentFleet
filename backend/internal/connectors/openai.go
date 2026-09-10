package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/image/webp"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	// ChatTemplateKwargs is how a self-hosted gateway is told to skip a
	// reasoning model's thinking pass. It is not part of the OpenAI API, so it
	// is only ever set for the openai-compatible kind -- api.openai.com
	// refuses a request carrying an argument it does not recognise.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
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
		// OpenAI nests the cache hit inside prompt_tokens_details, and it is
		// already counted in prompt_tokens. Several OpenAI-compatible servers
		// (vLLM, LiteLLM, Groq) copy the same shape, which is why this connector
		// reads it for the compatible kinds too; ones that omit it simply
		// report zero.
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
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
		imgB64, imgMime := pngIfWebP(m.Image, mimeOr(m.ImageMime))
		img.ImageURL = &struct {
			URL    string `json:"url"`
			Detail string `json:"detail,omitempty"`
		}{URL: "data:" + imgMime + ";base64," + imgB64, Detail: "high"}
		parts = append(parts, img)
		body.Messages = append(body.Messages, oaMessage{Role: m.Role, Content: parts})
	}
	if req.JSONOnly {
		body.ResponseFormat = map[string]any{"type": "json_object"}
		if req.JSONSchema != nil {
			body.ResponseFormat = map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{"name": "action", "schema": req.JSONSchema},
			}
		}
	}
	// A reasoning model asked for a short answer spends the whole budget
	// thinking and returns empty content, which every caller reads as an
	// unhealthy provider -- the 8-token health probe fails against it
	// permanently. Request.DisableThinking exists to prevent exactly that, but
	// until now only the native Ollama connector honoured it, so the flag was
	// silently dropped here and the retry in Registry.complete could never
	// change the outcome.
	if req.DisableThinking && c.p.Kind == protocol.ProviderCompatible {
		body.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
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

	resp, raw, err := doWhileLoading(ctx, c.hc, httpReq, buf)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.p.Name, err)
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
	// Both wrapped so Registry.complete's retry-without-thinking can recognise
	// them. Bare strings here meant the retry — whose whole point is a
	// reasoning model that thought its budget away — fired only for Ollama.
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: %w", c.p.Name, ErrEmptyCompletion)
	}
	if strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("%s: %w (finish_reason %s)",
			c.p.Name, ErrEmptyCompletion, out.Choices[0].FinishReason)
	}
	return &Response{
		Text:         out.Choices[0].Message.Content,
		Truncated:    out.Choices[0].FinishReason == "length",
		Model:        firstNonEmpty(out.Model, c.p.Model),
		PromptTokens: out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		CachedTokens: out.Usage.PromptTokensDetails.CachedTokens,
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

// defaultMaxTokens is the output limit when neither the request nor the
// provider sets one. It was 1024, which is a paragraph: a developer bot
// writing a 150-line file was cut off mid-action every time, and the loop
// read the truncated JSON as a refusal to act. Every connector shares this.
const defaultMaxTokens = 4096

func pickInt(a, b int) int {
	if a > 0 {
		return a
	}
	if b > 0 {
		return b
	}
	return defaultMaxTokens
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

// pngIfWebP re-encodes a WebP frame as PNG on the way to the model.
//
// The agent loop captures WebP because it is the cheapest thing to put in a
// prompt, and api.openai.com accepts it. Several self-hosted gateways that
// speak this same wire format do not: a llama.cpp server answers
// 400 "Failed to load image or audio file" and the whole task fails with
// "every model provider failed", which reads as a broken model rather than an
// unsupported container. Transcoding here keeps that decision local to the
// request — the artifact store still keeps the original WebP, so the audit
// trail and the recorder are untouched.
//
// Anything that does not decode is passed through unchanged rather than
// dropped: a provider that understands the original is better served by it
// than by an error.
func pngIfWebP(b64, mime string) (string, string) {
	if b64 == "" || !strings.EqualFold(mime, "image/webp") {
		return b64, mime
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return b64, mime
	}
	img, err := webp.Decode(bytes.NewReader(raw))
	if err != nil {
		return b64, mime
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return b64, mime
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), "image/png"
}

// doWhileLoading sends the request and, while the server answers 503 "the
// model is loading", waits and sends it again until the context gives up.
//
// A gateway that pages a large model in on first use answers every request
// during the load with 503 rather than holding the connection (seen on a
// 94 GB model: about three minutes of "Loading model" before the first token).
// Treating that as a failure fell through to the next provider or failed the
// step; the operator's model was never broken, only not yet resident. The
// probe's own deadline still bounds the wait, so a model that never loads
// still reports as one.
func doWhileLoading(ctx context.Context, hc *http.Client, req *http.Request, body []byte) (*http.Response, []byte, error) {
	wait := 5 * time.Second
	for {
		req.Body = io.NopCloser(bytes.NewReader(body))
		resp, err := hc.Do(req)
		if err != nil {
			return nil, nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		if !isLoading(resp.StatusCode, raw) {
			return resp, raw, nil
		}
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("model still loading when the deadline passed: %w", ctx.Err())
		case <-time.After(wait):
		}
		if wait < 15*time.Second {
			wait += 5 * time.Second
		}
	}
}

func isLoading(status int, raw []byte) bool {
	if status != http.StatusServiceUnavailable {
		return false
	}
	lower := strings.ToLower(string(raw))
	return strings.Contains(lower, "loading model") || strings.Contains(lower, "unavailable_error")
}
