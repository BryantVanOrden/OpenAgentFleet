package httpapi

import (
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

func agents(names ...string) []protocol.Instance {
	out := make([]protocol.Instance, 0, len(names))
	for _, n := range names {
		out = append(out, protocol.Instance{ID: n, Name: n})
	}
	return out
}

func order(content string, in []protocol.Instance) []string {
	got := orderByMention(content, in)
	names := make([]string, len(got))
	for i, g := range got {
		names[i] = g.Name
	}
	return names
}

// The agents named in the message answer first, in the order they were named.
func TestNamedAgentsAnswerInTheOrderMentioned(t *testing.T) {
	in := agents("Researcher", "Builder", "Auditor", "ToolCheck")

	got := order("Builder take the game, then Auditor review it", in)
	if got[0] != "Builder" || got[1] != "Auditor" {
		t.Errorf("order = %v, want Builder then Auditor first", got)
	}
	if len(got) != 4 {
		t.Errorf("dropped an agent: %v", got)
	}
}

// Close-enough spellings still count: people type quickly.
func TestNamesAreMatchedFuzzily(t *testing.T) {
	in := agents("Researcher", "ToolCheck", "Builder")

	cases := map[string]string{
		"researcher take this":      "Researcher", // case
		"tool check please start":   "ToolCheck",  // split into two words
		"toolcheck please start":    "ToolCheck",  // run together
		"TOOL-CHECK go":             "ToolCheck",  // punctuation and caps
		"reasercher can you look":   "Researcher", // transposed letters
		"@Builder, start on it":     "Builder",    // punctuation around it
		"Builder's turn":            "Builder",    // possessive
		"can Reseacher handle this": "Researcher", // dropped letter
	}
	for msg, want := range cases {
		got := order(msg, in)
		if got[0] != want {
			t.Errorf("%q put %q first, want %q (full order %v)", msg, got[0], want, got)
		}
	}
}

// A name that is not there does not match something else.
func TestUnrelatedWordsDoNotMatch(t *testing.T) {
	in := agents("Bob", "Auditor")

	// "auditors" contains "auditor", which is a real mention of it.
	if got := order("auditors generally check things", in); got[0] != "Auditor" {
		t.Errorf("a word containing the name should count: %v", got)
	}
	// But an unrelated short word must not match a short name.
	got := order("the job needs doing", in)
	if len(got) != 2 {
		t.Fatalf("dropped an agent: %v", got)
	}
	// Nobody named: both present, order unconstrained.
}

// Everyone still answers, named or not.
func TestUnmentionedAgentsStillAnswer(t *testing.T) {
	in := agents("Researcher", "Builder", "Auditor", "ToolCheck")
	got := order("Auditor, you first", in)

	if got[0] != "Auditor" {
		t.Fatalf("named agent is not first: %v", got)
	}
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Errorf("%q appears twice: %v", n, got)
		}
		seen[n] = true
	}
	if len(seen) != 4 {
		t.Errorf("expected all four agents, got %v", got)
	}
}

// With nobody named the order varies, so the same agent does not take the
// interesting part of every job by virtue of being first in the database.
func TestUnnamedOrderIsNotAlwaysTheSame(t *testing.T) {
	in := agents("A", "B", "C", "D", "E")
	first := map[string]bool{}
	for i := 0; i < 60; i++ {
		first[order("just get it done", in)[0]] = true
	}
	if len(first) < 2 {
		t.Errorf("the same agent answered first every time: %v", first)
	}
}

// Each named agent gets its own instruction, not the whole message.
//
// Given only the whole message, two agents produced identical plans and both
// did the design, leaving the parts they were named for undone.
func TestAssignmentForSplitsTheMessagePerAgent(t *testing.T) {
	in := agents("Researcher", "Builder", "ToolCheck", "Auditor")
	msg := "reasercher design a one-screen tap game. Builder then writes it as a " +
		"single HTML file. tool check tests it. Auditor reviews it."

	want := map[string]string{
		"Researcher": "design a one-screen tap game",
		"Builder":    "then writes it as a single HTML file",
		"ToolCheck":  "tests it",
		"Auditor":    "reviews it",
	}
	for name, expect := range want {
		got := assignmentFor(msg, name, in)
		if got != expect {
			t.Errorf("%s was assigned %q, want %q", name, got, expect)
		}
	}
}

// An agent nobody named gets nothing, rather than the whole message.
func TestUnnamedAgentHasNoAssignment(t *testing.T) {
	in := agents("Builder", "Auditor")
	if got := assignmentFor("Builder writes it", "Auditor", in); got != "" {
		t.Errorf("an unnamed agent was assigned %q", got)
	}
}
