package swarm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The phase field on swarm messages was recorded and displayed and nothing
// gated on it — a swarm started every member at once whatever the message
// history said. These pin the barrier: a plan-first swarm holds execution
// until every member's plan artifact is in.

// phaseHarness records every task the coordinator starts.
type phaseHarness struct {
	mu    sync.Mutex
	tasks []string // "<instance>: <goal first line>"
	goals map[string]string
}

func newPhaseHarness() (*Coordinator, *phaseHarness) {
	h := &phaseHarness{goals: map[string]string{}}
	c := NewCoordinator()
	c.Wire(
		func(ctx context.Context, instanceID, goal, source string) (string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.tasks = append(h.tasks, instanceID)
			h.goals[instanceID] = goal
			return fmt.Sprintf("task-%d", len(h.tasks)), nil
		},
		func(ctx context.Context, id string) (string, string, error) {
			return "bot-" + id, "fullstack_dev", nil
		},
		nil,
	)
	return c, h
}

func (h *phaseHarness) started() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.tasks...)
}

func (h *phaseHarness) goalOf(instanceID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.goals[instanceID]
}

func twoMembers() []protocol.SwarmMember {
	return []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "Architect"},
		{InstanceID: "i-2", Role: "Auditor"},
	}
}

func TestPlanFirstHoldsExecutionUntilEveryPlanIsIn(t *testing.T) {
	ctx := context.Background()
	c, h := newPhaseHarness()

	sw, err := c.CreateSwarm(ctx, "Launch", "ship it", twoMembers(), true)
	if err != nil {
		t.Fatal(err)
	}
	if sw.Phase != "planning" {
		t.Fatalf("phase = %q, want planning", sw.Phase)
	}

	// Both members were started — but with PLANNING goals, not the work.
	if got := h.started(); len(got) != 2 {
		t.Fatalf("started %v, want both members' planning tasks", got)
	}
	if g := h.goalOf("i-1"); !strings.Contains(g, "PLANNING") || !strings.Contains(g, "Do NOT start the work") {
		t.Errorf("member goal is not a planning goal: %q", g)
	}

	// The barrier's teeth: work product during planning is refused, with the
	// reason in the error an agent will actually read.
	if _, err := c.PublishArtifact(ctx, sw.ID, "the patch", "bot-i-1", "code_patch", "diff"); err == nil {
		t.Fatal("a non-plan artifact was accepted during planning")
	} else if !strings.Contains(err.Error(), "planning phase") {
		t.Errorf("the refusal does not explain the phase: %v", err)
	}

	// First plan lands: still planning, execution still held.
	if _, err := c.PublishArtifact(ctx, sw.ID, "arch plan", "bot-i-1", "plan", "step 1..."); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.GetSwarm(ctx, sw.ID); got.Phase != "planning" {
		t.Fatalf("phase advanced with one of two plans in: %q", got.Phase)
	}
	if got := h.started(); len(got) != 2 {
		t.Fatalf("execution started early: %v", got)
	}

	// Second plan lands: the barrier lifts on its own.
	if _, err := c.PublishArtifact(ctx, sw.ID, "audit plan", "bot-i-2", "plan", "step A..."); err != nil {
		t.Fatal(err)
	}
	got, _ := c.GetSwarm(ctx, sw.ID)
	if got.Phase != "execution" {
		t.Fatalf("phase = %q after all plans, want execution", got.Phase)
	}
	if started := h.started(); len(started) != 4 {
		t.Fatalf("started %v, want 2 planning + 2 execution tasks", started)
	}

	// Execution goals carry the agreed plans, so the work starts from what the
	// team decided rather than from the mission alone.
	if g := h.goalOf("i-2"); !strings.Contains(g, "The team planned before starting") ||
		!strings.Contains(g, "arch plan") {
		t.Errorf("execution goal does not carry the plans: %q", g)
	}
}

func TestAnOperatorCanAdvanceAStuckPlanningPhase(t *testing.T) {
	ctx := context.Background()
	c, h := newPhaseHarness()

	sw, err := c.CreateSwarm(ctx, "Launch", "ship it", twoMembers(), true)
	if err != nil {
		t.Fatal(err)
	}
	// Only one plan ever arrives — the other member is stuck.
	if _, err := c.PublishArtifact(ctx, sw.ID, "arch plan", "bot-i-1", "plan", "..."); err != nil {
		t.Fatal(err)
	}

	if err := c.AdvancePhase(ctx, sw.ID, "the operator"); err != nil {
		t.Fatalf("manual advance: %v", err)
	}
	got, _ := c.GetSwarm(ctx, sw.ID)
	if got.Phase != "execution" {
		t.Fatalf("phase = %q, want execution", got.Phase)
	}
	if started := h.started(); len(started) != 4 {
		t.Fatalf("started %v, want execution tasks for both members", started)
	}

	// Advancing twice is refused rather than double-starting the fleet.
	if err := c.AdvancePhase(ctx, sw.ID, "the operator"); err == nil {
		t.Fatal("a second advance was accepted and would have started duplicate tasks")
	}
}

func TestASwarmWithoutPlanFirstBehavesAsBefore(t *testing.T) {
	ctx := context.Background()
	c, h := newPhaseHarness()

	sw, err := c.CreateSwarm(ctx, "Launch", "ship it", twoMembers(), false)
	if err != nil {
		t.Fatal(err)
	}
	if sw.Phase != "execution" {
		t.Fatalf("phase = %q, want execution from the start", sw.Phase)
	}
	if g := h.goalOf("i-1"); strings.Contains(g, "PLANNING") {
		t.Errorf("a non-plan-first swarm got a planning goal: %q", g)
	}
	// Work product is accepted immediately.
	if _, err := c.PublishArtifact(ctx, sw.ID, "the patch", "bot-i-1", "code_patch", "diff"); err != nil {
		t.Fatalf("artifact refused outside planning: %v", err)
	}
}
