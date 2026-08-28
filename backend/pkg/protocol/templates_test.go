package protocol

import "testing"

func TestDefaultBotTemplates(t *testing.T) {
	templates := DefaultBotTemplates()

	// The list below is the set callers and docs rely on existing. Asserting
	// presence rather than an exact length is deliberate: an exact count breaks
	// every time an archetype is added — which is a change, not a defect — while
	// still failing loudly if one is removed or renamed.
	expectedIDs := []string{
		"fleet_manager",
		"cyber_ops", "fullstack_dev", "devops_sre", "qa_ui_ux", "game_dev",
		"growth_media", "media_studio", "agentic_crm", "data_quant", "deep_researcher",
	}

	if len(templates) < len(expectedIDs) {
		t.Fatalf("got %d bot templates, want at least the %d documented ones",
			len(templates), len(expectedIDs))
	}

	for _, expectedID := range expectedIDs {
		tmpl := BotTemplateByID(expectedID)
		if tmpl == nil {
			t.Errorf("BotTemplateByID(%q) returned nil", expectedID)
			continue
		}
		if tmpl.Name == "" {
			t.Errorf("template %s has empty Name", expectedID)
		}
		if tmpl.SpecializedPrompt == "" {
			t.Errorf("template %s has empty SpecializedPrompt", expectedID)
		}
		if len(tmpl.PreinstalledTools) == 0 {
			t.Errorf("template %s has no preinstalled tools", expectedID)
		}
		if tmpl.VCPU <= 0 || tmpl.MemoryMB <= 0 || tmpl.DiskGB <= 0 {
			t.Errorf("template %s has invalid resource specifications: vcpu=%f, mem=%d, disk=%d",
				expectedID, tmpl.VCPU, tmpl.MemoryMB, tmpl.DiskGB)
		}
	}
}

func TestBotTemplateByIDNotFound(t *testing.T) {
	if tmpl := BotTemplateByID("non_existent_archetype"); tmpl != nil {
		t.Errorf("expected nil for non-existent archetype, got %v", tmpl)
	}
}
