package agent

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Lenient decoding of an action object.
//
// Watched live on 2026-09-09, qwen3.8-flash-next answered with the right
// action in the wrong shape several different ways inside one run:
//
//	{"action": "shell", "text": "ls", "command": "ls", "target": "", "coordinates": "",
//	 "mark": "", "mcp_params": "", ...}          every schema field, most of them ""
//	<function=Shell><parameter=action>shell</parameter><parameter=timeout>10</parameter>
//	<parameter=text>cat app.js</parameter>       the schema as XML parameters, numbers quoted
//	{"action": {"name": "read_file", "arguments": {"path": "app.js"}}}   action as an object
//	[{"name": "bash", "arguments": {"command": "cat app.js"}}]            a tool-call array
//
// Each of these failed strict decoding and, three in a row, failed the run.
// So the object is cleaned before it is decoded: empty values go, numbers and
// lists and maps in strings are parsed according to the real field types, a
// nested action object is flattened, and the names function-calling models
// use for a shell are read as `shell`. Providers that can hold the model to
// a schema never produce any of this; see schema.go.

// actionFieldKinds maps each JSON field of protocol.Action to how a string
// value should be coerced: "int", "bool", "ints", "map", or "" for strings.
// Built from the struct itself so a new field is covered the day it is added
// -- Timeout was missed by a hand-written list and "10" in a string failed
// a run.
var actionFieldKinds = func() map[string]string {
	kinds := map[string]string{}
	t := reflect.TypeOf(protocol.Action{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Float32, reflect.Float64:
			kinds[name] = "int"
		case reflect.Bool:
			kinds[name] = "bool"
		case reflect.Slice:
			kinds[name] = "ints"
		case reflect.Map:
			kinds[name] = "map"
		}
	}
	return kinds
}()

// verbAliases are the names function-calling models give our actions.
var verbAliases = map[string]string{
	"bash": "shell", "sh": "shell", "terminal": "shell", "run_command": "shell",
	"execute": "shell", "exec": "shell", "command": "shell", "run_shell": "shell",
	"run_python": "python", "python3": "python", "code_interpreter": "python",
	"press": "key", "keypress": "key", "hotkey": "key",
	"type_text": "type", "input_text": "type",
	"finish": "done", "complete": "done", "final_answer": "done",
}

// lenientActionJSON cleans an action object so strict decoding can accept it.
// Objects that are not JSON objects at all are returned unchanged for the
// decoder to complain about.
func lenientActionJSON(body string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return body
	}
	cleaned := cleanActionMap(m)
	out, err := json.Marshal(cleaned)
	if err != nil {
		return body
	}
	return string(out)
}

func cleanActionMap(m map[string]any) map[string]any {
	// A nested action -- {"action": {"name": "shell", "arguments": {...}}} --
	// is flattened first, so its arguments are cleaned like everything else.
	if inner, ok := m["action"].(map[string]any); ok {
		flat := map[string]any{}
		for k, v := range m {
			if k != "action" {
				flat[k] = v
			}
		}
		if name, ok := firstString(inner, "name", "function", "tool", "action"); ok {
			flat["action"] = name
		}
		for _, k := range []string{"arguments", "parameters", "params", "input", "args"} {
			if args, ok := inner[k].(map[string]any); ok {
				for ak, av := range args {
					flat[ak] = av
				}
			}
		}
		for k, v := range inner {
			switch k {
			case "name", "function", "tool", "action", "arguments", "parameters", "params", "input", "args":
			default:
				flat[k] = v
			}
		}
		m = flat
	}

	out := make(map[string]any, len(m))
	for k, v := range m {
		key := strings.ToLower(strings.TrimSpace(k))
		switch val := v.(type) {
		case nil:
			continue
		case string:
			s := strings.TrimSpace(val)
			if s == "" {
				continue
			}
			switch actionFieldKinds[key] {
			case "int":
				if n, err := strconv.ParseFloat(s, 64); err == nil {
					out[key] = n
				}
				continue
			case "ints":
				if xs, ok := parseInts(s); ok {
					out[key] = xs
				}
				continue
			case "bool":
				if b, err := strconv.ParseBool(s); err == nil {
					out[key] = b
				}
				continue
			case "map":
				var mm map[string]any
				if json.Unmarshal([]byte(s), &mm) == nil {
					out[key] = mm
				}
				continue
			}
			out[key] = val
		case []any:
			if len(val) == 0 {
				continue
			}
			out[key] = val
		case map[string]any:
			if len(val) == 0 {
				continue
			}
			out[key] = val
		default:
			out[key] = val
		}
	}

	if action, ok := out["action"].(string); ok {
		lower := strings.ToLower(strings.TrimSpace(action))
		if alias, ok := verbAliases[lower]; ok {
			lower = alias
		}
		out["action"] = lower
	}
	// "command" is what function-calling models call a shell's text; "path"
	// with a shell-less read is a cat.
	if _, has := out["text"]; !has {
		for _, alt := range []string{"command", "cmd"} {
			if s, ok := out[alt].(string); ok {
				out["text"] = s
				break
			}
		}
	}
	// A shell command in `code`, or Python in `text`: the field names are
	// ours, the intent is plain. Seen live: {"action":"shell","code":"cd … && ls"}.
	action, _ := out["action"].(string)
	text, hasText := out["text"].(string)
	code, hasCode := out["code"].(string)
	switch action {
	case "shell":
		if !hasText && hasCode {
			out["text"] = code
			delete(out, "code")
		}
	case "python":
		if !hasCode && hasText {
			out["code"] = text
			delete(out, "text")
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s, true
		}
	}
	return "", false
}

// parseInts reads "[3, 4]", "3,4" or "3 4" as a list of ints.
func parseInts(s string) ([]int, bool) {
	var xs []int
	if json.Unmarshal([]byte(s), &xs) == nil {
		return xs, len(xs) > 0
	}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '[' || r == ']' }) {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, false
		}
		xs = append(xs, n)
	}
	return xs, len(xs) > 0
}
