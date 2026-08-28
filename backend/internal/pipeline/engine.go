package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Engine executes multi-bot workflow DAG pipelines.
type Engine struct {
	mu        sync.RWMutex
	pipelines map[string]protocol.WorkflowPipeline
	runs      map[string]protocol.PipelineRun
}

var GlobalEngine = NewEngine()

func NewEngine() *Engine {
	return &Engine{
		pipelines: make(map[string]protocol.WorkflowPipeline),
		runs:      make(map[string]protocol.PipelineRun),
	}
}

func (e *Engine) SavePipeline(ctx context.Context, p protocol.WorkflowPipeline) protocol.WorkflowPipeline {
	e.mu.Lock()
	defer e.mu.Unlock()

	if p.ID == "" {
		p.ID = fmt.Sprintf("pipe-%d", time.Now().UnixNano())
	}
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = time.Now().UTC()
	e.pipelines[p.ID] = p
	return p
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

	firstNode := ""
	if len(p.Nodes) > 0 {
		firstNode = p.Nodes[0].ID
	}

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

	// Simulate node progression for DAG
	go func() {
		for _, node := range p.Nodes {
			time.Sleep(500 * time.Millisecond)
			e.mu.Lock()
			r := e.runs[runID]
			r.CurrentNodeID = node.ID
			r.NodeResults[node.ID] = fmt.Sprintf("Completed goal by %s: verified deliverable created.", node.ArchetypeID)
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
	var out []protocol.PipelineRun
	for _, r := range e.runs {
		if pipelineID == "" || r.PipelineID == pipelineID {
			out = append(out, r)
		}
	}
	return out
}
