package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// These pin the two things the README said were missing: independent nodes do
// not run in parallel, and edge conditions are stored and displayed while
// nothing acts on them.

func schedNode(id string, deps ...string) protocol.PipelineNode {
	return protocol.PipelineNode{ID: id, Name: id, GoalTemplate: "do " + id, InstanceID: "inst-1"}
}

func condEdge(from, to, cond string) protocol.PipelineEdge {
	return protocol.PipelineEdge{FromNodeID: from, ToNodeID: to, Condition: cond}
}

// awaitRun polls until a run reaches a terminal status.
func awaitRun(t *testing.T, e *Engine, runID string) protocol.PipelineRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range e.ListRuns(context.Background(), "") {
			if r.ID != runID {
				continue
			}
			if r.FinishedAt != nil {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never finished", runID)
	return protocol.PipelineRun{}
}

func saveAndRun(t *testing.T, p protocol.WorkflowPipeline, runner NodeRunner) (*Engine, protocol.PipelineRun) {
	t.Helper()
	e := NewEngine()
	e.SetNodeRunner(runner)
	saved, err := e.SavePipeline(context.Background(), p)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	started, err := e.TriggerRun(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	return e, awaitRun(t, e, started.ID)
}

// ------------------------------------------------------------- parallelism ---

func TestIndependentNodesRunConcurrently(t *testing.T) {
	// Three nodes, no edges. Sequentially this is 3x the node duration; in
	// parallel it is one. The assertion is on observed concurrency rather than
	// on wall clock, which would be flaky on a loaded machine.
	var mu sync.Mutex
	var concurrent, peak int

	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		mu.Lock()
		concurrent++
		if concurrent > peak {
			peak = concurrent
		}
		mu.Unlock()

		time.Sleep(150 * time.Millisecond)

		mu.Lock()
		concurrent--
		mu.Unlock()
		return "ok", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "fan out",
		Nodes: []protocol.PipelineNode{schedNode("a"), schedNode("b"), schedNode("c")},
	}, runner)

	if run.Status != "completed" {
		t.Errorf("status = %q, want completed", run.Status)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d: independent nodes are still running one at a time", peak)
	}
	if len(run.NodeResults) != 3 {
		t.Errorf("ran %d nodes, want 3", len(run.NodeResults))
	}
}

func TestDependenciesAreStillRespectedWhileRunningInParallel(t *testing.T) {
	// A diamond: a -> {b, c} -> d. b and c may overlap; d must come after both,
	// and a must come before both.
	var mu sync.Mutex
	started := map[string]time.Time{}
	ended := map[string]time.Time{}

	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		mu.Lock()
		started[n.ID] = time.Now()
		mu.Unlock()
		time.Sleep(80 * time.Millisecond)
		mu.Lock()
		ended[n.ID] = time.Now()
		mu.Unlock()
		return "ok", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "diamond",
		Nodes: []protocol.PipelineNode{schedNode("a"), schedNode("b"), schedNode("c"), schedNode("d")},
		Edges: []protocol.PipelineEdge{
			condEdge("a", "b", ""), condEdge("a", "c", ""),
			condEdge("b", "d", ""), condEdge("c", "d", ""),
		},
	}, runner)

	if run.Status != "completed" {
		t.Fatalf("status = %q, want completed", run.Status)
	}
	mu.Lock()
	defer mu.Unlock()
	if !started["b"].After(ended["a"]) || !started["c"].After(ended["a"]) {
		t.Error("b or c started before a finished")
	}
	if !started["d"].After(ended["b"]) || !started["d"].After(ended["c"]) {
		t.Error("d started before both its dependencies finished")
	}
	// b and c should have overlapped; if they did not, the fan-out is serial.
	if started["c"].After(ended["b"]) {
		t.Error("b and c did not overlap: the fan-out is still sequential")
	}
}

func TestMaxParallelIsHonoured(t *testing.T) {
	var mu sync.Mutex
	var concurrent, peak int

	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		mu.Lock()
		concurrent++
		if concurrent > peak {
			peak = concurrent
		}
		mu.Unlock()
		time.Sleep(60 * time.Millisecond)
		mu.Lock()
		concurrent--
		mu.Unlock()
		return "ok", nil
	}

	nodes := make([]protocol.PipelineNode, 8)
	for i := range nodes {
		nodes[i] = schedNode(fmt.Sprintf("n%d", i))
	}
	// Every node starts a real task on a real desktop, so a graph with no edges
	// must not try to start all eight at once.
	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name: "bounded", Nodes: nodes, MaxParallel: 2,
	}, runner)

	if run.Status != "completed" {
		t.Errorf("status = %q, want completed", run.Status)
	}
	if peak > 2 {
		t.Errorf("peak concurrency was %d, want at most the configured 2", peak)
	}
	if len(run.NodeResults) != 8 {
		t.Errorf("ran %d nodes, want all 8", len(run.NodeResults))
	}
}

// -------------------------------------------------------------- conditions ---

func TestSuccessAndFailureBranchesTakeDifferentPaths(t *testing.T) {
	// deploy fails; only the rollback branch should run, and the announce
	// branch should be skipped. Both used to run, because the condition was
	// stored and evaluated by nothing.
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		if n.ID == "deploy" {
			return "", errors.New("the health check never went green")
		}
		return "ok", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "deploy with a rollback branch",
		Nodes: []protocol.PipelineNode{schedNode("deploy"), schedNode("announce"), schedNode("rollback")},
		Edges: []protocol.PipelineEdge{
			condEdge("deploy", "announce", "success"),
			condEdge("deploy", "rollback", "failure"),
		},
	}, runner)

	if got := run.NodeStates["rollback"]; got != NodeDone {
		t.Errorf("rollback state = %q, want %q: the failure branch did not fire", got, NodeDone)
	}
	if got := run.NodeStates["announce"]; got != NodeSkipped {
		t.Errorf("announce state = %q, want %q: the success branch fired after a failure", got, NodeSkipped)
	}
	// The failure is handled by an explicit branch, so the run itself is not a
	// failure — otherwise writing an error path would be pointless.
	if run.Status != "completed" {
		t.Errorf("status = %q, want completed: the failure has an explicit branch", run.Status)
	}
}

func TestAnUnhandledFailureFailsTheRun(t *testing.T) {
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		if n.ID == "build" {
			return "", errors.New("compile error")
		}
		return "ok", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "no error path",
		Nodes: []protocol.PipelineNode{schedNode("build"), schedNode("ship")},
		Edges: []protocol.PipelineEdge{condEdge("build", "ship", "success")},
	}, runner)

	if run.Status != "failed" {
		t.Errorf("status = %q, want failed", run.Status)
	}
	if got := run.NodeStates["ship"]; got != NodeSkipped {
		t.Errorf("ship state = %q, want skipped", got)
	}
	if !strings.Contains(run.NodeResults["build"], "compile error") {
		t.Errorf("build result %q loses the reason", run.NodeResults["build"])
	}
}

func TestContentConditionsGateOnTheUpstreamResult(t *testing.T) {
	cases := []struct {
		name      string
		condition string
		result    string
		wantRun   bool
	}{
		{"contains matches", "contains:approved", "The change was APPROVED by review", true},
		{"contains does not match", "contains:approved", "rejected", false},
		{"not_contains blocks on a match", "not_contains:error", "there was an error", false},
		{"not_contains passes when absent", "not_contains:error", "all clear", true},
		{"equals is exact", "equals:done", "  Done  ", true},
		{"equals rejects a substring", "equals:done", "done and more", false},
		{"matches on a regex", `matches:^\d+ files? changed`, "12 files changed", true},
		{"regex that does not match", `matches:^\d+ files? changed`, "nothing changed", false},
		{"always runs regardless", "always", "anything", true},
		{"an empty condition is a plain dependency", "", "anything", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
				if n.ID == "review" {
					return tc.result, nil
				}
				return "ok", nil
			}
			_, run := saveAndRun(t, protocol.WorkflowPipeline{
				Name:  "gated",
				Nodes: []protocol.PipelineNode{schedNode("review"), schedNode("merge")},
				Edges: []protocol.PipelineEdge{condEdge("review", "merge", tc.condition)},
			}, runner)

			got := run.NodeStates["merge"]
			want := NodeSkipped
			if tc.wantRun {
				want = NodeDone
			}
			if got != want {
				t.Errorf("merge state = %q, want %q (condition %q against result %q)",
					got, want, tc.condition, tc.result)
			}
		})
	}
}

func TestSkippingPropagatesDownstream(t *testing.T) {
	// a succeeds, so the failure branch b is skipped, and c — which depends on
	// b — has nothing to run on. It must be skipped, not left waiting forever
	// and not run as though b had succeeded.
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		return "fine", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "propagation",
		Nodes: []protocol.PipelineNode{schedNode("a"), schedNode("b"), schedNode("c")},
		Edges: []protocol.PipelineEdge{
			condEdge("a", "b", "failure"),
			condEdge("b", "c", ""),
		},
	}, runner)

	if run.Status != "completed" {
		t.Errorf("status = %q, want completed", run.Status)
	}
	for _, id := range []string{"b", "c"} {
		if got := run.NodeStates[id]; got != NodeSkipped {
			t.Errorf("%s state = %q, want skipped", id, got)
		}
	}
	if got := run.NodeStates["a"]; got != NodeDone {
		t.Errorf("a state = %q, want done", got)
	}
}

func TestAllIncomingConditionsMustPass(t *testing.T) {
	// Two dependencies, one satisfied and one not. The node waits for both, so
	// it also requires both — an AND, which is what reading the graph suggests.
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		if n.ID == "lint" {
			return "3 warnings", nil
		}
		return "clean", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "both gates",
		Nodes: []protocol.PipelineNode{schedNode("test"), schedNode("lint"), schedNode("release")},
		Edges: []protocol.PipelineEdge{
			condEdge("test", "release", "success"),
			condEdge("lint", "release", "not_contains:warning"),
		},
	}, runner)

	if got := run.NodeStates["release"]; got != NodeSkipped {
		t.Errorf("release state = %q, want skipped: one of its two gates failed", got)
	}
}

// -------------------------------------------------------------- validation ---

func TestUnknownConditionsAreRejectedAtSave(t *testing.T) {
	e := NewEngine()
	_, err := e.SavePipeline(context.Background(), protocol.WorkflowPipeline{
		Name:  "typo",
		Nodes: []protocol.PipelineNode{schedNode("a"), schedNode("b")},
		Edges: []protocol.PipelineEdge{condEdge("a", "b", "sucess")}, // misspelled
	})
	if err == nil {
		t.Fatal("a misspelled condition should be rejected at save")
	}
	// Now that conditions are evaluated, a misspelling would be a branch that
	// silently never fires, which is the failure this check exists to prevent.
	if !strings.Contains(err.Error(), "sucess") {
		t.Errorf("error %q does not quote the offending condition", err)
	}
}

func TestConditionArgumentsAreValidatedAtSave(t *testing.T) {
	for _, tc := range []struct{ name, cond, want string }{
		{"contains with no text", "contains:", "needs text"},
		{"contains with no colon", "contains", "needs text"},
		{"success with an argument", "success:yes", "takes no argument"},
		{"an invalid regex", "matches:[unclosed", "invalid regular expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCondition(tc.cond)
			if err == nil {
				t.Fatalf("expected %q to be rejected", tc.cond)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestValidConditionsAreAccepted(t *testing.T) {
	for _, cond := range []string{
		"", "always", "success", "failure",
		"contains:approved", "not_contains:error", "equals:done",
		`matches:^\d+ ok$`,
		// A colon inside the argument must not be re-split.
		"contains:http://example.com",
	} {
		if err := ValidateCondition(cond); err != nil {
			t.Errorf("ValidateCondition(%q) = %v, want nil", cond, err)
		}
	}
}

// ---------------------------------------------------------------- guardrails ---

func TestEachNodeRunsExactlyOnce(t *testing.T) {
	// A diamond fans in on d. A scheduler that treated "a dependency finished"
	// as "the node is ready" without claiming it would start d twice, once per
	// incoming edge.
	var counts sync.Map
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		v, _ := counts.LoadOrStore(n.ID, new(int32))
		atomic.AddInt32(v.(*int32), 1)
		return "ok", nil
	}

	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "fan in",
		Nodes: []protocol.PipelineNode{schedNode("a"), schedNode("b"), schedNode("c"), schedNode("d")},
		Edges: []protocol.PipelineEdge{
			condEdge("a", "b", ""), condEdge("a", "c", ""),
			condEdge("b", "d", ""), condEdge("c", "d", ""),
		},
	}, runner)

	if run.Status != "completed" {
		t.Fatalf("status = %q, want completed", run.Status)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		v, ok := counts.Load(id)
		if !ok {
			t.Errorf("%s never ran", id)
			continue
		}
		if got := atomic.LoadInt32(v.(*int32)); got != 1 {
			t.Errorf("%s ran %d times, want exactly 1", id, got)
		}
	}
}

func TestRunsAreSnapshottedWhileNodesWriteToThem(t *testing.T) {
	// NodeStates is a second map on the run struct, so it needs the same
	// detaching NodeResults already had. Without it, serialising a running
	// pipeline is a concurrent map read and write, which is fatal rather than
	// recoverable and would take the orchestrator down.
	release := make(chan struct{})
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		<-release
		return "ok", nil
	}

	e := NewEngine()
	e.SetNodeRunner(runner)
	nodes := make([]protocol.PipelineNode, 12)
	for i := range nodes {
		nodes[i] = schedNode(fmt.Sprintf("n%d", i))
	}
	saved, err := e.SavePipeline(context.Background(), protocol.WorkflowPipeline{
		Name: "concurrent reads", Nodes: nodes, MaxParallel: 6,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	started, err := e.TriggerRun(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 400; i++ {
			for _, r := range e.ListRuns(context.Background(), saved.ID) {
				// Reading every map is what a JSON encoder does.
				for k, v := range r.NodeStates {
					_, _ = k, v
				}
				for k, v := range r.NodeResults {
					_, _ = k, v
				}
			}
		}
	}()

	time.Sleep(30 * time.Millisecond)
	close(release)
	<-done
	awaitRun(t, e, started.ID)
}

func TestARunWithNoNodeRunnerIsRefused(t *testing.T) {
	e := NewEngine()
	saved, err := e.SavePipeline(context.Background(), protocol.WorkflowPipeline{
		Name: "no runner", Nodes: []protocol.PipelineNode{schedNode("a")},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// Refusing beats reporting invented success, which is what it used to do.
	if _, err := e.TriggerRun(context.Background(), saved.ID); err == nil {
		t.Fatal("a run with nothing wired up to execute a node should be refused")
	}
}

// A node runner is told which run and pipeline it is running for: that is
// what groups a run's stage tickets under one ticket for the run.
func TestANodeKnowsItsRun(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]RunInfo{}
	runner := func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		info, ok := RunFrom(ctx)
		if !ok {
			return "", context.Canceled
		}
		mu.Lock()
		seen[n.ID] = info
		mu.Unlock()
		return "ok", nil
	}
	_, run := saveAndRun(t, protocol.WorkflowPipeline{
		Name:  "nightly",
		Nodes: []protocol.PipelineNode{schedNode("a"), schedNode("b")},
	}, runner)
	if run.Status != "completed" {
		t.Fatalf("status %q", run.Status)
	}
	for _, id := range []string{"a", "b"} {
		if seen[id].RunID != run.ID || seen[id].Pipeline != "nightly" {
			t.Errorf("node %s saw %+v, want run %s of nightly", id, seen[id], run.ID)
		}
	}
}
