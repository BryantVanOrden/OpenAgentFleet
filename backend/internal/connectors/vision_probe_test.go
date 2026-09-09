package connectors

import (
	"context"
	"net/http"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The probe is a measurement: a model that names the colour can see, one that
// says nothing was attached cannot, whatever its record claims.
func TestDetectVisionReadsTheAnswerNotTheRecord(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  bool
	}{
		{"names the colour", `{"choices":[{"message":{"content":"Red"},"finish_reason":"stop"}]}`, true},
		{"names it in a sentence", `{"choices":[{"message":{"content":"The image is filled with a solid red colour."},"finish_reason":"stop"}]}`, true},
		{"was given no image", `{"choices":[{"message":{"content":"NONE"},"finish_reason":"stop"}]}`, false},
		{"explains it cannot see", `{"choices":[{"message":{"content":"No image is attached to your message, so I cannot see a colour."},"finish_reason":"stop"}]}`, false},
		{"guesses another colour", `{"choices":[{"message":{"content":"Blue"},"finish_reason":"stop"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, cp := fakeProvider(t, http.StatusOK, tc.reply)
			// Registered blind on purpose: the probe must send the image anyway.
			c := build(t, protocol.ProviderCompatible, ts, func(p *protocol.Provider) { p.Vision = false })
			got, err := DetectVision(context.Background(), c)
			if err != nil {
				t.Fatalf("DetectVision() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("DetectVision() = %v, want %v", got, tc.want)
			}
			// The request carried a real image part, whatever the record said.
			msgs, _ := dig(cp.body, "messages")
			last := msgs.([]any)[len(msgs.([]any))-1].(map[string]any)
			parts, ok := last["content"].([]any)
			if !ok || len(parts) != 2 {
				t.Fatalf("the probe did not attach an image: %#v", last["content"])
			}
		})
	}
}
