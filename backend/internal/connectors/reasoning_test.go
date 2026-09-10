package connectors

import "testing"

func TestStripReasoningRemovesClosedBlocks(t *testing.T) {
	cases := map[string]string{
		"<think>\nlet me see\n</think>\nPLAN: I test it.":                         "PLAN: I test it.",
		"<antml_thinking>I'm Checker, I need to respond.</antml_thinking>PLAN: ok": "PLAN: ok",
		"<thinking>a</thinking>first<reasoning>b</reasoning> second":                "first second",
		"no tags here { \"action\": \"click\" }":                                    "no tags here { \"action\": \"click\" }",
		// Case and attributes do not matter.
		"<Think type=\"x\">...</THINK>\n{\"action\":\"done\"}": "{\"action\":\"done\"}",
	}
	for in, want := range cases {
		if got := StripReasoning(in); got != want {
			t.Errorf("StripReasoning(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripReasoningKeepsAnUnclosedThoughtsText(t *testing.T) {
	// The model ran out of tokens mid-thought: the tag goes, the words stay.
	got := StripReasoning("<antml_thinking>\nLooking at the history: Builder has taken the build.")
	if got != "Looking at the history: Builder has taken the build." {
		t.Fatalf("got %q", got)
	}
}

func TestStripReasoningEmptiesAReplyThatWasAllThought(t *testing.T) {
	if got := StripReasoning("<think>only thinking, no answer</think>"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
