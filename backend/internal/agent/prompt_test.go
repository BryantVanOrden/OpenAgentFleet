package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A bot must know its own name and what it is for.
//
// The bug: chat was told only the sandbox name, so asked "what are you good
// at?" in a fresh chat -- no history to infer from -- the honest answer was
// "I don't know". Identity is now shared with the runner so the two cannot
// describe the same bot differently.
func TestIdentityTellsTheAgentWhoItIs(t *testing.T) {
	inst := &protocol.Instance{
		Name:              "Researcher",
		ArchetypeID:       "cyber_ops",
		PreinstalledTools: []string{"nmap", "curl"},
		SystemPrompt:      "You are relentless about citing sources.",
		Profile:           protocol.TierProfile{Name: "standard", VCPU: 2, MemoryMB: 4096, DiskGB: 20},
		ShellAccess:       true,
	}

	got := Identity(inst)

	for _, want := range []string{
		"Researcher", // its name
		"nmap",       // what it actually has
		"You are relentless about citing sources.", // the operator's persona
	} {
		if !strings.Contains(got, want) {
			t.Errorf("identity does not mention %q:\n%s", want, got)
		}
	}

	// The archetype's human-readable role, not just the raw id.
	if tmpl := protocol.BotTemplateByID("cyber_ops"); tmpl != nil {
		if !strings.Contains(got, tmpl.Name) {
			t.Errorf("identity does not name the role %q:\n%s", tmpl.Name, got)
		}
	}

	// Tools are stated plainly now: the orchestrator replaces the archetype's
	// wish list with what actually answered, so hedging them as unverified
	// told the agent not to trust true information.
	if strings.Contains(got, "NOT guaranteed installed") {
		t.Error("tools are verified before this runs; they should not be hedged")
	}
}

// A bot created without an explicit personality inherits its archetype's.
func TestIdentityIsEmptyHandedWithoutAPersona(t *testing.T) {
	bare := &protocol.Instance{Name: "Blank"}
	got := Identity(bare)
	if !strings.Contains(got, "Blank") {
		t.Errorf("identity must still name the bot:\n%s", got)
	}
	if strings.Contains(got, "persona and guidelines") {
		t.Errorf("no persona was set, so none should be claimed:\n%s", got)
	}
}
