package protocol

import "testing"

func TestDefaultBotTemplates(t *testing.T) {
	templates := DefaultBotTemplates()
	if len(templates) != 8 {
		t.Fatalf("expected 8 bot templates, got %d", len(templates))
	}

	expectedIDs := []string{
		"cyber_ops", "fullstack_dev", "qa_ui_ux", "game_dev",
		"growth_media", "media_studio", "agentic_crm", "data_quant",
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
