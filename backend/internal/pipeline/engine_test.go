package pipeline

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// gatedRunner runs nodes one at a time, releasing each only when the test says
// so, which is what lets a test observe a run precisely mid-flight.
type gatedRunner struct {
	release chan struct{}
	started chan string
}

func newGatedRunner() *gatedRunner {
	return &gatedRunner{
		release: make(chan struct{}),
		started: make(chan string, 8),
	}
}

func (g *gatedRunner) run(ctx context.Context, n protocol.PipelineNode) (string, error) {
	g.started <- n.ID
	<-g.release
	return "result of " + n.ID, nil
}

func twoNodePipeline() protocol.WorkflowPipeline {
	return protocol.WorkflowPipeline{
		ID:    "pipe-test",
		Name:  "two step",
		Nodes: []protocol.PipelineNode{node("first"), node("second")},
		Edges: []protocol.PipelineEdge{edge("first", "second")},
	}
}

// A run handed back to a caller is a snapshot of that moment. It used to share
// its NodeResults map with the goroutine still executing the pipeline, so the
// API layer serialised a map that was being written to — a concurrent map
// access, which is a fatal error the process cannot recover from.
func TestRunHandedToACallerIsNotWrittenToByTheExecutingPipeline(t *testing.T) {
	e := NewEngine()
	g := newGatedRunner()
	e.SetNodeRunner(g.run)
	if _, err := e.SavePipeline(context.Background(), twoNodePipeline()); err != nil {
		t.Fatalf("save: %v", err)
	}

	run, err := e.TriggerRun(context.Background(), "pipe-test")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if len(run.NodeResults) != 0 {
		t.Fatalf("a run starts with no results, got %v", run.NodeResults)
	}

	<-g.started      // first node is in the runner
	close(g.release) // let the whole pipeline finish
	waitForStatus(t, e, "completed")

	if len(run.NodeResults) != 0 {
		t.Fatalf("the value returned to the caller was mutated by the running pipeline: %v",
			run.NodeResults)
	}
}

// The same guarantee for the listing endpoint, which is the one an operator
// polls while a pipeline is running.
func TestListedRunsAreNotWrittenToByTheExecutingPipeline(t *testing.T) {
	e := NewEngine()
	g := newGatedRunner()
	e.SetNodeRunner(g.run)
	if _, err := e.SavePipeline(context.Background(), twoNodePipeline()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := e.TriggerRun(context.Background(), "pipe-test"); err != nil {
		t.Fatalf("trigger: %v", err)
	}

	<-g.started
	listed := e.ListRuns(context.Background(), "pipe-test")
	if len(listed) != 1 {
		t.Fatalf("expected one run, got %d", len(listed))
	}
	before := len(listed[0].NodeResults)

	close(g.release)
	waitForStatus(t, e, "completed")

	if after := len(listed[0].NodeResults); after != before {
		t.Fatalf("a listed run gained %d results after being returned; the map is shared "+
			"with the executing pipeline", after-before)
	}
}

// Serialising a run while its pipeline progresses is what the HTTP handlers do.
// Under -race this catches the shared map directly; without it, it still
// exercises the path that used to crash the orchestrator.
func TestRunsCanBeSerialisedWhileTheirPipelineProgresses(t *testing.T) {
	e := NewEngine()
	e.SetNodeRunner(func(ctx context.Context, n protocol.PipelineNode) (string, error) {
		time.Sleep(2 * time.Millisecond)
		return "result of " + n.ID, nil
	})
	p := twoNodePipeline()
	p.Nodes = append(p.Nodes, node("third"), node("fourth"))
	p.Edges = append(p.Edges, edge("second", "third"), edge("third", "fourth"))
	if _, err := e.SavePipeline(context.Background(), p); err != nil {
		t.Fatalf("save: %v", err)
	}

	run, err := e.TriggerRun(context.Background(), "pipe-test")
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if _, err := json.Marshal(run); err != nil {
					t.Errorf("marshal returned run: %v", err)
					return
				}
				for _, r := range e.ListRuns(context.Background(), "pipe-test") {
					if _, err := json.Marshal(r); err != nil {
						t.Errorf("marshal listed run: %v", err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	waitForStatus(t, e, "completed")
}

func waitForStatus(t *testing.T, e *Engine, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs := e.ListRuns(context.Background(), "pipe-test")
		if len(runs) == 1 && runs[0].Status == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("run never reached status %q", want)
}
