package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The step budget is a checkpoint, not a kill switch — these pin the policy
// and the handover, which is what lets a run go on for days without the
// agent ever being told it failed for running out of steps.

func TestShouldContinueFollowsPolicy(t *testing.T) {
	r := &Runner{cfg: &config.Config{Marathon: true, MarathonMaxWindows: 0}}
	task := &protocol.Task{}
	if !r.shouldContinue(task, 1) || !r.shouldContinue(task, 500) {
		t.Fatal("unbounded marathon should always continue")
	}

	r.cfg.MarathonMaxWindows = 3
	if !r.shouldContinue(task, 2) {
		t.Error("window 2 of a 3-window cap should continue")
	}
	if r.shouldContinue(task, 3) {
		t.Error("window 3 of a 3-window cap is the last; it must not continue")
	}

	once := &protocol.Task{Params: map[string]string{protocol.ParamOnce: "true"}}
	r.cfg.MarathonMaxWindows = 0
	if r.shouldContinue(once, 1) {
		t.Error("a task opted out with once=true must not continue")
	}

	r.cfg.Marathon = false
	if r.shouldContinue(task, 1) {
		t.Error("marathon off means the old behaviour: the budget ends the run")
	}
}

func TestNextWindowCarriesTheChain(t *testing.T) {
	first := &protocol.Task{
		ID: "t1", InstanceID: "i1", OwnerID: "u1", Goal: "ship it", SkillID: "sk",
		ParentTaskID: "parent", AutoRefine: true, MaxSteps: 60, ProviderID: "p1",
		Params: map[string]string{"repo": "x/y"},
	}
	if taskWindow(first) != 1 {
		t.Fatalf("a task with no chain is window 1, got %d", taskWindow(first))
	}
	next := nextWindowTask(first, "did A and B; C is half done", 2)

	if next.ID == "" || next.ID == first.ID {
		t.Error("the successor needs its own id")
	}
	if next.Goal != first.Goal || next.SkillID != first.SkillID || next.ParentTaskID != first.ParentTaskID ||
		next.InstanceID != first.InstanceID || next.OwnerID != first.OwnerID || next.ProviderID != first.ProviderID ||
		!next.AutoRefine || next.MaxSteps != first.MaxSteps {
		t.Errorf("the successor must be the same job: %+v", next)
	}
	if next.State != protocol.TaskQueued || next.Step != 0 {
		t.Error("the successor starts fresh")
	}
	if next.Params["repo"] != "x/y" {
		t.Error("the task's own parameters travel with it")
	}
	if next.Params[protocol.ParamProgress] != "did A and B; C is half done" ||
		next.Params[protocol.ParamContinuationOf] != "t1" || next.Params[protocol.ParamWindow] != "2" {
		t.Errorf("chain params wrong: %v", next.Params)
	}
	if taskWindow(next) != 2 {
		t.Errorf("successor should read as window 2, got %d", taskWindow(next))
	}
	// The original's params must not have been mutated in place.
	if _, leaked := first.Params[protocol.ParamProgress]; leaked {
		t.Error("nextWindowTask mutated the original task's params")
	}
}

func TestProgressBlockAppearsInThePromptAndParamsStayClean(t *testing.T) {
	task := &protocol.Task{
		Goal: "ship it",
		Params: map[string]string{
			"repo":                       "x/y",
			protocol.ParamProgress:       "installed deps; tests half written",
			protocol.ParamContinuationOf: "t1",
			protocol.ParamWindow:         "2",
		},
	}
	obs := &protocol.Observation{}
	turn := buildTurn(task, nil, obs, nil, "", "")

	if !strings.Contains(turn, "PROGRESS FROM EARLIER WINDOWS") || !strings.Contains(turn, "installed deps") {
		t.Fatalf("the progress note is missing from the turn:\n%s", turn)
	}
	if !strings.Contains(turn, "window 2") {
		t.Error("the window number should be stated")
	}
	if !strings.Contains(turn, "- repo = x/y") {
		t.Error("the task's real parameter should still be listed")
	}
	for _, k := range []string{protocol.ParamContinuationOf, protocol.ParamWindow, protocol.ParamProgress} {
		if strings.Contains(turn, "- "+k+" =") {
			t.Errorf("chain bookkeeping %q leaked into PARAMETERS", k)
		}
	}
}

func TestFirstWindowHasNoProgressBlock(t *testing.T) {
	task := &protocol.Task{Goal: "ship it", Params: map[string]string{"repo": "x/y"}}
	turn := buildTurn(task, nil, &protocol.Observation{}, nil, "", "")
	if strings.Contains(turn, "PROGRESS FROM EARLIER WINDOWS") {
		t.Error("a first window has no earlier progress to show")
	}
	if !strings.Contains(turn, "PARAMETERS") {
		t.Error("ordinary parameters still render")
	}
}

func TestClipProgressKeepsTheNewestState(t *testing.T) {
	long := strings.Repeat("old ", 1000) + "NEWEST"
	got := clipProgress(long)
	if len(got) > maxProgressChars+3 {
		t.Errorf("not clipped: %d chars", len(got))
	}
	if !strings.HasSuffix(got, "NEWEST") {
		t.Error("clipping must keep the tail — the newest state is what the next window needs")
	}
}

func TestProgressSummaryFallsBackWithoutModels(t *testing.T) {
	r := &Runner{cfg: &config.Config{Marathon: true}}
	task := &protocol.Task{Goal: "g", Params: map[string]string{protocol.ParamProgress: "earlier note"}}
	hist := []turnSummary{{Step: 1, Action: "click Save", Outcome: "saved"}}
	got := r.progressSummary(nil, task, &protocol.Instance{}, hist)
	if !strings.Contains(got, "earlier note") || !strings.Contains(got, "click Save") {
		t.Errorf("fallback must carry both the prior note and the recent actions: %q", got)
	}
}
