package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// NodeRunner runs one node to completion and returns what it produced.
//
// Injected rather than implemented here: running a node means starting a real
// task on a real instance and waiting for it, which lives in the API layer.
// The engine's job is the graph.
type NodeRunner func(ctx context.Context, node protocol.PipelineNode) (string, error)

// Engine executes multi-bot workflow DAG pipelines.
type Engine struct {
	mu        sync.RWMutex
	pipelines map[string]protocol.WorkflowPipeline
	runs      map[string]protocol.PipelineRun

	// store makes pipelines durable. Optional: with none attached the engine is
	// in-memory only, which is what it was.
	store PipelineStore

	// run is how a node becomes work. With none attached the engine refuses to
	// start a run rather than reporting invented success, which is what it did
	// before: every node slept for half a second and recorded "verified
	// deliverable created" without anything having run.
	run NodeRunner

	// interrupted collects runs found `running` at load — the previous process
	// died under them. ResumeInterrupted drains it once a runner exists.
	interrupted []string
}

// PipelineStore is the durable half — the pipelines themselves and their runs.
type PipelineStore interface {
	UpsertPipeline(ctx context.Context, p protocol.WorkflowPipeline) error
	ListPipelines(ctx context.Context) ([]protocol.WorkflowPipeline, error)
	DeletePipeline(ctx context.Context, id string) error
	UpsertPipelineRun(ctx context.Context, r protocol.PipelineRun) error
	ListPipelineRuns(ctx context.Context, limit int) ([]protocol.PipelineRun, error)
}

// AttachStore reloads stored pipelines and run history, and writes both through.
//
// Runs that were `running` when the previous process died are remembered for
// ResumeInterrupted, which the server calls once the node runner exists — the
// runner is attached after the store, so resuming here would dispatch nodes
// into a nil runner.
func (e *Engine) AttachStore(ctx context.Context, st PipelineStore) error {
	saved, err := st.ListPipelines(ctx)
	if err != nil {
		return err
	}
	runs, err := st.ListPipelineRuns(ctx, 0)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = st
	for _, p := range saved {
		e.pipelines[p.ID] = p
	}
	for _, r := range runs {
		// Anything started since boot is newer than the table.
		if _, live := e.runs[r.ID]; live {
			continue
		}
		e.runs[r.ID] = r
		if r.Status == "running" {
			e.interrupted = append(e.interrupted, r.ID)
		}
	}
	return nil
}

// ResumeInterrupted restarts every run the previous process left in flight.
//
// A run resumes from its last settled node: nodes recorded done, failed or
// skipped keep their state and results, and everything that was waiting or
// mid-flight is executed again. A node that was mid-flight when the process
// died is re-dispatched from the start — its half-finished task cannot be
// rejoined — which re-runs work rather than losing it; for a pipeline node
// that is the right side to err on, and the log says it happened.
func (e *Engine) ResumeInterrupted(ctx context.Context) int {
	e.mu.Lock()
	ids := e.interrupted
	e.interrupted = nil
	runner := e.run
	e.mu.Unlock()

	if len(ids) == 0 {
		return 0
	}
	if runner == nil {
		// Refused loudly rather than silently: a deployment with no runner
		// cannot resume anything, and pretending otherwise re-creates the
		// stuck-at-running lie this feature removes.
		return 0
	}

	resumed := 0
	for _, runID := range ids {
		e.mu.Lock()
		r, okRun := e.runs[runID]
		p, okPipe := e.pipelines[r.PipelineID]
		e.mu.Unlock()
		if !okRun || !okPipe {
			// The pipeline was deleted while its run was in flight. The run
			// cannot continue; it is closed out rather than left running forever.
			e.closeOrphanRun(ctx, runID, "the pipeline no longer exists")
			continue
		}

		go func(runID string, p protocol.WorkflowPipeline, prior protocol.PipelineRun) {
			newExecutorResuming(e, runID, p, prior).Run(context.WithoutCancel(ctx), runner)
		}(runID, p, r)
		resumed++
	}
	return resumed
}

// closeOrphanRun terminates a run that cannot be resumed.
func (e *Engine) closeOrphanRun(ctx context.Context, runID, reason string) {
	e.mu.Lock()
	r, ok := e.runs[runID]
	if ok {
		r.Status = "failed"
		if r.NodeResults == nil {
			r.NodeResults = map[string]string{}
		}
		r.NodeResults["_run"] = "not resumed: " + reason
		now := time.Now().UTC()
		r.FinishedAt = &now
		e.runs[runID] = r
	}
	e.mu.Unlock()
	if ok {
		e.persistRun(ctx, runID)
	}
}

// persistRun writes one run's current snapshot through to the store.
//
// Fire-and-forget on purpose: node transitions are minutes apart, and a failed
// write costs the durable copy one update, not the run.
func (e *Engine) persistRun(ctx context.Context, runID string) {
	e.mu.RLock()
	st := e.store
	r, ok := e.runs[runID]
	var snap protocol.PipelineRun
	if ok {
		snap = snapshotRun(r)
	}
	e.mu.RUnlock()
	if st == nil || !ok {
		return
	}
	go func() {
		_ = st.UpsertPipelineRun(context.WithoutCancel(ctx), snap)
	}()
}

// SetNodeRunner attaches the thing that actually does the work.
func (e *Engine) SetNodeRunner(r NodeRunner) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.run = r
}

var GlobalEngine = NewEngine()

func NewEngine() *Engine {
	return &Engine{
		pipelines: make(map[string]protocol.WorkflowPipeline),
		runs:      make(map[string]protocol.PipelineRun),
	}
}

// SavePipeline stores a pipeline after checking it can actually run.
func (e *Engine) SavePipeline(ctx context.Context, p protocol.WorkflowPipeline) (protocol.WorkflowPipeline, error) {
	if err := Validate(p); err != nil {
		return protocol.WorkflowPipeline{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if p.ID == "" {
		p.ID = fmt.Sprintf("pipe-%d", time.Now().UnixNano())
	}
	if existing, ok := e.pipelines[p.ID]; ok {
		p.CreatedAt = existing.CreatedAt
	} else {
		p.CreatedAt = time.Now().UTC()
	}
	p.UpdatedAt = time.Now().UTC()
	e.pipelines[p.ID] = p
	st := e.store
	e.mu.Unlock()

	if st != nil {
		if err := st.UpsertPipeline(ctx, p); err != nil {
			// Reported rather than swallowed: a pipeline that looks saved and
			// is gone after the next deploy is the failure this replaced.
			e.mu.Lock()
			return protocol.WorkflowPipeline{}, err
		}
	}
	e.mu.Lock()
	return p, nil
}

func (e *Engine) ListPipelines(ctx context.Context) []protocol.WorkflowPipeline {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]protocol.WorkflowPipeline, 0, len(e.pipelines))
	for _, p := range e.pipelines {
		out = append(out, p)
	}
	return out
}

func (e *Engine) GetPipeline(ctx context.Context, id string) (protocol.WorkflowPipeline, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.pipelines[id]
	return p, ok
}

func (e *Engine) DeletePipeline(ctx context.Context, id string) {
	e.mu.Lock()
	delete(e.pipelines, id)
	st := e.store
	e.mu.Unlock()

	if st != nil {
		_ = st.DeletePipeline(ctx, id)
	}
}

func (e *Engine) TriggerRun(ctx context.Context, pipelineID string) (protocol.PipelineRun, error) {
	e.mu.Lock()
	p, ok := e.pipelines[pipelineID]
	if !ok {
		e.mu.Unlock()
		return protocol.PipelineRun{}, fmt.Errorf("pipeline %q not found", pipelineID)
	}

	runner := e.run
	e.mu.Unlock()

	if runner == nil {
		return protocol.PipelineRun{}, errors.New(
			"pipelines cannot run: nothing is wired up to execute a node")
	}

	// Checked before a run id exists: a cyclic graph can never execute, and
	// recording a run that immediately fails would put a permanent failure in
	// the history for a pipeline that was never startable.
	order, err := TopoOrder(p)
	if err != nil {
		return protocol.PipelineRun{}, err
	}
	firstNode := ""
	if len(order) > 0 {
		firstNode = order[0].ID
	}

	states := make(map[string]string, len(p.Nodes))
	for _, n := range p.Nodes {
		states[n.ID] = NodeWaiting
	}

	e.mu.Lock()
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	run := protocol.PipelineRun{
		ID:            runID,
		PipelineID:    pipelineID,
		Status:        "running",
		CurrentNodeID: firstNode,
		NodeResults:   make(map[string]string),
		NodeStates:    states,
		StartedAt:     time.Now().UTC(),
	}
	e.runs[runID] = run
	snapshot := snapshotRun(run)
	e.mu.Unlock()

	// Durable from birth: a run that dies with the process must be findable as
	// `running`-then-resumed, not absent as though it never started.
	e.persistRun(ctx, runID)

	// Real execution: every node whose dependencies have settled runs, several
	// at a time, with each incoming edge's condition deciding whether the node
	// runs or is skipped. What is drawn as a graph now executes as one.
	//
	// Two things this replaced. The original loop walked the node list in
	// declaration order and ignored the edges entirely, so a node could
	// "complete" before the node it depended on had started. The version after
	// that honoured the edges but ran strictly one node at a time and ignored
	// every condition, so a pipeline built to fan work out to three bots took
	// three times as long as it should and ran both sides of a success/failure
	// branch.
	go func() {
		// Detached from the request that started the run: a pipeline outlives
		// the HTTP call that triggered it.
		newExecutor(e, runID, p).Run(context.WithoutCancel(ctx), runner)
	}()

	return snapshot, nil
}

func (e *Engine) ListRuns(ctx context.Context, pipelineID string) []protocol.PipelineRun {
	e.mu.RLock()
	defer e.mu.RUnlock()
	// Empty rather than nil so the JSON is [] and not null; see bus.go.
	out := make([]protocol.PipelineRun, 0)
	for _, r := range e.runs {
		if pipelineID == "" || r.PipelineID == pipelineID {
			out = append(out, snapshotRun(r))
		}
	}
	return out
}

// snapshotRun detaches a run from the goroutine still executing it.
//
// PipelineRun carries a map, so handing the struct out by value still shares
// the results with the executor. The API layer then serialises that map while
// a node writes to it, which is a concurrent map access — a fatal error, not a
// recoverable one, so polling a running pipeline could take the orchestrator
// down.
func snapshotRun(r protocol.PipelineRun) protocol.PipelineRun {
	results := make(map[string]string, len(r.NodeResults))
	for k, v := range r.NodeResults {
		results[k] = v
	}
	r.NodeResults = results

	// NodeStates needs the same treatment for the same reason: it is a map on a
	// struct handed out by value while the executor is still writing to it.
	states := make(map[string]string, len(r.NodeStates))
	for k, v := range r.NodeStates {
		states[k] = v
	}
	r.NodeStates = states

	if r.FinishedAt != nil {
		at := *r.FinishedAt
		r.FinishedAt = &at
	}
	return r
}
