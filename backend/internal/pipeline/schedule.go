package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The executor.
//
// It ran nodes one at a time in topological order. That is correct and slow: a
// pipeline whose whole point is "have three bots review this in parallel, then
// summarise" took three times as long as it needed to, and the fan-out that
// makes a DAG worth drawing bought nothing over a list.
//
// This runs every node whose dependencies have finished, concurrently, and
// evaluates each incoming edge's condition to decide whether the node runs at
// all or is skipped.

// DefaultMaxParallel bounds concurrent nodes when a pipeline does not say.
//
// Every node starts a real task on a real desktop, so unbounded fan-out is a way
// to flood the fleet from a single pipeline: a 40-node graph with no edges would
// try to start 40 agents at once.
const DefaultMaxParallel = 4

// Node states, as reported in PipelineRun.NodeStates.
const (
	NodeWaiting = "waiting"
	NodeRunning = "running"
	NodeDone    = "done"
	NodeFailed  = "failed"
	// NodeSkipped means an incoming edge's condition was not met. It is not a
	// failure: a `failure` branch that does not fire because everything worked
	// is the pipeline behaving correctly.
	NodeSkipped = "skipped"
)

// executor holds one run's mutable state.
type executor struct {
	engine   *Engine
	runID    string
	pipeline protocol.WorkflowPipeline

	// byID and dependents are the graph, precomputed.
	byID       map[string]protocol.PipelineNode
	position   map[string]int
	incoming   map[string][]protocol.PipelineEdge
	dependents map[string][]string

	mu      sync.Mutex
	state   map[string]string
	outcome map[string]nodeOutcome
	// failedUnhandled counts failures with no failure-branch to catch them,
	// which is what decides the run's own status.
	failedUnhandled int
}

func newExecutor(e *Engine, runID string, p protocol.WorkflowPipeline) *executor {
	x := &executor{
		engine:     e,
		runID:      runID,
		pipeline:   p,
		byID:       make(map[string]protocol.PipelineNode, len(p.Nodes)),
		position:   make(map[string]int, len(p.Nodes)),
		incoming:   make(map[string][]protocol.PipelineEdge),
		dependents: make(map[string][]string),
		state:      make(map[string]string, len(p.Nodes)),
		outcome:    make(map[string]nodeOutcome, len(p.Nodes)),
	}
	for i, n := range p.Nodes {
		x.byID[n.ID] = n
		x.position[n.ID] = i
		x.state[n.ID] = NodeWaiting
	}
	for _, edge := range p.Edges {
		if _, ok := x.byID[edge.FromNodeID]; !ok {
			continue
		}
		if _, ok := x.byID[edge.ToNodeID]; !ok {
			continue
		}
		x.incoming[edge.ToNodeID] = append(x.incoming[edge.ToNodeID], edge)
		x.dependents[edge.FromNodeID] = append(x.dependents[edge.FromNodeID], edge.ToNodeID)
	}
	return x
}

// newExecutorResuming rebuilds an executor from a run the previous process
// left in flight.
//
// Nodes recorded done, failed or skipped keep their state and — critically —
// their outcomes, because downstream edge conditions test those outcomes: a
// `contains:` branch has to see the same result text after a restart that it
// would have seen without one. Nodes recorded running are demoted to waiting
// and re-dispatched; their half-finished task cannot be rejoined.
func newExecutorResuming(e *Engine, runID string, p protocol.WorkflowPipeline, prior protocol.PipelineRun) *executor {
	x := newExecutor(e, runID, p)
	for id, st := range prior.NodeStates {
		if _, known := x.byID[id]; !known {
			continue // the pipeline changed shape since; unknown nodes are dropped
		}
		switch st {
		case NodeDone:
			x.state[id] = NodeDone
			x.outcome[id] = nodeOutcome{Result: prior.NodeResults[id]}
		case NodeFailed:
			x.state[id] = NodeFailed
			x.outcome[id] = nodeOutcome{Failed: true, Result: strings.TrimPrefix(prior.NodeResults[id], "failed: ")}
			if !x.hasFailureBranchLocked(id) {
				x.failedUnhandled++
			}
		case NodeSkipped:
			x.state[id] = NodeSkipped
			x.outcome[id] = nodeOutcome{Skipped: true}
		default:
			// waiting or running: runs again.
		}
	}
	return x
}

// maxParallel is the pipeline's limit, or the default.
func (x *executor) maxParallel() int {
	if x.pipeline.MaxParallel > 0 {
		return x.pipeline.MaxParallel
	}
	return DefaultMaxParallel
}

// Run executes the whole graph and returns when nothing is left to do.
func (x *executor) Run(ctx context.Context, runner NodeRunner) {
	// A counting semaphore rather than a worker pool: the set of runnable nodes
	// changes as the graph progresses, so there is no fixed queue to hand out.
	sem := make(chan struct{}, x.maxParallel())
	var wg sync.WaitGroup

	for {
		ready := x.claimReady(ctx)
		if len(ready) == 0 {
			// Nothing runnable. Either everything is finished, or work is still
			// in flight and will unblock more when it lands.
			if !x.anyRunning() {
				break
			}
			// Wait for whatever is running to change the picture. A short poll
			// rather than a condition variable: node durations are minutes, so
			// the latency is irrelevant and the code stays obvious.
			select {
			case <-ctx.Done():
				x.cancelRemaining("the run was cancelled")
				wg.Wait()
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}

		for _, node := range ready {
			select {
			case <-ctx.Done():
				x.cancelRemaining("the run was cancelled")
				wg.Wait()
				return
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(n protocol.PipelineNode) {
				defer wg.Done()
				defer func() { <-sem }()
				x.runNode(ctx, runner, n)
			}(node)
		}
	}

	wg.Wait()
	x.finish()
}

// claimReady marks every currently-runnable node as running (or skipped) and
// returns the ones that should actually execute.
//
// Claiming and returning in one locked step is what stops a node being started
// twice: the loop above calls this repeatedly from one goroutine, but a node
// becomes ready as a side effect of another node finishing.
func (x *executor) claimReady(ctx context.Context) []protocol.PipelineNode {
	x.mu.Lock()
	skipped := 0

	var ready []protocol.PipelineNode
	for {
		progressed := false
		for id, st := range x.state {
			if st != NodeWaiting {
				continue
			}
			if !x.dependenciesSettledLocked(id) {
				continue
			}

			if reason, ok := x.conditionsMetLocked(id); !ok {
				// Skipped, and its own dependents may now be settled, so the
				// loop goes round again to propagate.
				x.state[id] = NodeSkipped
				x.outcome[id] = nodeOutcome{Skipped: true}
				x.recordResultLocked(id, "skipped: "+reason)
				skipped++
				progressed = true
				continue
			}
			x.state[id] = NodeRunning
			ready = append(ready, x.byID[id])
		}
		if !progressed {
			break
		}
	}

	// Declaration order, so a pipeline with no edges starts in the order it
	// reads and the logs are predictable. Go's map iteration is random.
	sort.Slice(ready, func(i, j int) bool {
		return x.position[ready[i].ID] < x.position[ready[j].ID]
	})
	x.mu.Unlock()

	// A skip settles a node as surely as a result does, so it goes to the
	// durable copy at the same moment it goes to the in-memory one.
	if skipped > 0 {
		x.engine.persistRun(ctx, x.runID)
	}
	return ready
}

// dependenciesSettledLocked reports whether every upstream node has finished.
func (x *executor) dependenciesSettledLocked(id string) bool {
	for _, edge := range x.incoming[id] {
		switch x.state[edge.FromNodeID] {
		case NodeDone, NodeFailed, NodeSkipped:
		default:
			return false
		}
	}
	return true
}

// conditionsMetLocked evaluates every incoming edge.
//
// All of them have to pass. A node with two dependencies waits for both, so it
// also requires both to have ended the way its edges asked for — an AND, which
// is what a reader of the graph expects. Alternatives ("run if either branch
// succeeded") are expressed by giving the node one dependency, not by changing
// this rule.
func (x *executor) conditionsMetLocked(id string) (string, bool) {
	for _, edge := range x.incoming[id] {
		up := x.outcome[edge.FromNodeID]
		if evaluateCondition(edge.Condition, up) {
			continue
		}
		cond := strings.TrimSpace(edge.Condition)
		if cond == "" {
			cond = "always"
		}
		if up.Skipped {
			return fmt.Sprintf("%s was skipped", edge.FromNodeID), false
		}
		return fmt.Sprintf("%s did not satisfy %q on the edge from it", edge.FromNodeID, cond), false
	}
	return "", true
}

func (x *executor) runNode(ctx context.Context, runner NodeRunner, node protocol.PipelineNode) {
	x.engine.mu.Lock()
	if r, ok := x.engine.runs[x.runID]; ok {
		// CurrentNodeID predates parallel execution and cannot describe several
		// nodes at once. Kept as "most recently started" for anything still
		// reading it; NodeStates is the accurate answer.
		r.CurrentNodeID = node.ID
		x.engine.runs[x.runID] = r
	}
	x.engine.mu.Unlock()
	x.publishState(node.ID, NodeRunning)

	result, err := runner(ctx, node)

	x.mu.Lock()
	if err != nil {
		x.state[node.ID] = NodeFailed
		x.outcome[node.ID] = nodeOutcome{Failed: true, Result: err.Error()}
		if !x.hasFailureBranchLocked(node.ID) {
			// Only an unhandled failure fails the run. A graph with an explicit
			// failure branch has said what to do about it, and reporting the
			// whole pipeline as failed anyway would make error handling
			// pointless.
			x.failedUnhandled++
		}
		x.recordResultLocked(node.ID, "failed: "+err.Error())
		x.mu.Unlock()
		x.engine.persistRun(ctx, x.runID)
		return
	}
	x.state[node.ID] = NodeDone
	x.outcome[node.ID] = nodeOutcome{Result: result}
	x.recordResultLocked(node.ID, result)
	x.mu.Unlock()
	x.engine.persistRun(ctx, x.runID)
}

// hasFailureBranchLocked reports whether the graph handles this node failing.
func (x *executor) hasFailureBranchLocked(id string) bool {
	for _, dep := range x.dependents[id] {
		for _, edge := range x.incoming[dep] {
			if edge.FromNodeID != id {
				continue
			}
			if kind, _, _ := splitCondition(strings.TrimSpace(edge.Condition)); kind == "failure" {
				return true
			}
		}
	}
	return false
}

// recordResultLocked writes a node's result into the shared run. Callers hold
// x.mu; the engine's own lock is taken inside.
func (x *executor) recordResultLocked(nodeID, result string) {
	states := make(map[string]string, len(x.state))
	for k, v := range x.state {
		states[k] = v
	}

	x.engine.mu.Lock()
	defer x.engine.mu.Unlock()
	r, ok := x.engine.runs[x.runID]
	if !ok {
		return
	}
	if r.NodeResults == nil {
		r.NodeResults = make(map[string]string)
	}
	r.NodeResults[nodeID] = result
	r.NodeStates = states
	x.engine.runs[x.runID] = r
}

func (x *executor) publishState(nodeID, state string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.state[nodeID] = state
	states := make(map[string]string, len(x.state))
	for k, v := range x.state {
		states[k] = v
	}

	x.engine.mu.Lock()
	if r, ok := x.engine.runs[x.runID]; ok {
		r.NodeStates = states
		x.engine.runs[x.runID] = r
	}
	x.engine.mu.Unlock()

	// Persisted too: after a crash, the durable copy should say which nodes
	// were mid-flight, because those are exactly the ones resume re-dispatches.
	x.engine.persistRun(context.Background(), x.runID)
}

func (x *executor) anyRunning() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, st := range x.state {
		if st == NodeRunning {
			return true
		}
	}
	return false
}

// cancelRemaining marks everything that never started, so a cancelled run does
// not leave nodes reading "waiting" forever.
func (x *executor) cancelRemaining(reason string) {
	x.mu.Lock()
	for id, st := range x.state {
		if st == NodeWaiting {
			x.state[id] = NodeSkipped
			x.outcome[id] = nodeOutcome{Skipped: true}
			x.recordResultLocked(id, "skipped: "+reason)
		}
	}
	failed := x.failedUnhandled
	x.mu.Unlock()

	x.setStatus(statusFor(failed, true))
}

func (x *executor) finish() {
	x.mu.Lock()
	failed := x.failedUnhandled
	x.mu.Unlock()
	x.setStatus(statusFor(failed, false))
}

func statusFor(failedUnhandled int, cancelled bool) string {
	switch {
	case cancelled:
		return "cancelled"
	case failedUnhandled > 0:
		return "failed"
	default:
		return "completed"
	}
}

func (x *executor) setStatus(status string) {
	x.engine.mu.Lock()
	r, ok := x.engine.runs[x.runID]
	// A terminal status is not overwritten: cancelRemaining and finish can both
	// land on a cancelled run.
	if !ok || r.FinishedAt != nil {
		x.engine.mu.Unlock()
		return
	}
	r.Status = status
	r.CurrentNodeID = ""
	now := time.Now().UTC()
	r.FinishedAt = &now
	x.engine.runs[x.runID] = r
	x.engine.mu.Unlock()

	// The terminal write is the one that matters most: it is the difference
	// between history and a run stuck at `running` forever.
	x.engine.persistRun(context.Background(), x.runID)
}
