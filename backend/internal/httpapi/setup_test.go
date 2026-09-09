package httpapi

import (
	"reflect"
	"testing"
)

// The plan is the one place that says what a fresh deployment is missing, in
// the order it has to be fixed: nothing works without a model, a model does
// nothing without a bot, a bot with no goal is a desktop nobody is using.
func TestSetupPlanOrdersTheMissingPieces(t *testing.T) {
	cases := []struct {
		name                            string
		providers, bots, running, tasks int
		next                            string
		done                            []bool
	}{
		{"fresh install", 0, 0, 0, 0, "model", []bool{false, false, false}},
		{"model connected", 1, 0, 0, 0, "bot", []bool{true, false, false}},
		{"bot provisioned", 1, 1, 1, 0, "goal", []bool{true, true, false}},
		{"first task run", 1, 1, 1, 1, "ready", []bool{true, true, true}},
		{"bot without a model still wants the model first", 0, 2, 2, 5, "model", []bool{false, true, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := setupPlan(tc.providers, tc.bots, tc.running, tc.tasks)
			if st.Next != tc.next {
				t.Errorf("Next = %q, want %q", st.Next, tc.next)
			}
			var done []bool
			for _, s := range st.Steps {
				done = append(done, s.Done)
			}
			if !reflect.DeepEqual(done, tc.done) {
				t.Errorf("done = %v, want %v", done, tc.done)
			}
			if st.Steps[0].Action != "/setup" {
				t.Errorf("the model step should be doable from the chat, got action %q", st.Steps[0].Action)
			}
		})
	}
}

// Vision-advertising names lead, utility models trail: the probe budget is
// three models, and spending it on an embedding model finds nothing.
func TestRankModelsPutsLikelyDriversFirst(t *testing.T) {
	got := rankModels([]string{"nomic-embed-text", "deepseek-r1:14b", "qwen2.5vl:7b", "glm-ocr:latest", "qwen3.8-flash-next:latest"})
	want := []string{"qwen2.5vl:7b", "qwen3.8-flash-next:latest", "deepseek-r1:14b", "nomic-embed-text", "glm-ocr:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rankModels = %v, want %v", got, want)
	}
}

func TestV1OfDoesNotDoubleTheSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"http://h:11434":     "http://h:11434/v1",
		"http://h:11434/":    "http://h:11434/v1",
		"http://h:11434/v1":  "http://h:11434/v1",
		"http://h:11434/v1/": "http://h:11434/v1",
	} {
		if got := v1Of(in); got != want {
			t.Errorf("v1Of(%q) = %q, want %q", in, got, want)
		}
	}
}
