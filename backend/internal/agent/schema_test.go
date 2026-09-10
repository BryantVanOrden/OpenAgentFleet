package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActionSchemaDescribesTheActionObject(t *testing.T) {
	raw, err := json.Marshal(ActionSchema())
	if err != nil {
		t.Fatalf("schema does not marshal: %v", err)
	}
	var s struct {
		Type                 string                    `json:"type"`
		Properties           map[string]map[string]any `json:"properties"`
		Required             []string                  `json:"required"`
		AdditionalProperties bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if s.Type != "object" || s.AdditionalProperties {
		t.Fatalf("schema should be a closed object: %s", raw)
	}
	if len(s.Required) != 2 || s.Required[0] != "thought" || s.Required[1] != "action" {
		t.Fatalf("thought and action should be required, in that order: %v", s.Required)
	}
	enum := s.Properties["action"]["enum"].([]any)
	seen := map[string]bool{}
	for _, v := range enum {
		seen[v.(string)] = true
	}
	for _, want := range []string{"shell", "python", "click", "type", "done", "message_peer"} {
		if !seen[want] {
			t.Errorf("action enum is missing %q", want)
		}
	}
	if s.Properties["coordinates"]["type"] != "array" {
		t.Errorf("coordinates should be an array: %v", s.Properties["coordinates"])
	}
	if s.Properties["mark"]["type"] != "integer" || s.Properties["timeout"]["type"] != "integer" {
		t.Errorf("mark and timeout should be integers")
	}
	if s.Properties["text"]["type"] != "string" {
		t.Errorf("text should be a string: %v", s.Properties["text"])
	}
}

// The grammar emits properties in schema order, so the order on the wire is
// the order the model thinks in: thought, then the verb, then its arguments.
func TestActionSchemaPutsThoughtBeforeAction(t *testing.T) {
	raw, _ := json.Marshal(ActionSchema())
	props := string(raw)[strings.Index(string(raw), `"properties":`):]
	thought, action, text := strings.Index(props, `"thought"`), strings.Index(props, `"action"`), strings.Index(props, `"text"`)
	if !(thought >= 0 && thought < action && action < text) {
		t.Fatalf("expected thought < action < text in the wire order, got %d %d %d", thought, action, text)
	}
}
