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
