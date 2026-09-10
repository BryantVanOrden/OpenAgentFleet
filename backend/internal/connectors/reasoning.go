package connectors

import (
	"regexp"
	"strings"
)

// Reasoning blocks.
//
// Some models think out loud inside tags before they answer: <think> (DeepSeek,
// Qwen), <thinking>, <reasoning>, and -- seen live from qwen3.8-flash-next on
// 2026-09-09 -- <antml_thinking>, which it must have picked up from training
// data. A gateway configured with reasoning off still passed one through, and
// the fleet channel showed a bot's colleague-facing reply beginning "Let me
// analyze what's happening here. I'm Checker, and I need to respond to this
// message." Nobody was meant to read that.
//
// Every completion is scrubbed here, on its way out of the registry, so the
// agent loop, the peer responder, Oaf and the chat never see the tags. A
// closed block goes entirely. An unclosed one -- the model ran out of tokens
// mid-thought -- keeps its text minus the tag, because whatever it managed to
// say is all there is.

var (
	reasoningClosed = regexp.MustCompile(`(?is)<(think|thinking|reasoning|antml_thinking|antml:thinking)\b[^>]*>.*?</\s*(think|thinking|reasoning|antml_thinking|antml:thinking)\s*>`)
	reasoningTag    = regexp.MustCompile(`(?i)</?\s*(think|thinking|reasoning|antml_thinking|antml:thinking)\b[^>]*>`)
)

// StripReasoning removes a model's thinking blocks from its reply. The result
// is trimmed; a reply that was nothing but thought comes back empty, which the
// empty-completion handling upstream already knows what to do with.
func StripReasoning(text string) string {
	if !strings.Contains(text, "<") {
		return text
	}
	out := reasoningClosed.ReplaceAllString(text, "")
	if strings.TrimSpace(out) == "" && strings.TrimSpace(text) != "" {
		// The whole reply was one thinking block: the model answered inside
		// its own head. Better its thoughts than nothing? No -- upstream
		// treats an empty completion as a retryable fault, which gets a real
		// answer; leaking the block would get a bot talking to itself in
		// public.
		return ""
	}
	out = reasoningTag.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}
