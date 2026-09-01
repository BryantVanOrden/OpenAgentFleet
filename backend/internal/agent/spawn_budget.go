package agent

import (
	"fmt"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Limits on recursive sub-agents.
//
// These are cost controls first and stability controls second. Each spawned
// child runs its own perceive/decide/act loop against a paid model, so an
// unbounded tree turns one prompt-injected web page into an exponential bill.
// Depth 3 with 5 children per task caps a single run at 155 tasks in the worst
// case, which is survivable; unbounded is not.
const (
	maxSpawnDepth      = 3
	maxChildrenPerTask = 5
	// Ceiling across the whole orchestrator, so several shallow trees cannot
	// achieve together what one deep tree is prevented from doing alone.
	maxConcurrentTasks = 32
)

// checkSpawnBudget returns an empty string when a spawn is allowed, or the
// refusal to hand back to the model. The refusal is deliberately phrased as an
// outcome the agent can reason about — it should do the work itself rather than
// retry the spawn.
func (r *Runner) checkSpawnBudget(parent *protocol.Task) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.running) >= maxConcurrentTasks {
		return fmt.Sprintf(
			"refused: the orchestrator is already running %d tasks (limit %d); "+
				"do this step yourself instead of delegating",
			len(r.running), maxConcurrentTasks)
	}

	depth := r.depth[parent.ID]
	if depth >= maxSpawnDepth {
		return fmt.Sprintf(
			"refused: sub-agent depth limit reached (%d); you are already a "+
				"child agent this deep, so complete the work yourself",
			maxSpawnDepth)
	}

	if r.children[parent.ID] >= maxChildrenPerTask {
		return fmt.Sprintf(
			"refused: this task has already spawned %d children (limit %d); "+
				"do the remaining work yourself",
			r.children[parent.ID], maxChildrenPerTask)
	}

	return ""
}

// recordSpawn books a child against its parent's budget and seeds the child's
// own depth.
//
// Depth is tracked in memory rather than persisted. That means a child resumed
// after an orchestrator restart is treated as depth 0 and could nest a further
// three levels. The alternative is a schema column; the in-memory bound is the
// cheap 95% and the concurrency ceiling still holds across a restart, so this
// is a deliberate trade rather than an oversight.
func (r *Runner) recordSpawn(parent, child *protocol.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.children[parent.ID]++
	r.depth[child.ID] = r.depth[parent.ID] + 1
}

// forgetSpawn drops a finished task's budget bookkeeping so the maps do not
// grow for the life of the process.
func (r *Runner) forgetSpawn(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.depth, taskID)
	delete(r.children, taskID)
}
