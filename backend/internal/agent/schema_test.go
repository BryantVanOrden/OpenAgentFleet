package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

type schemaBranch struct {
	Type                 string                    `json:"type"`
	Properties           map[string]map[string]any `json:"properties"`
	Required             []string                  `json:"required"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

func schemaBranches(t *testing.T) (raw []byte, branches []schemaBranch) {
	t.Helper()
	raw, err := json.Marshal(ActionSchema())
	if err != nil {
		t.Fatalf("schema does not marshal: %v", err)
	}
	var s struct {
		AnyOf []schemaBranch `json:"anyOf"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if len(s.AnyOf) < 2 {
		t.Fatalf("schema should be one branch per action with required fields plus one for the rest: %s", raw)
	}
	return raw, s.AnyOf
}

func TestActionSchemaDescribesTheActionObject(t *testing.T) {
	_, branches := schemaBranches(t)
	seen := map[string]schemaBranch{}
	for _, b := range branches {
		if b.Type != "object" || b.AdditionalProperties {
			t.Fatalf("every branch is a closed object: %+v", b)
		}
		if len(b.Required) < 2 || b.Required[0] != "thought" || b.Required[1] != "action" {
			t.Fatalf("thought and action come first in every branch: %v", b.Required)
		}
		for _, v := range b.Properties["action"]["enum"].([]any) {
			if _, dup := seen[v.(string)]; dup {
				t.Fatalf("%s is in two branches", v)
			}
			seen[v.(string)] = b
		}
	}
	for _, want := range []string{"shell", "python", "click", "type", "done", "message_peer", "create_ticket", "reopen_ticket"} {
		if _, ok := seen[want]; !ok {
			t.Errorf("no branch accepts %q", want)
		}
	}
	for verb, fields := range map[string][]string{
		"create_ticket": {"target", "title", "text"},
		"reopen_ticket": {"ticket", "text"},
		"done":          {"summary"},
		"shell":         {"text"},
	} {
		b := seen[verb]
		got := strings.Join(b.Required[2:], ",")
		if got != strings.Join(fields, ",") {
			t.Errorf("%s requires %s, want %s", verb, got, strings.Join(fields, ","))
		}
		for _, f := range fields {
			if b.Properties[f]["minLength"] != float64(1) {
				t.Errorf("%s.%s must be non-empty: %v", verb, f, b.Properties[f])
			}
		}
	}
	click := seen["click"]
	if len(click.Required) != 2 {
		t.Errorf("click needs nothing beyond thought and action (mark or coordinates both work): %v", click.Required)
	}
	if click.Properties["coordinates"]["type"] != "array" {
		t.Errorf("coordinates should be an array: %v", click.Properties["coordinates"])
	}
	if click.Properties["mark"]["type"] != "integer" || click.Properties["timeout"]["type"] != "integer" {
		t.Errorf("mark and timeout should be integers")
	}
	if click.Properties["text"]["type"] != "string" {
		t.Errorf("text should be a string: %v", click.Properties["text"])
	}
}

// The grammar emits properties in schema order, so the order on the wire is
// the order the model thinks in: thought, then the verb, then its arguments.
func TestActionSchemaPutsThoughtBeforeAction(t *testing.T) {
	raw, _ := schemaBranches(t)
	s := string(raw)
	i := strings.Index(s, `"create_ticket"`)
	branch := s[strings.LastIndex(s[:i], `"properties":`):]
	thought, action, target, text := strings.Index(branch, `"thought"`), strings.Index(branch, `"action"`), strings.Index(branch, `"target"`), strings.Index(branch, `"text"`)
	if !(thought >= 0 && thought < action && action < target && target < text) {
		t.Fatalf("expected thought < action < target < text in create_ticket's wire order, got %d %d %d %d", thought, action, target, text)
	}
}
