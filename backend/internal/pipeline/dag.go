package pipeline

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Validation and ordering for pipeline DAGs.
//
// A pipeline used to be accepted whatever it contained — nodes with no goal, a
// dependency on a node that does not exist, a cycle — and the engine then
// walked the node list in declaration order, ignoring the edges entirely. What
// was stored as a graph executed as a list, so nothing that made it a DAG had
// any effect at run time.

// ErrCycle means the dependencies loop and no order can satisfy them.
var ErrCycle = errors.New("the dependencies form a cycle, so no order can run them")

// Validate checks a pipeline is runnable before it is stored.
//
// Rejecting at save is the point: a pipeline is written once and run on a
// schedule, so a graph that cannot execute should fail in front of the person
// writing it rather than at 3am on its first cron tick.
func Validate(p protocol.WorkflowPipeline) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("a pipeline needs a name")
	}
	if len(p.Nodes) == 0 {
		return errors.New("a pipeline needs at least one node")
	}

	seen := make(map[string]bool, len(p.Nodes))
	for _, n := range p.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return errors.New("every node needs an id")
		}
		if seen[id] {
			return fmt.Errorf("two nodes share the id %q", id)
		}
		seen[id] = true

		// A node with nothing to do is the most common way a pipeline looks
		// configured and does nothing: the field was misspelled and silently
		// dropped on decode.
		if strings.TrimSpace(n.GoalTemplate) == "" {
			return fmt.Errorf("node %q has no goal", id)
		}
		if strings.TrimSpace(n.InstanceID) == "" && strings.TrimSpace(n.ArchetypeID) == "" {
			return fmt.Errorf("node %q names neither an instance nor an archetype to run on", id)
		}
	}

	for _, e := range p.Edges {
		if !seen[e.FromNodeID] {
			return fmt.Errorf("an edge comes from %q, which is not a node", e.FromNodeID)
		}
		if !seen[e.ToNodeID] {
			return fmt.Errorf("an edge goes to %q, which is not a node", e.ToNodeID)
		}
		if e.FromNodeID == e.ToNodeID {
			return fmt.Errorf("node %q depends on itself", e.FromNodeID)
		}
		// Conditions are checked here so a typo is a 400 in front of whoever is
		// drawing the graph. A misspelled condition used to be accepted and
		// ignored, along with every correctly spelled one; now that they are
		// evaluated, an unknown one would be a branch that silently never fires.
		if err := ValidateCondition(e.Condition); err != nil {
			return fmt.Errorf("the edge from %q to %q: %w", e.FromNodeID, e.ToNodeID, err)
		}
	}

	if p.MaxParallel < 0 {
		return errors.New("max_parallel cannot be negative; leave it at 0 for the default")
	}

	_, err := TopoOrder(p)
	return err
}

// TopoOrder returns the nodes in an order that satisfies every edge.
//
// Ties are broken by the order the nodes were declared, so a pipeline with no
// dependencies at all runs the way it reads — the previous behaviour, which is
// what makes this change safe for pipelines that never used edges.
func TopoOrder(p protocol.WorkflowPipeline) ([]protocol.PipelineNode, error) {
	byID := make(map[string]protocol.PipelineNode, len(p.Nodes))
	position := make(map[string]int, len(p.Nodes))
	indegree := make(map[string]int, len(p.Nodes))
	for i, n := range p.Nodes {
		byID[n.ID] = n
		position[n.ID] = i
		indegree[n.ID] = 0
	}

	dependents := make(map[string][]string, len(p.Edges))
	for _, e := range p.Edges {
		if _, ok := byID[e.FromNodeID]; !ok {
			continue
		}
		if _, ok := byID[e.ToNodeID]; !ok {
			continue
		}
		dependents[e.FromNodeID] = append(dependents[e.FromNodeID], e.ToNodeID)
		indegree[e.ToNodeID]++
	}

	ready := make([]string, 0, len(p.Nodes))
	for id, d := range indegree {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return position[ready[i]] < position[ready[j]] })

	out := make([]protocol.PipelineNode, 0, len(p.Nodes))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, byID[id])

		next := make([]string, 0, len(dependents[id]))
		for _, dep := range dependents[id] {
			indegree[dep]--
			if indegree[dep] == 0 {
				next = append(next, dep)
			}
		}
		sort.Slice(next, func(i, j int) bool { return position[next[i]] < position[next[j]] })
		ready = append(ready, next...)
		sort.SliceStable(ready, func(i, j int) bool { return position[ready[i]] < position[ready[j]] })
	}

	if len(out) != len(p.Nodes) {
		return nil, ErrCycle
	}
	return out, nil
}

// DependenciesOf lists the nodes a node waits on.
func DependenciesOf(p protocol.WorkflowPipeline, nodeID string) []string {
	var out []string
	for _, e := range p.Edges {
		if e.ToNodeID == nodeID {
			out = append(out, e.FromNodeID)
		}
	}
	return out
}
