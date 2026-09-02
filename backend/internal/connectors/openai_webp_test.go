package connectors

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A real 64x64 WebP. The agent loop sends WebP every turn, and a llama.cpp
// server answers 400 "Failed to load image or audio file" to it, which
// surfaced as "every model provider failed" and failed the whole task.
const webpFrame = "UklGRmAAAABXRUJQVlA4IFQAAABwBQCdASpAAEAALmlIpFIiJaWlhYBoS0gzgzQLwB+gH8AzwTYD9AP4Bi+QIG31YS5EEc/AAP77mZHg0x6L316Zg/BXAnHQfgrhv0qrxDDcvrsFqAA="

func webpRequest() Request {
	return Request{
		Messages: []Message{{
			Role: RoleUser, Text: "What is the next action?",
			Image: webpFrame, ImageMime: "image/webp",
		}},
	}
}

func TestOpenAITranscodesWebPToPNG(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderCompatible, ts, nil)
	if _, err := c.Complete(context.Background(), webpRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	got, ok := dig(cp.ready().body, "messages.0.content.1.image_url.url")
	if !ok {
		t.Fatalf("no image part in the request body")
	}
	url, _ := got.(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("image mime = %.30q, want a PNG data URI", url)
	}
	if strings.Contains(url, "image/webp") || strings.Contains(url, webpFrame) {
		t.Error("the WebP payload reached the provider unchanged")
	}
}

// Undecodable bytes must go out as they came in: a provider that understands
// the original is better served by it than by an error.
func TestOpenAIPassesThroughAnImageItCannotDecode(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderCompatible, ts, nil)
	req := webpRequest()
	req.Messages[0].Image = "QUJDRA==" // "ABCD", not an image at all
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	wantStr(t, cp, "messages.0.content.1.image_url.url", "data:image/webp;base64,QUJDRA==")
}

// The thinking hint is not an OpenAI argument, and api.openai.com rejects a
// request carrying one it does not recognise -- so it may only go to the
// self-hosted kind.
func TestDisableThinkingOnlyReachesTheCompatibleKind(t *testing.T) {
	for _, tc := range []struct {
		kind protocol.ProviderKind
		sent bool
	}{
		{protocol.ProviderCompatible, true},
		{protocol.ProviderOpenAI, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			ts, cp := fakeProvider(t, http.StatusOK, oaOK)
			c := build(t, tc.kind, ts, nil)
			req := webpRequest()
			req.DisableThinking = true
			if _, err := c.Complete(context.Background(), req); err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			path := "chat_template_kwargs.enable_thinking"
			if tc.sent {
				if got, ok := dig(cp.ready().body, path); !ok || got != false {
					t.Errorf("%s = %#v (present=%v), want false", path, got, ok)
				}
			} else {
				wantAbsent(t, cp, path)
			}
		})
	}
}

// Without DisableThinking nothing is added, for either kind.
func TestNoThinkingHintWhenNotAsked(t *testing.T) {
	ts, cp := fakeProvider(t, http.StatusOK, oaOK)
	c := build(t, protocol.ProviderCompatible, ts, nil)
	if _, err := c.Complete(context.Background(), webpRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	wantAbsent(t, cp, "chat_template_kwargs")
}
