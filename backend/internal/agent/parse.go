package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

var validActions = map[protocol.ActionKind]bool{
	protocol.ActClick: true, protocol.ActDoubleClick: true, protocol.ActRightClick: true,
	protocol.ActType: true, protocol.ActKey: true, protocol.ActScroll: true,
	protocol.ActDrag: true, protocol.ActWait: true, protocol.ActWaitFor: true,
	protocol.ActFocus: true, protocol.ActShell: true, protocol.ActPython: true,
	protocol.ActSpawnAgent: true, protocol.ActMountTool: true,
	protocol.ActUnmountTool: true, protocol.ActCallTool: true,
	protocol.ActDeepSearch: true, protocol.ActAssert: true,
	protocol.ActAskHuman: true, protocol.ActDone: true, protocol.ActFail: true,
}

// ParseAction pulls a single action object out of a model reply. Models wrap
// JSON in fences, prepend commentary, or emit several objects; all three are
// recovered here rather than burning a retry.
func ParseAction(raw string) (protocol.Action, error) {
	var a protocol.Action
	body := extractJSON(raw)
	if body == "" {
		return a, fmt.Errorf("no JSON object in model reply: %s", clip(raw, 200))
	}
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return a, fmt.Errorf("malformed action JSON: %w (%s)", err, clip(body, 200))
	}

	a.Action = protocol.ActionKind(strings.ToLower(strings.TrimSpace(string(a.Action))))
	if !validActions[a.Action] {
		return a, fmt.Errorf("unknown action %q", a.Action)
	}
	if len(a.Coordinates) == 1 || len(a.Coordinates) > 2 {
		return a, fmt.Errorf("coordinates must be [x,y], got %v", a.Coordinates)
	}

	switch a.Action {
	case protocol.ActClick, protocol.ActDoubleClick, protocol.ActRightClick:
		if a.Mark <= 0 && len(a.Coordinates) != 2 && strings.TrimSpace(a.Target) == "" {
			return a, fmt.Errorf("%s needs mark, coordinates or a target label", a.Action)
		}
	case protocol.ActType:
		if a.Text == "" {
			return a, fmt.Errorf("type needs text")
		}
	case protocol.ActKey:
		if a.Key == "" {
			return a, fmt.Errorf("key needs a key combination")
		}
	case protocol.ActDrag:
		if len(a.Coordinates) != 2 || len(a.To) != 2 {
			return a, fmt.Errorf("drag needs coordinates and to")
		}
	case protocol.ActWaitFor:
		if a.Text == "" {
			return a, fmt.Errorf("wait_for needs the text to wait for")
		}
		if a.Timeout <= 0 {
			a.Timeout = 120
		}
	case protocol.ActShell, protocol.ActAssert:
		if a.Text == "" {
			return a, fmt.Errorf("%s needs a command in text", a.Action)
		}
	case protocol.ActPython:
		if a.Code == "" && a.Text != "" {
			a.Code = a.Text
		}
		if strings.TrimSpace(a.Code) == "" {
			return a, fmt.Errorf("python needs python code in code or text")
		}
	case protocol.ActSpawnAgent:
		if a.SubGoal == "" && a.Text != "" {
			a.SubGoal = a.Text
		}
		if strings.TrimSpace(a.SubGoal) == "" {
			return a, fmt.Errorf("spawn_agent needs a sub-goal in sub_goal or text")
		}
	case protocol.ActMountTool:
		if a.ToolName == "" && a.Target != "" {
			a.ToolName = a.Target
		}
		if strings.TrimSpace(a.ToolName) == "" {
			return a, fmt.Errorf("mount_tool needs tool_name")
		}
		if a.ToolHandler == "" && a.Code != "" {
			a.ToolHandler = a.Code
		} else if a.ToolHandler == "" && a.Text != "" {
			a.ToolHandler = a.Text
		}
		if strings.TrimSpace(a.ToolHandler) == "" {
			return a, fmt.Errorf("mount_tool needs tool_handler code")
		}
	case protocol.ActUnmountTool:
		if a.ToolName == "" && a.Target != "" {
			a.ToolName = a.Target
		} else if a.ToolName == "" && a.Text != "" {
			a.ToolName = a.Text
		}
		if strings.TrimSpace(a.ToolName) == "" {
			return a, fmt.Errorf("unmount_tool needs tool_name")
		}
	case protocol.ActCallTool:
		if a.ToolName == "" && a.Target != "" {
			a.ToolName = a.Target
		}
		if strings.TrimSpace(a.ToolName) == "" {
			return a, fmt.Errorf("call_tool needs tool_name")
		}
	case protocol.ActDeepSearch:
		if a.Query == "" && a.Text != "" {
			a.Query = a.Text
		} else if a.Query == "" && a.SubGoal != "" {
			a.Query = a.SubGoal
		}
		if strings.TrimSpace(a.Query) == "" {
			return a, fmt.Errorf("deep_search needs query in query or text")
		}
	case protocol.ActAskHuman:
		if a.Question == "" {
			a.Question = firstNonEmpty(a.Summary, a.Thought, "I need help to continue.")
		}
	case protocol.ActWait:
		if a.Amount <= 0 {
			a.Amount = 3
		}
		if a.Amount > 300 {
			a.Amount = 300
		}
	case protocol.ActScroll:
		if a.Amount == 0 {
			a.Amount = 3
		}
	}
	return a, nil
}

// extractJSON returns the first balanced top-level object in s, ignoring braces
// inside string literals.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:]
		}
		if k := strings.Index(rest, "```"); k >= 0 {
			rest = rest[:k]
		}
		s = strings.TrimSpace(rest)
	}
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth, inStr, escaped := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
