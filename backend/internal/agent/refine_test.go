package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/recorder"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func TestRefineOutputParsing(t *testing.T) {
	raw := `
Here is the refined skill:
` + "```json" + `
{
  "name": "Refined Build & Test",
  "description": "Optimized procedure with healed accessible labels",
  "params": ["repo", "branch"],
  "steps": [
    {
      "index": 1,
      "kind": "focus",
      "window": "Terminal"
    },
    {
      "index": 2,
      "kind": "click",
      "role": "push button",
      "label": "Build Project"
    },
    {
      "index": 3,
      "kind": "wait_for",
      "text": "Build Finished"
    }
  ],
  "refinement_notes": "Replaced fragile coordinate click on step 2 with accessible label 'Build Project'."
}
` + "```"

	body := extractJSON(raw)
	if body == "" {
		t.Fatalf("extractJSON returned empty")
	}

	var out refineOutput
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if out.Name != "Refined Build & Test" {
		t.Errorf("Name = %q, want 'Refined Build & Test'", out.Name)
	}
	if len(out.Steps) != 3 {
		t.Fatalf("Steps count = %d, want 3", len(out.Steps))
	}
	if out.Steps[1].Label != "Build Project" {
		t.Errorf("Step 2 label = %q, want 'Build Project'", out.Steps[1].Label)
	}
	if !strings.Contains(out.RefinementNotes, "Replaced fragile coordinate") {
		t.Errorf("RefinementNotes = %q", out.RefinementNotes)
	}

	// Verify rendered markdown
	sk := &protocol.Skill{
		Name:            out.Name,
		Description:     out.Description,
		Params:          out.Params,
		Steps:           out.Steps,
		Version:         2,
		RefinementNotes: out.RefinementNotes,
	}
	md := recorder.Render(sk)
	if !strings.Contains(md, "Build Project") {
		t.Errorf("Rendered markdown missing step label: %s", md)
	}
}
