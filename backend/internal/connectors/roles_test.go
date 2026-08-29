package connectors

import (
	"reflect"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

func combo(id string, roles map[string]string) protocol.ModelCombo {
	return protocol.ModelCombo{ID: id, Name: id, Roles: roles}
}

// The point of the feature: the same chain sends different roles to different
// models.
func TestChainRoutesRolesToDifferentModels(t *testing.T) {
	c := combo("combo-1", map[string]string{
		protocol.RoleVision:    "fast-eyes",
		protocol.RoleReasoning: "deep-brain",
	})
	entries := []string{"combo-1"}

	if got := ExpandChainForRole(entries, []protocol.ModelCombo{c}, protocol.RoleVision); !reflect.DeepEqual(got, []string{"fast-eyes"}) {
		t.Errorf("vision resolved to %v", got)
	}
	if got := ExpandChainForRole(entries, []protocol.ModelCombo{c}, protocol.RoleReasoning); !reflect.DeepEqual(got, []string{"deep-brain"}) {
		t.Errorf("reasoning resolved to %v", got)
	}
}

// A brain-and-hands pair must still answer roles it does not name, or picking
// two models would quietly stop the fleet summarising anything.
func TestSimpleComboFallsBackWithinItself(t *testing.T) {
	c := combo("combo-1", map[string]string{
		protocol.RoleVision:    "eyes",
		protocol.RoleReasoning: "brain",
	})
	combos := []protocol.ModelCombo{c}

	// Summarise and refine are analysis, so they land on the brain.
	for _, role := range []string{protocol.RoleSummarize, protocol.RoleRefine} {
		if got := ProviderForRole(c, role); got != "brain" {
			t.Errorf("%s resolved to %q, want brain", role, got)
		}
	}
	// Chat carries a screenshot, so it prefers the model that can see.
	if got := ProviderForRole(c, protocol.RoleChat); got != "eyes" {
		t.Errorf("chat resolved to %q, want eyes", got)
	}
	if got := ExpandChainForRole([]string{"combo-1"}, combos, protocol.RoleSummarize); len(got) != 1 {
		t.Errorf("summarise chain = %v", got)
	}
}

// Vision must never fall back to a model chosen for text. The registry drops
// vision-blind connectors when a screenshot is present, so a fallback here
// would produce an agent that is skipped every turn.
func TestVisionNeverFallsBackToATextRole(t *testing.T) {
	c := combo("combo-1", map[string]string{protocol.RoleReasoning: "text-only"})
	if got := ProviderForRole(c, protocol.RoleVision); got != "" {
		t.Errorf("vision fell back to %q; it must resolve to nothing", got)
	}
	// And the chain simply moves on rather than routing eyes to a text model.
	got := ExpandChainForRole([]string{"combo-1", "plain-vision"},
		[]protocol.ModelCombo{c}, protocol.RoleVision)
	if !reflect.DeepEqual(got, []string{"plain-vision"}) {
		t.Errorf("chain = %v, want the plain provider only", got)
	}
}

// A plain provider answers every role — that is the old single-model behaviour
// and it has to keep working.
func TestPlainProviderServesEveryRole(t *testing.T) {
	for _, role := range protocol.ModelRoles {
		got := ExpandChainForRole([]string{"solo"}, nil, role)
		if !reflect.DeepEqual(got, []string{"solo"}) {
			t.Errorf("%s resolved to %v", role, got)
		}
	}
}

// Combinations and providers mix in one chain, in the order given.
func TestComboAndProviderMixInOrder(t *testing.T) {
	c := combo("combo-1", map[string]string{protocol.RoleVision: "eyes"})
	got := ExpandChainForRole([]string{"combo-1", "backup"},
		[]protocol.ModelCombo{c}, protocol.RoleVision)
	if !reflect.DeepEqual(got, []string{"eyes", "backup"}) {
		t.Errorf("chain = %v, want [eyes backup]", got)
	}
}

// Two entries resolving to the same provider must not retry it twice: the
// second attempt is a duplicate of the failure that caused the fallback.

func TestResolvedChainDropsDuplicates(t *testing.T) {
	a := combo("a", map[string]string{protocol.RoleVision: "shared"})
	b := combo("b", map[string]string{protocol.RoleVision: "shared"})
	got := ExpandChainForRole([]string{"a", "b", "shared"},
		[]protocol.ModelCombo{a, b}, protocol.RoleVision)
	if !reflect.DeepEqual(got, []string{"shared"}) {
		t.Errorf("chain = %v, want one entry", got)
	}
}

// Deleting a provider or a combination must not break bots that referenced it.
func TestUnknownEntriesAreSkipped(t *testing.T) {
	c := combo("combo-1", map[string]string{protocol.RoleVision: "eyes"})
	got := ExpandChainForRole([]string{"combo-1", "deleted-combo-or-provider"},
		[]protocol.ModelCombo{c}, protocol.RoleVision)
	// The unknown entry is treated as a provider ID; the registry drops it when
	// it fails to build, which is where a deleted provider is already handled.
	if len(got) != 2 {
		t.Errorf("chain = %v", got)
	}
}

func TestSimpleClassification(t *testing.T) {
	simple := combo("s", map[string]string{
		protocol.RoleVision: "a", protocol.RoleReasoning: "b"})
	if !simple.Simple() {
		t.Error("brain + hands should classify as simple")
	}
	complexOne := combo("c", map[string]string{
		protocol.RoleVision: "a", protocol.RoleSummarize: "b"})
	if complexOne.Simple() {
		t.Error("a combination naming other roles is not simple")
	}
}
