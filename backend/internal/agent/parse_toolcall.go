package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Tool-call dialects.
//
// Models trained for function calling do not always answer in the JSON object
// the prompt asks for. Under pressure -- a long shell heredoc to emit, a
// system prompt that was cut short -- Qwen falls back to the XML it was
// fine-tuned on:
//
//	<tool_call>
//	<function=shell>
//	<parameter=command>
//	mkdir -p /home/agent/app && cat > /home/agent/app/index.html <<'EOF'
//	...
//	</parameter>
//	</function>
//	</tool_call>
//
// and Hermes-style models wrap a {"name": ..., "arguments": {...}} object in
// the same tags. Both say exactly what the model wants done. Refusing them
// cost a real run: Builder, seven steps into a build, was failed with "model
// would not produce a valid action" three replies in a row, each one a
// perfectly good shell command in the wrong clothes. So both dialects are
// translated into the action object here, once, rather than every model
// being asked to relearn our syntax under load.

var (
	reFunctionOpen = regexp.MustCompile(`<function\s*(?:=|name=)\s*"?([A-Za-z0-9_.:-]+)"?\s*>`)
	reParameter    = regexp.MustCompile(`(?s)<parameter\s*(?:=|name=)\s*"?([A-Za-z0-9_.-]+)"?\s*>(.*?)</parameter>`)
)

// argumentAliases maps the parameter names function-calling models reach for
// onto the fields of protocol.Action. Unknown names pass through unchanged,
// which lets a model that already knows our field names use them.
var argumentAliases = map[string]string{
	"command":  "text",
	"cmd":      "text",
	"script":   "text",
	"message":  "text",
	"content":  "text",
	"input":    "text",
	"value":    "text",
	"keys":     "key",
	"shortcut": "key",
	"label":    "target",
	"element":  "target",
	"window":   "target",
	"selector": "target",
	"source":   "code",
	"python":   "code",
	"reason":   "thought",
	"seconds":  "seconds",
	"duration": "seconds",
	"goal":     "sub_goal",
	"peer":     "peer_id",
	"to":       "peer_id",
	"name":     "tool_name",
}

// toolCallToJSON translates a tool-call reply into action JSON. ok is false
// when the reply carries no tool call at all, so the caller can fall back to
// the normal JSON search.
func toolCallToJSON(raw string) (string, bool) {
	if m := reFunctionOpen.FindStringSubmatch(raw); m != nil {
		obj := map[string]any{"action": strings.ToLower(m[1])}
		rest := raw[strings.Index(raw, m[0])+len(m[0]):]
		if end := strings.Index(rest, "</function>"); end >= 0 {
			rest = rest[:end]
		}
		for _, p := range reParameter.FindAllStringSubmatch(rest, -1) {
			key, val := strings.ToLower(p[1]), trimOneNewline(p[2])
			if alias, ok := argumentAliases[key]; ok {
				key = alias
			}
			setArgument(obj, key, val)
		}
		b, err := json.Marshal(obj)
		return string(b), err == nil
	}

	// Hermes / Qwen-JSON: <tool_call>{"name": "shell", "arguments": {...}}</tool_call>,
	// or that object bare. The JSON search finds the object; normalisation
	// turns name/arguments into an action.
	if body := extractJSON(raw); body != "" {
		if out, ok := normalizeToolCallObject(body); ok {
			return out, true
		}
	}
	return "", false
}

// normalizeToolCallObject rewrites a {"name": ..., "arguments": {...}} object
// (also "function"/"parameters"/"input" spellings) into action JSON. ok is
// false when the object is not that shape, e.g. when it already is an action.
func normalizeToolCallObject(body string) (string, bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return "", false
	}
	if _, isAction := m["action"]; isAction {
		return "", false
	}
	name := ""
	for _, k := range []string{"name", "function", "tool"} {
		if s, ok := m[k].(string); ok && s != "" {
			name = s
			break
		}
	}
	if name == "" {
		// OpenAI shape: {"function": {"name": ..., "arguments": ...}}
		if fn, ok := m["function"].(map[string]any); ok {
			if s, ok := fn["name"].(string); ok {
				name = s
				m = fn
			}
		}
	}
	if name == "" {
		return "", false
	}
	obj := map[string]any{"action": strings.ToLower(name)}
	var args map[string]any
	for _, k := range []string{"arguments", "parameters", "params", "input", "args"} {
		switch v := m[k].(type) {
		case map[string]any:
			args = v
		case string:
			// Arguments arrive as a JSON string in the OpenAI shape.
			_ = json.Unmarshal([]byte(v), &args)
		}
		if args != nil {
			break
		}
	}
	for k, v := range args {
		key := strings.ToLower(k)
		if alias, ok := argumentAliases[key]; ok {
			key = alias
		}
		if s, ok := v.(string); ok {
			setArgument(obj, key, s)
		} else {
			obj[key] = v
		}
	}
	if t, ok := m["thought"].(string); ok && obj["thought"] == nil {
		obj["thought"] = t
	}
	b, err := json.Marshal(obj)
	return string(b), err == nil
}

// setArgument stores a string argument, coercing the few numeric fields.
func setArgument(obj map[string]any, key, val string) {
	switch key {
	case "mark", "seconds", "amount":
		var n float64
		if err := json.Unmarshal([]byte(strings.TrimSpace(val)), &n); err == nil {
			obj[key] = n
			return
		}
	case "coordinates", "to":
		var xy []int
		if err := json.Unmarshal([]byte(strings.TrimSpace(val)), &xy); err == nil {
			obj[key] = xy
			return
		}
	}
	obj[key] = val
}

// trimOneNewline drops the single newline models put after an opening tag and
// before the closing one, leaving the value's own whitespace alone: a heredoc
// that ends in a blank line has to keep it.
func trimOneNewline(s string) string {
	s = strings.TrimPrefix(s, "\r\n")
	s = strings.TrimPrefix(s, "\n")
	s = strings.TrimSuffix(s, "\r\n")
	s = strings.TrimSuffix(s, "\n")
	return s
}
