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
		props = append(props, struct {
			Name   string
			Schema map[string]any
		}{name, schemaFor(f.Type)})
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             []string{"thought", "action"},
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
