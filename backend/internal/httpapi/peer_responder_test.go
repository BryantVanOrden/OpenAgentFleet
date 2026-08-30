package httpapi

import "testing"

// A plan only counts when the agent opened with it.
//
// The bug this guards: fleet comms answered every message as a status update,
// so asked to work together and build something, four agents each replied "I
// am currently idle and available" and nobody did anything. The prompt now
// lets an agent commit to a part -- and a commitment nobody acts on would be
// the same bug wearing a hat, so the marker starts real work.
func TestPlanFrom(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"committed", "PLAN: I will write the game loop.", "I will write the game loop."},
		{"lowercase", "plan: I will do the art.", "I will do the art."},
		{"leading space", "  PLAN: mine is the physics.", "mine is the physics."},
		{"status update", "I am currently idle and available.", ""},
		{"mentions plans", "I have no plans today.", ""},
		{"marker mid-sentence", "My PLAN: is unclear.", ""},
		{"marker with nothing after", "PLAN:", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := planFrom(tc.body)
			if tc.want == "" {
				if ok {
					t.Errorf("planFrom(%q) started work on a non-plan: %q", tc.body, got)
				}
				return
			}
			if !ok {
				t.Fatalf("planFrom(%q) found no plan", tc.body)
			}
			if got != tc.want {
				t.Errorf("planFrom(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}


// A commitment counts even without the marker, and a status update never does.
//
// Every string here was produced by a real agent in fleet comms. Two of the
// commitments -- Builder taking the game and Researcher taking the design --
// started no work, because neither opened with the word the parser wanted, and
// the two most important parts of the job silently did not happen.
func TestPlanFromReadsRealAgentReplies(t *testing.T) {
	commitments := []string{
		"I accept the task of writing the game itself. I will produce a complete, self-contained HTML/JS file.",
		"I will take the design phase for the game, defining the core mechanics.",
		"PLAN: I will take the testing role by creating a test script.",
		"I'll handle the art and the sprite sheet.",
		"I am taking the review pass once Builder is done.",
	}
	for _, c := range commitments {
		if _, ok := planFrom(c); !ok {
			t.Errorf("commitment not recognised, so no work would start:\n  %s", c)
		}
	}

	statuses := []string{
		"I am currently idle and available, as my previous attempts were cancelled.",
		"I have been idle since my last message, so I am currently available.",
		"I am currently idle and available on the XFCE desktop with nothing running.",
		"I will be available once my current task finishes.",
		"I am ready to receive instructions.",
		"Standing by for the next task.",
		"I have no plans today.",
	}
	for _, st := range statuses {
		if plan, ok := planFrom(st); ok {
			t.Errorf("a status update started work:\n  %s\n  parsed as: %s", st, plan)
		}
	}
}
