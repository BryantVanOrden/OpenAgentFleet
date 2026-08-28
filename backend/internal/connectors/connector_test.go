package connectors

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ----------------------------------------------------------------- harness ---

// capture records exactly what a connector put on the wire. `done` is closed
// once the handler has finished filling it in, so readers get a happens-before
// edge instead of relying on the HTTP round trip for synchronisation.
type capture struct {
	done    chan struct{}
	once    sync.Once
	method  string
	path    string
	escaped string
	query   url.Values
	header  http.Header
	raw     []byte
	body    map[string]any
}

// ready blocks until the request has been recorded and returns the capture.
func (c *capture) ready() *capture {
	<-c.done
	return c
}

// fakeProvider stands up an HTTP endpoint that records the request and replies
// with a canned status and body.
func fakeProvider(t *testing.T, status int, respBody string) (*httptest.Server, *capture) {
	t.Helper()
	cp := &capture{done: make(chan struct{})}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		cp.method, cp.path, cp.escaped = r.Method, r.URL.Path, r.URL.EscapedPath()
		cp.query = r.URL.Query()
		cp.header = r.Header.Clone()
		cp.raw = raw
		_ = json.Unmarshal(raw, &cp.body)
		cp.once.Do(func() { close(cp.done) })
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(func() {
		ts.Close()
		// Unblock any reader waiting on a request that never arrived.
		cp.once.Do(func() { close(cp.done) })
	})
	return ts, cp
}

// dig walks a decoded JSON document with a dotted path; numeric segments index
// into arrays. "messages.1.content.0.type" and so on.
func dig(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, seg := range strings.Split(path, ".") {
		switch cur := v.(type) {
		case map[string]any:
			nv, ok := cur[seg]
			if !ok {
				return nil, false
			}
			v = nv
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(cur) {
				return nil, false
			}
			v = cur[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func wantStr(t *testing.T, cp *capture, path, want string) {
	t.Helper()
	cp = cp.ready()
	got, ok := dig(cp.body, path)
	if !ok {
		t.Errorf("%s is missing from the request body:\n%s", path, cp.ready().raw)
		return
	}
	s, isStr := got.(string)
	if !isStr {
		t.Errorf("%s = %#v, want the string %q", path, got, want)
		return
	}
	if s != want {
		t.Errorf("%s = %q, want %q", path, s, want)
	}
}

func wantNum(t *testing.T, cp *capture, path string, want float64) {
	t.Helper()
	cp = cp.ready()
	got, ok := dig(cp.body, path)
	if !ok {
		t.Errorf("%s is missing from the request body:\n%s", path, cp.ready().raw)
		return
	}
	n, isNum := got.(float64)
	if !isNum || n != want {
		t.Errorf("%s = %#v, want %v", path, got, want)
	}
}

func wantAbsent(t *testing.T, cp *capture, path string) {
	t.Helper()
	cp = cp.ready()
	if got, ok := dig(cp.body, path); ok {
		t.Errorf("%s should not be present, got %#v", path, got)
	}
}

func build(t *testing.T, kind protocol.ProviderKind, ts *httptest.Server, tweak func(*protocol.Provider)) Connector {
	t.Helper()
	p := protocol.Provider{
		ID: "prov-1", Name: "TestProvider", Kind: kind, BaseURL: ts.URL,
		Model: "test-model", Vision: true, Temperature: 0.25, MaxTokens: 512,
	}
	if tweak != nil {
		tweak(&p)
	}
	c, err := Build(p, "sk-test-key", ts.Client())
	if err != nil {
		t.Fatalf("Build(%s) error = %v", kind, err)
	}
	return c
}

// visionRequest is the shape the agent loop actually sends every turn.
func visionRequest() Request {
	return Request{
		System:   "You are a computer-use agent.",
		JSONOnly: true,
		Messages: []Message{{
			Role:      RoleUser,
			Text:      "What is the next action?",
			Image:     "QUJDRA==",
			ImageMime: "", // exercises the image/webp default
		}},
	}
}

// ------------------------------------------------------------------ openai ---

const oaOK = `{"model":"gpt-4o-2024-11-20",
	"choices":[{"message":{"content":"{\"action\":\"done\"}"},"finish_reason":"stop"}],
	"usage":{"prompt_tokens":1234,"completion_tokens":56}}`

func TestOpenAIRequestShape(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderOpenAI, ts, nil)

	resp, err := c.Complete(context.Background(), visionRequest())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if cp.ready().method != http.MethodPost || cp.ready().path != "/chat/completions" {
		t.Errorf("hit %s %s, want POST /chat/completions", cp.method, cp.ready().path)
	}
	if got := cp.ready().header.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := cp.ready().header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	wantStr(t, cp, "model", "test-model")
	wantNum(t, cp, "temperature", 0.25)
	wantNum(t, cp, "max_tokens", 512)

	// The system prompt is an ordinary message with role "system".
	wantStr(t, cp, "messages.0.role", "system")
	wantStr(t, cp, "messages.0.content", "You are a computer-use agent.")

	// The image becomes a data: URL inside an image_url content part.
	wantStr(t, cp, "messages.1.role", "user")
	wantStr(t, cp, "messages.1.content.0.type", "text")
	wantStr(t, cp, "messages.1.content.0.text", "What is the next action?")
	wantStr(t, cp, "messages.1.content.1.type", "image_url")
	wantStr(t, cp, "messages.1.content.1.image_url.url", "data:image/webp;base64,QUJDRA==")
	wantStr(t, cp, "messages.1.content.1.image_url.detail", "high")

	// JSONOnly switches on the native JSON mode.
	wantStr(t, cp, "response_format.type", "json_object")

	if resp.Text != `{"action":"done"}` {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Model != "gpt-4o-2024-11-20" {
		t.Errorf("Model = %q, want the model the server reported", resp.Model)
	}
	if resp.PromptTokens != 1234 || resp.OutputTokens != 56 {
		t.Errorf("tokens = %d/%d, want 1234/56", resp.PromptTokens, resp.OutputTokens)
	}
	if resp.Provider != "prov-1" {
		t.Errorf("Provider = %q, want the provider row id", resp.Provider)
	}
}

func TestOpenAIHonoursExplicitMimeAndTextOnlyTurns(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderOpenAI, ts, nil)

	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Text: "hello"},
			{Role: RoleAssistant, Text: "hi"},
			{Role: RoleUser, Image: "SU1H", ImageMime: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	// No system prompt means no system message at all.
	wantStr(t, cp, "messages.0.role", "user")
	wantStr(t, cp, "messages.0.content", "hello")
	wantStr(t, cp, "messages.1.role", "assistant")
	wantStr(t, cp, "messages.1.content", "hi")
	// An image-only turn has exactly one part.
	wantStr(t, cp, "messages.2.content.0.type", "image_url")
	wantStr(t, cp, "messages.2.content.0.image_url.url", "data:image/png;base64,SU1H")
	wantAbsent(t, cp, "messages.2.content.1")
	// JSONOnly off means no response_format at all.
	wantAbsent(t, cp, "response_format")
}

func TestOpenAICompatibleRequiresABaseURL(t *testing.T) {
	_, err := Build(protocol.Provider{
		Name: "vLLM", Kind: protocol.ProviderCompatible, Model: "m",
	}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "base_url is required") {
		t.Fatalf("Build() error = %v, want a base_url error", err)
	}
}

func TestOpenAICompatibleUsesTheSameShape(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderCompatible, ts, nil)
	if _, err := c.Complete(context.Background(), visionRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if cp.ready().path != "/chat/completions" {
		t.Errorf("path = %q", cp.ready().path)
	}
	wantStr(t, cp, "messages.1.content.1.image_url.url", "data:image/webp;base64,QUJDRA==")
}

func TestBuildTrimsATrailingSlashFromBaseURL(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderOpenAI, ts, func(p *protocol.Provider) {
		p.BaseURL = ts.URL + "/"
	})
	if _, err := c.Complete(context.Background(), visionRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if cp.ready().path != "/chat/completions" {
		t.Errorf("path = %q, want no doubled slash", cp.ready().path)
	}
}

// ------------------------------------------------------------------ ollama ---

const olOK = `{"model":"llava:13b","message":{"content":"{\"action\":\"wait\"}"},
	"done":true,"prompt_eval_count":987,"eval_count":21}`

func TestOllamaRequestShape(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, olOK)
	c := build(t, protocol.ProviderOllama, ts, nil)

	resp, err := c.Complete(context.Background(), visionRequest())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if cp.ready().method != http.MethodPost || cp.ready().path != "/api/chat" {
		t.Errorf("hit %s %s, want POST /api/chat", cp.method, cp.ready().path)
	}
	// Ollama has no API key; nothing should be sent.
	if got := cp.ready().header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want it unset for ollama", got)
	}

	wantStr(t, cp, "model", "test-model")
	wantStr(t, cp, "format", "json")
	wantNum(t, cp, "options.temperature", 0.25)
	wantNum(t, cp, "options.num_predict", 512)
	if stream, _ := dig(cp.body, "stream"); stream != false {
		t.Errorf("stream = %#v, want false", stream)
	}

	wantStr(t, cp, "messages.0.role", "system")
	wantStr(t, cp, "messages.0.content", "You are a computer-use agent.")
	wantStr(t, cp, "messages.1.role", "user")
	wantStr(t, cp, "messages.1.content", "What is the next action?")

	// The image is a BARE base64 string in an array — no data: prefix, which is
	// the difference from the OpenAI shape that breaks vision silently.
	wantStr(t, cp, "messages.1.images.0", "QUJDRA==")
	if img, ok := dig(cp.body, "messages.1.images.0"); ok {
		if s, _ := img.(string); strings.HasPrefix(s, "data:") {
			t.Errorf("ollama image carries a data: prefix: %q", s)
		}
	}
	if imgs, ok := dig(cp.body, "messages.1.images"); ok {
		if arr, isArr := imgs.([]any); !isArr || len(arr) != 1 {
			t.Errorf("images = %#v, want a one-element array", imgs)
		}
	}
	// A message with no image must not carry an empty images array.
	wantAbsent(t, cp, "messages.0.images")

	// Token counts come from ollama's own eval counters.
	if resp.PromptTokens != 987 || resp.OutputTokens != 21 {
		t.Errorf("tokens = %d/%d, want 987/21", resp.PromptTokens, resp.OutputTokens)
	}
	if resp.Text != `{"action":"wait"}` {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Model != "llava:13b" {
		t.Errorf("Model = %q", resp.Model)
	}
}

func TestOllamaOmitsFormatWhenNotJSONOnly(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, olOK)
	c := build(t, protocol.ProviderOllama, ts, nil)
	if _, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Text: "hi"}},
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	wantAbsent(t, cp, "format")
}

func TestOllamaSurfacesABodyLevelError(t *testing.T) {
	ts, _ := fakeProvider(t, http.StatusOK, `{"error":"model 'llava' not found"}`)
	c := build(t, protocol.ProviderOllama, ts, nil)
	_, err := c.Complete(context.Background(), visionRequest())
	if err == nil {
		t.Fatal("Complete() = nil error, want the body-level error surfaced")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "TestProvider") {
		t.Errorf("error = %q", err)
	}
}

// --------------------------------------------------------------- anthropic ---

const anOK = `{"model":"claude-sonnet-4-5",
	"content":[{"type":"text","text":"{\"action\":"},{"type":"text","text":"\"done\"}"}],
	"stop_reason":"end_turn","usage":{"input_tokens":300,"output_tokens":12}}`

func TestAnthropicRequestShape(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, anOK)
	c := build(t, protocol.ProviderAnthropic, ts, nil)

	resp, err := c.Complete(context.Background(), visionRequest())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if cp.ready().method != http.MethodPost || cp.ready().path != "/v1/messages" {
		t.Errorf("hit %s %s, want POST /v1/messages", cp.method, cp.ready().path)
	}
	if got := cp.ready().header.Get("anthropic-version"); got != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", got, anthropicVersion)
	}
	if got := cp.ready().header.Get("x-api-key"); got != "sk-test-key" {
		t.Errorf("x-api-key = %q", got)
	}
	if got := cp.ready().header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want the key in x-api-key only", got)
	}

	// The system prompt is a TOP-LEVEL field, not a message.
	wantStr(t, cp, "system", "You are a computer-use agent.")
	msgs, _ := dig(cp.body, "messages")
	arr, ok := msgs.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("messages = %#v, want exactly one user message", msgs)
	}
	for i := range arr {
		if role, _ := dig(cp.body, "messages."+strconv.Itoa(i)+".role"); role == "system" {
			t.Error("the system prompt leaked into the messages array")
		}
	}

	// Image first as a base64 source block, then the instruction.
	wantStr(t, cp, "messages.0.role", "user")
	wantStr(t, cp, "messages.0.content.0.type", "image")
	wantStr(t, cp, "messages.0.content.0.source.type", "base64")
	wantStr(t, cp, "messages.0.content.0.source.media_type", "image/webp")
	wantStr(t, cp, "messages.0.content.0.source.data", "QUJDRA==")
	wantStr(t, cp, "messages.0.content.1.type", "text")
	wantStr(t, cp, "messages.0.content.1.text", "What is the next action?")
	wantNum(t, cp, "max_tokens", 512)
	wantNum(t, cp, "temperature", 0.25)

	// Text blocks are concatenated in order.
	if resp.Text != `{"action":"done"}` {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.PromptTokens != 300 || resp.OutputTokens != 12 {
		t.Errorf("tokens = %d/%d, want 300/12", resp.PromptTokens, resp.OutputTokens)
	}
}

func TestAnthropicSkipsEmptyTurns(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, anOK)
	c := build(t, protocol.ProviderAnthropic, ts, nil)
	if _, err := c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Text: "hello"},
			{Role: RoleAssistant}, // nothing to say: must be dropped
			{Role: RoleUser, Text: "still there"},
		},
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	msgs, _ := dig(cp.body, "messages")
	arr, _ := msgs.([]any)
	if len(arr) != 2 {
		t.Fatalf("got %d messages, want 2 (the empty turn should be dropped)", len(arr))
	}
	wantStr(t, cp, "messages.1.content.0.text", "still there")
}

func TestAnthropicOmitsSystemWhenEmpty(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, anOK)
	c := build(t, protocol.ProviderAnthropic, ts, nil)
	if _, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Text: "hi"}},
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	wantAbsent(t, cp, "system")
}

// ------------------------------------------------------------------ gemini ---

const gmOK = `{"candidates":[{"content":{"parts":[{"text":"{\"action\":\"done\"}"}]},
	"finishReason":"STOP"}],
	"usageMetadata":{"promptTokenCount":77,"candidatesTokenCount":9}}`

func TestGeminiRequestShape(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, gmOK)
	c := build(t, protocol.ProviderGemini, ts, func(p *protocol.Provider) {
		p.Model = "gemini-2.0-flash"
	})

	req := visionRequest()
	req.Messages = append(req.Messages, Message{Role: RoleAssistant, Text: "I clicked Save."})
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if cp.ready().path != "/v1beta/models/gemini-2.0-flash:generateContent" {
		t.Errorf("path = %q", cp.ready().path)
	}
	if got := cp.ready().query.Get("key"); got != "sk-test-key" {
		t.Errorf("key query param = %q", got)
	}

	// The system prompt goes to systemInstruction, never to contents.
	wantStr(t, cp, "systemInstruction.parts.0.text", "You are a computer-use agent.")
	wantAbsent(t, cp, "contents.0.parts.0.role")

	// Images are inlineData parts.
	wantStr(t, cp, "contents.0.role", "user")
	wantStr(t, cp, "contents.0.parts.0.inlineData.mimeType", "image/webp")
	wantStr(t, cp, "contents.0.parts.0.inlineData.data", "QUJDRA==")
	wantStr(t, cp, "contents.0.parts.1.text", "What is the next action?")

	// Assistant turns are role "model", not "assistant".
	wantStr(t, cp, "contents.1.role", "model")
	wantStr(t, cp, "contents.1.parts.0.text", "I clicked Save.")

	wantStr(t, cp, "generationConfig.responseMimeType", "application/json")
	wantNum(t, cp, "generationConfig.temperature", 0.25)
	wantNum(t, cp, "generationConfig.maxOutputTokens", 512)

	if resp.Text != `{"action":"done"}` {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.PromptTokens != 77 || resp.OutputTokens != 9 {
		t.Errorf("tokens = %d/%d, want 77/9", resp.PromptTokens, resp.OutputTokens)
	}
}

func TestGeminiEscapesTheModelInThePath(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, gmOK)
	c := build(t, protocol.ProviderGemini, ts, func(p *protocol.Provider) {
		p.Model = "models/weird name"
	})
	if _, err := c.Complete(context.Background(), visionRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if esc := cp.ready().escaped; strings.ContainsAny(esc, " ") || !strings.Contains(esc, "%20") {
		t.Errorf("the model name was not escaped into the path: %q", esc)
	}
}

func TestGeminiOmitsSystemInstructionWhenEmpty(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, gmOK)
	c := build(t, protocol.ProviderGemini, ts, nil)
	if _, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Text: "hi"}},
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	wantAbsent(t, cp, "systemInstruction")
	wantAbsent(t, cp, "generationConfig.responseMimeType")
}

// -------------------------------------------------- shared failure contract ---

// everyKind is the table every provider-agnostic contract is checked against.
var everyKind = []protocol.ProviderKind{
	protocol.ProviderOpenAI,
	protocol.ProviderCompatible,
	protocol.ProviderOllama,
	protocol.ProviderAnthropic,
	protocol.ProviderGemini,
}

func TestHTTPErrorMentionsTheProvider(t *testing.T) {
	for _, kind := range everyKind {
		for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests,
			http.StatusBadRequest, http.StatusInternalServerError} {
			t.Run(string(kind)+"/"+strconv.Itoa(status), func(t *testing.T) {
				ts, _ := fakeProvider(t, status, `{"error":{"message":"nope"}}`)
				c := build(t, kind, ts, nil)
				_, err := c.Complete(context.Background(), visionRequest())
				if err == nil {
					t.Fatalf("http %d produced no error", status)
				}
				if !strings.Contains(err.Error(), "TestProvider") {
					t.Errorf("error %q does not name the provider", err)
				}
				if !strings.Contains(err.Error(), strconv.Itoa(status)) {
					t.Errorf("error %q does not carry the status code", err)
				}
			})
		}
	}
}

// TestEmptyCompletionIsAnError pins the contract the fallback chain depends on:
// a provider that answers with no usable text must return an error so
// Registry.Complete falls through to the next provider. A silent empty string
// instead burns the agent loop's parse-error budget against a dead endpoint.
func TestEmptyCompletionIsAnError(t *testing.T) {
	cases := []struct {
		kind protocol.ProviderKind
		body string
	}{
		{protocol.ProviderOpenAI, `{"choices":[]}`},
		{protocol.ProviderOpenAI, `{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`},
		{protocol.ProviderCompatible, `{"choices":[{"message":{"content":"   "}}]}`},
		{protocol.ProviderOllama, `{"model":"llava","message":{"content":""},"done":true}`},
		{protocol.ProviderOllama, `{"model":"llava","message":{"content":"  \n "},"done":true}`},
		{protocol.ProviderAnthropic, `{"content":[],"stop_reason":"max_tokens"}`},
		{protocol.ProviderAnthropic, `{"content":[{"type":"text","text":""}]}`},
		{protocol.ProviderAnthropic, `{"content":[{"type":"text","text":" \n\t "}]}`},
		{protocol.ProviderAnthropic, `{"content":[{"type":"thinking","text":"no answer"}]}`},
		{protocol.ProviderGemini, `{"candidates":[]}`},
		{protocol.ProviderGemini, `{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind)+"/"+tc.body, func(t *testing.T) {
			ts, _ := fakeProvider(t, http.StatusOK, tc.body)
			c := build(t, tc.kind, ts, nil)
			resp, err := c.Complete(context.Background(), visionRequest())
			if err == nil {
				t.Fatalf("an empty completion returned success with Text=%q; the fallback chain will not engage", resp.Text)
			}
			if !strings.Contains(err.Error(), "TestProvider") {
				t.Errorf("error %q does not name the provider", err)
			}
		})
	}
}

func TestMalformedResponseBodyIsAnError(t *testing.T) {
	for _, kind := range everyKind {
		t.Run(string(kind), func(t *testing.T) {
			ts, _ := fakeProvider(t, http.StatusOK, `this is not json`)
			c := build(t, kind, ts, nil)
			if _, err := c.Complete(context.Background(), visionRequest()); err == nil {
				t.Fatal("a non-JSON body produced no error")
			}
		})
	}
}

func TestCancelledContextIsAnError(t *testing.T) {
	for _, kind := range everyKind {
		t.Run(string(kind), func(t *testing.T) {
			ts, _ := fakeProvider(t, http.StatusOK, oaOK)
			c := build(t, kind, ts, nil)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := c.Complete(ctx, visionRequest()); err == nil {
				t.Fatal("a cancelled context produced no error")
			}
		})
	}
}

// ------------------------------------------------------------------- Build ---

func TestBuildDefaultsAndIdentity(t *testing.T) {
	cases := []struct {
		kind    protocol.ProviderKind
		wantErr bool
	}{
		{protocol.ProviderOpenAI, false},
		{protocol.ProviderOllama, false},
		{protocol.ProviderAnthropic, false},
		{protocol.ProviderGemini, false},
		{protocol.ProviderKind("bogus"), true},
		{protocol.ProviderKind(""), true},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			c, err := Build(protocol.Provider{
				ID: "p9", Name: "Prod", Kind: tc.kind, Model: "m1", Vision: true,
			}, "key", nil) // no base URL: the vendor default must be filled in
			if tc.wantErr {
				if err == nil {
					t.Fatal("Build() = nil error, want an unknown-kind error")
				}
				if !strings.Contains(err.Error(), "unknown provider kind") {
					t.Errorf("error = %q", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if c.ID() != "p9" {
				t.Errorf("ID() = %q", c.ID())
			}
			if c.Name() != "Prod (m1)" {
				t.Errorf("Name() = %q, want %q", c.Name(), "Prod (m1)")
			}
			if !c.Vision() {
				t.Error("Vision() = false, want it to follow the provider row")
			}
		})
	}
}

func TestMimeOr(t *testing.T) {
	if got := mimeOr(""); got != "image/webp" {
		t.Errorf("mimeOr(\"\") = %q, want image/webp", got)
	}
	if got := mimeOr("image/png"); got != "image/png" {
		t.Errorf("mimeOr(image/png) = %q", got)
	}
}

func TestPickAndPickInt(t *testing.T) {
	if got := pick(0.5, 0.9); got != 0.5 {
		t.Errorf("pick(0.5, 0.9) = %v, want the request value", got)
	}
	if got := pick(0, 0.9); got != 0.9 {
		t.Errorf("pick(0, 0.9) = %v, want the provider default", got)
	}
	if got := pickInt(10, 20); got != 10 {
		t.Errorf("pickInt(10, 20) = %d", got)
	}
	if got := pickInt(0, 20); got != 20 {
		t.Errorf("pickInt(0, 20) = %d", got)
	}
	if got := pickInt(0, 0); got != 1024 {
		t.Errorf("pickInt(0, 0) = %d, want the 1024 floor", got)
	}
}

func TestHasImage(t *testing.T) {
	if hasImage(Request{Messages: []Message{{Text: "a"}}}) {
		t.Error("hasImage() = true for a text-only request")
	}
	if !hasImage(Request{Messages: []Message{{Text: "a"}, {Image: "b"}}}) {
		t.Error("hasImage() = false when a turn carries a screenshot")
	}
}
