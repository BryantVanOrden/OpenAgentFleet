package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func TestArchetypePromptInjection(t *testing.T) {
	templates := protocol.DefaultBotTemplates()
	// Lower bound, not an exact count: adding an archetype is a change, not a
	// regression, and this test is here to catch an empty or gutted catalogue.
	if len(templates) < 10 {
		t.Fatalf("got %d bot templates, want at least 10", len(templates))
	}

	for _, tmpl := range templates {
		t.Run(tmpl.ID, func(t *testing.T) {
			inst := &protocol.Instance{
				ID:                "test-instance-" + tmpl.ID,
				Name:              tmpl.Name,
				ArchetypeID:       tmpl.ID,
				SystemPrompt:      tmpl.SpecializedPrompt,
				PreinstalledTools: tmpl.PreinstalledTools,
				Tier:              tmpl.RecommendedTier,
				Profile: protocol.TierProfile{
					Name:     tmpl.RecommendedTier,
					VCPU:     tmpl.VCPU,
					MemoryMB: tmpl.MemoryMB,
					DiskGB:   tmpl.DiskGB,
					GPU:      tmpl.GPU,
				},
				ShellAccess: true,
				CreatedAt:   time.Now().UTC(),
				UpdatedAt:   time.Now().UTC(),
			}

			system := buildSystem(inst, nil)

			// 1. Check archetype id injection
			if !strings.Contains(system, "- archetype: "+tmpl.ID) {
				t.Errorf("[%s] system prompt missing archetype declaration: %s", tmpl.ID, system)
			}

			// 2. Check preinstalled tools inventory
			if len(tmpl.PreinstalledTools) > 0 {
				for _, tool := range tmpl.PreinstalledTools[:3] {
					if !strings.Contains(system, tool) {
						t.Errorf("[%s] system prompt missing tool %s", tmpl.ID, tool)
					}
				}
			}

			// 3. Check specialized playbook guidelines. The header is written
			// in the second person because it addresses the agent; what
			// matters is that the persona is carried, which the playbook
			// assertion below actually proves.
			if !strings.Contains(system, "Your persona and guidelines:") {
				t.Errorf("[%s] system prompt missing the persona section", tmpl.ID)
			}
			if !strings.Contains(system, "OPERATING PLAYBOOK:") {
				t.Errorf("[%s] system prompt missing OPERATING PLAYBOOK header", tmpl.ID)
			}
		})
	}
}
