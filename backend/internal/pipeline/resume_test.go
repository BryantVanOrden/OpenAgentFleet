package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Runs used to die with the process: the executor's state was memory, so a
// restart left every in-flight run recorded as `running` forever and lost the
// node results that had already landed. These tests run the whole death-and-
// rebirth cycle against a fake store: engine one writes through and "dies",
// engine two hydrates, resumes, and finishes the run.

// fakeRunStore is the durable half, in memory.
type fakeRunStore struct {
	mu        sync.Mutex
	pipelines map[string]protocol.WorkflowPipeline
	runs      map[string]protocol.PipelineRun
}

func newFakeRunStore() *fakeRunStore {
	return &fakeRunStore{
		pipelines: map[string]protocol.WorkflowPipeline{},
		runs:      map[string]protocol.PipelineRun{},
	}
}

func (f *fakeRunStore) UpsertPipeline(_ context.Context, p protocol.WorkflowPipeline) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pipelines[p.ID] = p
	return nil
}

func (f *fakeRunStore) ListPipelines(_ context.Context) ([]protocol.WorkflowPipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []protocol.WorkflowPipeline{}
	for _, p := range f.pipelines {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeRunStore) DeletePipeline(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pipelines, id)
	return nil
}

func (f *fakeRunStore) UpsertPipelineRun(_ context.Context, r protocol.PipelineRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[r.ID] = r
	return nil
}

func (f *fakeRunStore) ListPipelineRuns(_ context.Context, _ int) ([]protocol.PipelineRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []protocol.PipelineRun{}
	for _, r := range f.runs {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRunStore) run(id string) (protocol.PipelineRun, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	return r, ok
}

// waitFor polls until the condition holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func chainPipeline() protocol.WorkflowPipeline {
	return protocol.WorkflowPipeline{
		ID:   "pipe-resume",
		Name: "resume chain",
		Nodes: []protocol.PipelineNode{
			{ID: "a", Name: "A", InstanceID: "i", GoalTemplate: "first"},
			{ID: "b", Name: "B", InstanceID: "i", GoalTemplate: "second"},
			{ID: "c", Name: "C", InstanceID: "i", GoalTemplate: "third"},
		},
		Edges: []protocol.PipelineEdge{
			{FromNodeID: "a", ToNodeID: "b", Condition: "success"},
			{FromNodeID: "b", ToNodeID: "c", Condition: "success"},
		},
	}
}

func TestARunSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	st := newFakeRunStore()

	// Engine one: node A completes, node B blocks until the "crash".
	engine1 := NewEngine()
	if err := engine1.AttachStore(ctx, st); err != nil {
		t.Fatal(err)
	}

	crashed := make(chan struct{})
	engine1.SetNodeRunner(func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		if n.ID == "a" {
			return "alpha result", nil
		}
		// B and C hang until the crash: engine one never finishes them.
		<-crashed
		return "", context.Canceled
	})
	if _, err := engine1.SavePipeline(ctx, chainPipeline()); err != nil {
		t.Fatal(err)
	}
	run, err := engine1.TriggerRun(ctx, "pipe-resume")
	if err != nil {
		t.Fatal(err)
	}

	// Wait until A's result has reached the DURABLE copy while B is running —
	// this is the state a real crash leaves behind.
	waitFor(t, "node A persisted with B in flight", func() bool {
		r, ok := st.run(run.ID)
		return ok && r.NodeResults["a"] == "alpha result" && r.NodeStates["b"] == NodeRunning
	})
	close(crashed) // engine one's goroutines die with the "process"

	// Engine two: a fresh process. It must find the run, keep A's result, and
	// re-dispatch B — whose half-finished work cannot be rejoined — then C.
	engine2 := NewEngine()
	if err := engine2.AttachStore(ctx, st); err != nil {
		t.Fatal(err)
	}
	var reran []string
	var mu sync.Mutex
	engine2.SetNodeRunner(func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		mu.Lock()
		reran = append(reran, n.ID)
		mu.Unlock()
		return n.ID + " after resume", nil
	})

	if n := engine2.ResumeInterrupted(ctx); n != 1 {
		t.Fatalf("ResumeInterrupted = %d, want 1", n)
	}

	waitFor(t, "the resumed run to complete", func() bool {
		r, ok := st.run(run.ID)
		return ok && r.Status == "completed"
	})

	final, _ := st.run(run.ID)
	if final.NodeResults["a"] != "alpha result" {
		t.Errorf("node A's pre-crash result was lost: %q", final.NodeResults["a"])
	}
	if final.NodeResults["b"] != "b after resume" || final.NodeResults["c"] != "c after resume" {
		t.Errorf("B/C did not run after resume: %+v", final.NodeResults)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range reran {
		if id == "a" {
			t.Error("node A ran again despite having a settled result")
		}
	}
}

func TestResumeKeepsConditionSemantics(t *testing.T) {
	// A failure branch decided BEFORE the crash must stay decided after it:
	// the success path was skipped pre-crash, and resume must not run it.
	ctx := context.Background()
	st := newFakeRunStore()

	p := protocol.WorkflowPipeline{
		ID:   "pipe-branch",
		Name: "branch",
		Nodes: []protocol.PipelineNode{
			{ID: "build", Name: "Build", InstanceID: "i", GoalTemplate: "build"},
			{ID: "announce", Name: "Announce", InstanceID: "i", GoalTemplate: "announce"},
			{ID: "rollback", Name: "Rollback", InstanceID: "i", GoalTemplate: "roll back"},
		},
		Edges: []protocol.PipelineEdge{
			{FromNodeID: "build", ToNodeID: "announce", Condition: "success"},
			{FromNodeID: "build", ToNodeID: "rollback", Condition: "failure"},
		},
	}

	// The pre-crash durable state, written by hand: build failed, announce was
	// skipped, rollback was mid-flight when the process died.
	st.pipelines[p.ID] = p
	st.runs["run-x"] = protocol.PipelineRun{
		ID: "run-x", PipelineID: p.ID, Status: "running",
		NodeResults: map[string]string{
			"build":    "failed: the build broke",
			"announce": "skipped: build did not satisfy \"success\" on the edge from it",
		},
		NodeStates: map[string]string{
			"build": NodeFailed, "announce": NodeSkipped, "rollback": NodeRunning,
		},
		StartedAt: time.Now().UTC(),
	}

	engine := NewEngine()
	if err := engine.AttachStore(ctx, st); err != nil {
		t.Fatal(err)
	}
	var ran []string
	var mu sync.Mutex
	engine.SetNodeRunner(func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		mu.Lock()
		ran = append(ran, n.ID)
		mu.Unlock()
		return "rolled back", nil
	})
	if n := engine.ResumeInterrupted(ctx); n != 1 {
		t.Fatalf("ResumeInterrupted = %d, want 1", n)
	}

	waitFor(t, "the branch run to settle", func() bool {
		r, ok := st.run("run-x")
		return ok && r.FinishedAt != nil
	})

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "rollback" {
		t.Fatalf("resume ran %v, want only the rollback branch", ran)
	}
	final, _ := st.run("run-x")
	// The failure was handled by an explicit branch, so the run is not "failed".
	if final.Status != "completed" {
		t.Errorf("status = %q, want completed (the failure branch handled it)", final.Status)
	}
	if final.NodeStates["announce"] != NodeSkipped {
		t.Errorf("the pre-crash skip was forgotten: announce = %q", final.NodeStates["announce"])
	}
}

func TestARunWhosePipelineWasDeletedIsClosedNotStuck(t *testing.T) {
	ctx := context.Background()
	st := newFakeRunStore()
	st.runs["run-orphan"] = protocol.PipelineRun{
		ID: "run-orphan", PipelineID: "pipe-gone", Status: "running",
		NodeStates: map[string]string{"a": NodeRunning},
		StartedAt:  time.Now().UTC(),
	}

	engine := NewEngine()
	if err := engine.AttachStore(ctx, st); err != nil {
		t.Fatal(err)
	}
	engine.SetNodeRunner(func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		t.Error("a node ran for a pipeline that no longer exists")
		return "", nil
	})
	engine.ResumeInterrupted(ctx)

	waitFor(t, "the orphan run to be closed out", func() bool {
		r, ok := st.run("run-orphan")
		return ok && r.Status == "failed" && r.FinishedAt != nil
	})
	final, _ := st.run("run-orphan")
	if !strings.Contains(final.NodeResults["_run"], "no longer exists") {
		t.Errorf("the orphan run does not say why it was closed: %+v", final.NodeResults)
	}
}
