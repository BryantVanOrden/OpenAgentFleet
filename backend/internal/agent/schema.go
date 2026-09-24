package agent

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The action object as a JSON schema, for providers that can hold a model to
// one.
//
// Asking for "one JSON object" in the prompt and `response_format:
// json_object` on the wire was not enough: the LAN gateway honoured
// json_object on short prompts and ignored it past a few thousand tokens,
// which is every agent turn, and the model wandered through XML tool calls,
// prose, nested {"name","arguments"} objects and every field present but
// empty. It did honour a json_schema at the same length. So the loop sends
// the schema: the grammar makes the wrong shapes impossible, and the lenient
// decoder stays for providers that cannot enforce one.
//
// Field order matters. A grammar emits required properties in the schema's
// order, and Go marshals a map alphabetically, which put `action` first and
// `thought` near the end -- so the model chose the action before it had
// thought, and left the thought empty on most turns. Those turns were the
// pointless ones: a file read for the fifth time. With `thought` first and
// required, the model writes its reasoning before it commits to a verb, the
// way the prompt always asked it to.

var actionSchema map[string]any

func init() {
	actionSchema = buildActionSchema()
}

// ActionSchema is the JSON schema of protocol.Action: `thought` first, then
// `action` limited to the verbs the loop accepts, then every other field by
// its JSON name and type, nothing else allowed. Callers must not modify it.
//
// It is one branch per action that has fields it cannot do without, each
// naming those fields as required and non-empty right after the verb, and a
// last branch for every other action. With only thought and action
// required, a small model chose create_ticket and never wrote the
// instructions, six turns in a row; the parser could only refuse it, and the
// ticket blocked. Under the grammar the field is written or the verb is not.
func ActionSchema() map[string]any {
	return actionSchema
}

// orderedProps marshals as a JSON object in slice order.
type orderedProps []struct {
	Name   string
	Schema map[string]any
}

func (p orderedProps) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range p {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.Name)
		v, err := json.Marshal(kv.Schema)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// requiredFields are the fields an action cannot be carried out without, in
// the order the model should write them. The parser's own recoveries (a
// command read from text, instructions from the title) still apply to
// providers that cannot enforce a schema.
var requiredFields = map[protocol.ActionKind][]string{
	protocol.ActType:         {"text"},
	protocol.ActShell:        {"text"},
	protocol.ActAssert:       {"text"},
	protocol.ActWaitFor:      {"text"},
	protocol.ActRemember:     {"text"},
	protocol.ActSpeak:        {"text"},
	protocol.ActMsgPeer:      {"text"},
	protocol.ActKey:          {"key"},
	protocol.ActOpenURL:      {"url"},
	protocol.ActPython:       {"code"},
	protocol.ActDelegateTask: {"target", "sub_goal"},
	protocol.ActSpawnAgent:   {"sub_goal"},
	protocol.ActMountTool:    {"tool_name", "tool_handler"},
	protocol.ActUnmountTool:  {"tool_name"},
	protocol.ActCallTool:     {"tool_name"},
	protocol.ActDeepSearch:   {"query"},
	protocol.ActRecall:       {"query"},
	protocol.ActShareSecret:  {"secret_key", "secret_val"},
	protocol.ActShareSession: {"session_domain", "session_cookies"},
	protocol.ActCreateTicket: {"target", "title", "text"},
	protocol.ActReopenTicket: {"ticket", "text"},
	protocol.ActPublishWork:  {"work_name"},
	protocol.ActReadWork:     {"work_name"},
	protocol.ActDone:         {"summary"},
	protocol.ActFail:         {"summary"},
}

func buildActionSchema() map[string]any {
	verbs := make([]string, 0, len(validActions))
	for k := range validActions {
		verbs = append(verbs, string(k))
	}
	sort.Strings(verbs)

	props := orderedProps{
		{"thought", map[string]any{
			"type":        "string",
			"minLength":   1,
			"description": "What you see, what it means for the goal, and why this action is the next step. Written before the action.",
		}},
		{"action", map[string]any{"type": "string", "enum": verbs}},
		// `text` third: the command for shell, the words for type, the message
		// for message_peer. It is decided straight after the verb, while the
		// thought is fresh. Twenty fields further down, the model twice chose
		// "shell", filled in key, timeout and summary, and never the command.
		{"text", map[string]any{
			"type":        "string",
			"description": "The command for shell, the text to type, the message for message_peer, the memory for remember. Required by those actions.",
		}},
	}
	t := reflect.TypeOf(protocol.Action{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "thought" || name == "action" || name == "text" {
			continue
		}
		sch := schemaFor(f.Type)
		if name == "verdict" {
			sch = map[string]any{"type": "string", "enum": []string{"pass", "fail"},
				"description": "Only when finishing a review or verify ticket with done."}
		}
		props = append(props, struct {
			Name   string
			Schema map[string]any
		}{name, sch})
	}
	byName := map[string]map[string]any{}
	for _, p := range props {
		byName[p.Name] = p.Schema
	}

	var branches []any
	var rest []string
	for _, v := range verbs {
		need := requiredFields[protocol.ActionKind(v)]
		if len(need) == 0 {
			rest = append(rest, v)
			continue
		}
		branches = append(branches, branch(props, byName, map[string]any{"type": "string", "enum": []string{v}}, need))
	}
	if len(rest) > 0 {
		branches = append(branches, branch(props, byName, map[string]any{"type": "string", "enum": rest}, nil))
	}
	return map[string]any{"anyOf": branches}
}

// branch is the object schema for one set of verbs: thought, the verb, the
// fields it needs (required, non-empty), then everything else, optional.
func branch(all orderedProps, byName map[string]map[string]any, verb map[string]any, need []string) map[string]any {
	props := orderedProps{{"thought", byName["thought"]}, {"action", verb}}
	required := []string{"thought", "action"}
	seen := map[string]bool{"thought": true, "action": true}
	for _, n := range need {
		sch := map[string]any{}
		for k, v := range byName[n] {
			sch[k] = v
		}
		if sch["type"] == "string" {
			sch["minLength"] = 1
		}
		props = append(props, struct {
			Name   string
			Schema map[string]any
		}{n, sch})
		required = append(required, n)
		seen[n] = true
	}
	for _, p := range all {
		if !seen[p.Name] {
			props = append(props, p)
		}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func schemaFor(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaFor(t.Elem())}
	case reflect.Map, reflect.Struct:
		return map[string]any{"type": "object"}
	case reflect.Pointer:
		return schemaFor(t.Elem())
	}
	return map[string]any{}
}
