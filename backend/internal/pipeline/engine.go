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
}

// PipelineStore is the durable half.
type PipelineStore interface {
	UpsertPipeline(ctx context.Context, p protocol.WorkflowPipeline) error
	ListPipelines(ctx context.Context) ([]protocol.WorkflowPipeline, error)
	DeletePipeline(ctx context.Context, id string) error
}

// AttachStore reloads stored pipelines and writes new ones through.
func (e *Engine) AttachStore(ctx context.Context, st PipelineStore) error {
	saved, err := st.ListPipelines(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = st
	for _, p := range saved {
		e.pipelines[p.ID] = p
	}
	return nil
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
	if e.store != nil {
		_ = e.store.DeletePipeline(ctx, id)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pipelines, id)
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

	order, err := TopoOrder(p)
	if err != nil {
		return protocol.PipelineRun{}, err
	}
	firstNode := ""
	if len(order) > 0 {
		firstNode = order[0].ID
	}

	e.mu.Lock()

	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	run := protocol.PipelineRun{
		ID:            runID,
		PipelineID:    pipelineID,
		Status:        "running",
		CurrentNodeID: firstNode,
		NodeResults:   make(map[string]string),
		StartedAt:     time.Now().UTC(),
	}
	e.runs[runID] = run
	e.mu.Unlock()

	// Real execution, in dependency order. What is stored as a graph now runs
	// as one: the previous loop walked the node list in declaration order and
	// ignored the edges, so a node could "complete" before the node it
	// depended on had started.
	go func() {
		// Detached from the request that started the run: a pipeline outlives
		// the HTTP call that triggered it.
		runCtx := context.WithoutCancel(ctx)

		for _, node := range order {
			e.mu.Lock()
			r := e.runs[runID]
			r.CurrentNodeID = node.ID
			e.runs[runID] = r
			e.mu.Unlock()

			result, err := runner(runCtx, node)

			e.mu.Lock()
			r = e.runs[runID]
			if err != nil {
				// One node failing stops the run. Continuing would hand the
				// next node a dependency that never produced anything, and
				// report the whole pipeline as complete regardless.
				r.NodeResults[node.ID] = "failed: " + err.Error()
				r.Status = "failed"
				now := time.Now().UTC()
				r.FinishedAt = &now
				e.runs[runID] = r
				e.mu.Unlock()
				return
			}
			r.NodeResults[node.ID] = result
			e.runs[runID] = r
			e.mu.Unlock()
		}

		e.mu.Lock()
		r := e.runs[runID]
		r.Status = "completed"
		now := time.Now().UTC()
		r.FinishedAt = &now
		e.runs[runID] = r
		e.mu.Unlock()
	}()

	return run, nil
}

func (e *Engine) ListRuns(ctx context.Context, pipelineID string) []protocol.PipelineRun {
	e.mu.RLock()
	defer e.mu.RUnlock()
	// Empty rather than nil so the JSON is [] and not null; see bus.go.
	out := make([]protocol.PipelineRun, 0)
	for _, r := range e.runs {
		if pipelineID == "" || r.PipelineID == pipelineID {
			out = append(out, r)
		}
	}
	return out
}
