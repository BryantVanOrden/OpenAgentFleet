package pipeline

import (
	"errors"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

func node(id string) protocol.PipelineNode {
	return protocol.PipelineNode{ID: id, GoalTemplate: "do " + id, InstanceID: "inst-" + id}
}

func edge(from, to string) protocol.PipelineEdge {
	return protocol.PipelineEdge{FromNodeID: from, ToNodeID: to}
}

// The bug this pins: execution walked the node list in declaration order and
// ignored the edges, so a node could run before what it depended on.
func TestTopoOrderHonoursDependenciesNotDeclarationOrder(t *testing.T) {
	p := protocol.WorkflowPipeline{
		Name: "p",
		// Declared backwards on purpose.
		Nodes: []protocol.PipelineNode{node("audit"), node("build"), node("research")},
		Edges: []protocol.PipelineEdge{edge("research", "build"), edge("build", "audit")},
	}
	order, err := TopoOrder(p)
	if err != nil {
		t.Fatalf("topo: %v", err)
	}
	got := []string{order[0].ID, order[1].ID, order[2].ID}
	want := []string{"research", "build", "audit"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// With no edges the order is the order it was written, which is what pipelines
// built before edges existed relied on.
func TestTopoOrderKeepsDeclarationOrderWithoutEdges(t *testing.T) {
	p := protocol.WorkflowPipeline{
		Name:  "p",
		Nodes: []protocol.PipelineNode{node("a"), node("b"), node("c")},
	}
	order, _ := TopoOrder(p)
	for i, want := range []string{"a", "b", "c"} {
		if order[i].ID != want {
			t.Errorf("position %d = %s, want %s", i, order[i].ID, want)
		}
	}
}

func TestCycleIsRejected(t *testing.T) {
	p := protocol.WorkflowPipeline{
		Name:  "p",
		Nodes: []protocol.PipelineNode{node("a"), node("b")},
		Edges: []protocol.PipelineEdge{edge("a", "b"), edge("b", "a")},
	}
	if _, err := TopoOrder(p); !errors.Is(err, ErrCycle) {
		t.Errorf("got %v, want ErrCycle", err)
	}
	if err := Validate(p); err == nil {
		t.Error("a cyclic pipeline was accepted")
	}
}

// Every way a pipeline can look configured and do nothing.
func TestValidateRejectsUnrunnablePipelines(t *testing.T) {
	cases := []struct {
		name string
		p    protocol.WorkflowPipeline
	}{
		{"no name", protocol.WorkflowPipeline{Nodes: []protocol.PipelineNode{node("a")}}},
		{"no nodes", protocol.WorkflowPipeline{Name: "p"}},
		{"node with no id", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{{GoalTemplate: "x", InstanceID: "i"}}}},
		{"duplicate ids", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{node("a"), node("a")}}},
		{"node with no goal", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{{ID: "a", InstanceID: "i"}}}},
		{"node with nothing to run on", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{{ID: "a", GoalTemplate: "x"}}}},
		{"edge from nowhere", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{node("a")},
			Edges: []protocol.PipelineEdge{edge("ghost", "a")}}},
		{"edge to nowhere", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{node("a")},
			Edges: []protocol.PipelineEdge{edge("a", "ghost")}}},
		{"self dependency", protocol.WorkflowPipeline{Name: "p",
			Nodes: []protocol.PipelineNode{node("a")},
			Edges: []protocol.PipelineEdge{edge("a", "a")}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.p); err == nil {
				t.Error("accepted a pipeline that cannot run")
			}
		})
	}
}

func TestValidateAcceptsARealDAG(t *testing.T) {
	p := protocol.WorkflowPipeline{
		Name:  "p",
		Nodes: []protocol.PipelineNode{node("a"), node("b"), node("c")},
		Edges: []protocol.PipelineEdge{edge("a", "b"), edge("a", "c")},
	}
	if err := Validate(p); err != nil {
		t.Fatalf("rejected a valid DAG: %v", err)
	}
}

// A diamond: both middle nodes wait on the first, the last waits on both.
func TestDiamondOrdering(t *testing.T) {
	p := protocol.WorkflowPipeline{
		Name:  "p",
		Nodes: []protocol.PipelineNode{node("start"), node("left"), node("right"), node("join")},
		Edges: []protocol.PipelineEdge{
			edge("start", "left"), edge("start", "right"),
			edge("left", "join"), edge("right", "join"),
		},
	}
	order, err := TopoOrder(p)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n.ID] = i
	}
	if pos["start"] != 0 {
		t.Error("start did not run first")
	}
	if pos["join"] != 3 {
		t.Error("join did not run last")
	}
	if pos["left"] < pos["start"] || pos["right"] < pos["start"] {
		t.Error("a branch ran before what it depends on")
	}
}

func TestDependenciesOf(t *testing.T) {
	p := protocol.WorkflowPipeline{
		Nodes: []protocol.PipelineNode{node("a"), node("b"), node("c")},
		Edges: []protocol.PipelineEdge{edge("a", "c"), edge("b", "c")},
	}
	deps := DependenciesOf(p, "c")
	if len(deps) != 2 {
		t.Errorf("c depends on %v, want two nodes", deps)
	}
	if len(DependenciesOf(p, "a")) != 0 {
		t.Error("a should depend on nothing")
	}
}
