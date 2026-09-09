package connectors

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Registry.complete retries an empty completion once with DisableThinking set —
// but only when it can RECOGNISE the emptiness. These pin that every connector
// wraps its empty-completion error in ErrEmptyCompletion; bare strings here
// once made the retry Ollama-only, which is the same silently-dropped-contract
// class as the DisableThinking flag itself.

func TestEmptyCompletionIsRecognisableFromEveryConnector(t *testing.T) {
	cases := []struct {
		name string
		kind protocol.ProviderKind
		body string
	}{
		{"openai no choices", protocol.ProviderOpenAI,
			`{"model":"gpt-4o","choices":[]}`},
		{"openai blank content", protocol.ProviderCompatible,
			`{"model":"m","choices":[{"message":{"content":"  "},"finish_reason":"length"}]}`},
		{"anthropic blank text", protocol.ProviderAnthropic,
			`{"model":"c","content":[{"type":"text","text":" "}],"stop_reason":"max_tokens"}`},
		{"gemini blank parts", protocol.ProviderGemini,
			`{"candidates":[{"content":{"parts":[{"text":""}]},"finishReason":"MAX_TOKENS"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, _ := fakeProvider(t, http.StatusOK, tc.body)
			c := build(t, tc.kind, ts, nil)
			_, err := c.Complete(context.Background(), Request{
				Messages: []Message{{Role: RoleUser, Text: "ping"}},
			})
			if !errors.Is(err, ErrEmptyCompletion) {
				t.Errorf("err = %v, want errors.Is(_, ErrEmptyCompletion)", err)
			}
		})
	}
}

// Gemini cannot portably switch its thinking pass off (thinkingBudget=0 is a
// 400 on 2.5-pro, the whole field is a 400 pre-2.5), so DisableThinking buys
// output headroom instead: the probe's 8 tokens must not be eaten by thoughts.
func TestGeminiDisableThinkingRaisesTheOutputFloor(t *testing.T) {
	const gmPing = `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`

	t.Run("probe-sized budget is raised", func(t *testing.T) {
		ts, cp := fakeProvider(t, http.StatusOK, gmPing)
		c := build(t, protocol.ProviderGemini, ts, nil)
		if _, err := c.Complete(context.Background(), Request{
			Messages:        []Message{{Role: RoleUser, Text: "ping"}},
			MaxTokens:       8,
			DisableThinking: true,
		}); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		wantNum(t, cp, "generationConfig.maxOutputTokens", 512)
	})

	t.Run("a real budget is left alone", func(t *testing.T) {
		ts, cp := fakeProvider(t, http.StatusOK, gmPing)
		c := build(t, protocol.ProviderGemini, ts, nil)
		if _, err := c.Complete(context.Background(), Request{
			Messages:        []Message{{Role: RoleUser, Text: "ping"}},
			MaxTokens:       4096,
			DisableThinking: true,
		}); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		wantNum(t, cp, "generationConfig.maxOutputTokens", 4096)
	})

	t.Run("without the flag nothing changes", func(t *testing.T) {
		ts, cp := fakeProvider(t, http.StatusOK, gmPing)
		c := build(t, protocol.ProviderGemini, ts, nil)
		if _, err := c.Complete(context.Background(), Request{
			Messages:  []Message{{Role: RoleUser, Text: "ping"}},
			MaxTokens: 8,
		}); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		wantNum(t, cp, "generationConfig.maxOutputTokens", 8)
	})
}

// The prefill only applies to JSONOnly requests; a plain chat turn must not
// grow a phantom "{" — and a JSONOnly history already ending in an assistant
// turn must not gain a second assistant message (the API rejects two in a row).
func TestAnthropicPrefillIsScopedToJSONOnly(t *testing.T) {
	const anPing = `{"model":"c","content":[{"type":"text","text":"hello"}],
		"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`

	t.Run("plain chat is untouched", func(t *testing.T) {
		ts, cp := fakeProvider(t, http.StatusOK, anPing)
		c := build(t, protocol.ProviderAnthropic, ts, nil)
		resp, err := c.Complete(context.Background(), Request{
			Messages: []Message{{Role: RoleUser, Text: "hi"}},
		})
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if msgs, _ := dig(cp.body, "messages"); len(msgs.([]any)) != 1 {
			t.Errorf("messages = %#v, want just the user turn", msgs)
		}
		if resp.Text != "hello" {
			t.Errorf("Text = %q, want it unprefixed", resp.Text)
		}
	})

	t.Run("assistant-final history gets no second prefill", func(t *testing.T) {
		ts, cp := fakeProvider(t, http.StatusOK, anPing)
		c := build(t, protocol.ProviderAnthropic, ts, nil)
		if _, err := c.Complete(context.Background(), Request{
			JSONOnly: true,
			Messages: []Message{
				{Role: RoleUser, Text: "hi"},
				{Role: RoleAssistant, Text: "partial"},
			},
		}); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if msgs, _ := dig(cp.body, "messages"); len(msgs.([]any)) != 2 {
			t.Errorf("messages = %#v, want the two turns unchanged", msgs)
		}
	})
}

// The native Ollama connector has the same starved-probe failure Gemini had:
// think:false is a request to the template, and some templates spend tokens
// before any visible output. Measured on qwen3.8-flash-next — 8 tokens gave
// empty content, 512 gave "ok".
func TestOllamaDisableThinkingRaisesTheOutputFloor(t *testing.T) {
	const olOK = `{"model":"m","message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop"}`
	for _, tc := range []struct {
		name    string
		max     int
		disable bool
		want    float64
	}{
		{"probe-sized budget is raised", 8, true, 512},
		{"a real budget is left alone", 4096, true, 4096},
		{"without the flag nothing changes", 8, false, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, cp := fakeProvider(t, http.StatusOK, olOK)
			c := build(t, protocol.ProviderOllama, ts, nil)
			if _, err := c.Complete(context.Background(), Request{
				Messages:        []Message{{Role: RoleUser, Text: "ping"}},
				MaxTokens:       tc.max,
				DisableThinking: tc.disable,
			}); err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			wantNum(t, cp, "options.num_predict", tc.want)
		})
	}
}

// keep_alive rides on every request so a slow step does not pay the model
// load again; the env override is the operator's knob for VRAM they cannot
// afford to hold.
func TestOllamaSendsKeepAliveOnEveryRequest(t *testing.T) {
	const olOK = `{"model":"m","message":{"role":"assistant","content":"ok"},"done":true}`
	t.Run("default is thirty minutes", func(t *testing.T) {
		t.Setenv("OLLAMA_KEEP_ALIVE", "")
		ts, cp := fakeProvider(t, http.StatusOK, olOK)
		c := build(t, protocol.ProviderOllama, ts, nil)
		if _, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "ping"}}}); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if got, _ := dig(cp.body, "keep_alive"); got != "30m" {
			t.Errorf("keep_alive = %v, want 30m", got)
		}
	})
	t.Run("env override wins", func(t *testing.T) {
		t.Setenv("OLLAMA_KEEP_ALIVE", "-1")
		ts, cp := fakeProvider(t, http.StatusOK, olOK)
		c := build(t, protocol.ProviderOllama, ts, nil)
		if _, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "ping"}}}); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if got, _ := dig(cp.body, "keep_alive"); got != "-1" {
			t.Errorf("keep_alive = %v, want -1", got)
		}
	})
}
