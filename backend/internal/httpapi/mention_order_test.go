package httpapi

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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

// Naming people is also saying who is not needed.
//
// "Auditor write a two-sentence note" asked one agent for one small thing, and
// two others started writing their own version of the same note.
func TestNamingSomeoneExcludesTheRest(t *testing.T) {
	in := agents("Researcher", "Builder", "Auditor")

	named := func(content, who string) bool {
		anyNamed := false
		for _, other := range in {
			if _, _, ok := mentionSpan(content, other.Name); ok {
				if other.Name == who {
					return false
				}
				anyNamed = true
			}
		}
		return anyNamed
	}

	const one = "Auditor write a two-sentence note"
	if named(one, "Auditor") {
		t.Error("the named agent was excluded from its own job")
	}
	if !named(one, "Researcher") || !named(one, "Builder") {
		t.Error("an agent nobody named was allowed to start work anyway")
	}

	// Naming nobody addresses everyone.
	const open = "someone write a note about the fleet"
	for _, a := range in {
		if named(open, a.Name) {
			t.Errorf("%s was excluded although the message named nobody", a.Name)
		}
	}
}

// Whoever is named first has nobody ahead of them to be handed work by, so a
// tester named first must start rather than wait for a build nobody asked for.
func TestFirstNamedIsTheEarliestMentioned(t *testing.T) {
	msg := "ToolCheck please open the rollr app from the shared catalog and " +
		"test it. Builder fix anything ToolCheck reports."
	first := mentionIndex(msg, "ToolCheck")
	second := mentionIndex(msg, "Builder")
	if first < 0 || second < 0 {
		t.Fatalf("both should be mentioned: ToolCheck=%d Builder=%d", first, second)
	}
	if first >= second {
		t.Errorf("ToolCheck is named first but indexed later: %d vs %d", first, second)
	}
}

// The ordinary build-first phrasing must keep its order.
func TestBuilderFirstKeepsItsOrder(t *testing.T) {
	msg := "Builder please build convtest. ToolCheck test it after. Auditor review it last."
	b, tc, a := mentionIndex(msg, "Builder"), mentionIndex(msg, "ToolCheck"), mentionIndex(msg, "Auditor")
	if !(b < tc && tc < a) {
		t.Errorf("order wrong: Builder=%d ToolCheck=%d Auditor=%d", b, tc, a)
	}
}

// With no chat model, an external agent named in a request claims the part
// addressed to it in the operator's own words.
func TestAnExternalAgentClaimsItsPartWithoutAModel(t *testing.T) {
	in := agents("Claude", "Codex")
	msg := "Claude writes notes/plan.md with three steps, and Codex reviews it."
	if got := planWithoutModel(msg, "Claude", in); !strings.HasPrefix(got, "PLAN: writes notes/plan.md") {
		t.Errorf("Claude claims %q", got)
	}
	if got := planWithoutModel(msg, "Codex", in); !strings.HasPrefix(got, "PLAN: reviews it") {
		t.Errorf("Codex claims %q", got)
	}
	if got := planWithoutModel("build me a website", "Claude", in); got != "" {
		t.Errorf("an agent nobody named claims nothing, got %q", got)
	}
	if _, ok := planFrom(planWithoutModel(msg, "Claude", in)); !ok {
		t.Error("the claim reads as a plan, so it is filed as a ticket")
	}
}
