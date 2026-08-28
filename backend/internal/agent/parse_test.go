package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ---------------------------------------------------------------- extraction ---

func TestParseActionExtraction(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want protocol.Action
	}{
		{
			name: "plain object",
			raw:  `{"thought":"the button is there","action":"click","coordinates":[100,200]}`,
			want: protocol.Action{Thought: "the button is there", Action: protocol.ActClick, Coordinates: []int{100, 200}},
		},
		{
			name: "json fence",
			raw:  "```json\n{\"action\":\"click\",\"coordinates\":[10,20]}\n```",
			want: protocol.Action{Action: protocol.ActClick, Coordinates: []int{10, 20}},
		},
		{
			name: "bare fence without language tag",
			raw:  "```\n{\"action\":\"key\",\"key\":\"ctrl+s\"}\n```",
			want: protocol.Action{Action: protocol.ActKey, Key: "ctrl+s"},
		},
		{
			name: "leading prose commentary",
			raw:  "Sure! I'll click Save now.\n{\"action\":\"click\",\"target\":\"Save\"}",
			want: protocol.Action{Action: protocol.ActClick, Target: "Save"},
		},
		{
			name: "prose then fenced json",
			raw:  "Here is my next step:\n```json\n{\"action\":\"focus\",\"target\":\"Firefox\"}\n```",
			want: protocol.Action{Action: protocol.ActFocus, Target: "Firefox"},
		},
		{
			name: "two objects in a row takes the first",
			raw:  `{"action":"click","target":"First"} {"action":"click","target":"Second"}`,
			want: protocol.Action{Action: protocol.ActClick, Target: "First"},
		},
		{
			name: "braces inside a string literal do not break balance",
			raw:  `{"action":"type","text":"say {hi}"}`,
			want: protocol.Action{Action: protocol.ActType, Text: "say {hi}"},
		},
		{
			name: "unbalanced brace inside a string literal",
			raw:  `{"action":"type","text":"a } is not the end"}`,
			want: protocol.Action{Action: protocol.ActType, Text: "a } is not the end"},
		},
		{
			name: "escaped quotes inside a string",
			raw:  `{"action":"type","text":"he said \"hi\" loudly"}`,
			want: protocol.Action{Action: protocol.ActType, Text: `he said "hi" loudly`},
		},
		{
			name: "escaped backslash before the closing quote",
			raw:  `{"action":"type","text":"C:\\path\\"}`,
			want: protocol.Action{Action: protocol.ActType, Text: `C:\path\`},
		},
		{
			name: "nested object is kept whole",
			raw:  `{"action":"click","target":"Save","coordinates":[1,2],"meta":{"a":{"b":1}}}`,
			want: protocol.Action{Action: protocol.ActClick, Target: "Save", Coordinates: []int{1, 2}},
		},
		{
			name: "action kind is case and space insensitive",
			raw:  `{"action":" Double_Click ","coordinates":[5,6]}`,
			want: protocol.Action{Action: protocol.ActDoubleClick, Coordinates: []int{5, 6}},
		},
		{
			name: "python action with code",
			raw:  `{"action":"python","code":"print('hello world')"}`,
			want: protocol.Action{Action: protocol.ActPython, Code: "print('hello world')"},
		},
		{
			name: "spawn_agent action with sub_goal",
			raw:  `{"action":"spawn_agent","sub_goal":"compile the assets"}`,
			want: protocol.Action{Action: protocol.ActSpawnAgent, SubGoal: "compile the assets"},
		},
		{
			name: "mount_tool action with tool_name and tool_handler",
			raw:  `{"action":"mount_tool","tool_name":"parse_logs","tool_handler":"def parse_logs(): pass"}`,
			want: protocol.Action{Action: protocol.ActMountTool, ToolName: "parse_logs", ToolHandler: "def parse_logs(): pass"},
		},
		{
			name: "unmount_tool action with tool_name",
			raw:  `{"action":"unmount_tool","tool_name":"parse_logs"}`,
			want: protocol.Action{Action: protocol.ActUnmountTool, ToolName: "parse_logs"},
		},
		{
			name: "call_tool action with tool_name",
			raw:  `{"action":"call_tool","tool_name":"parse_logs"}`,
			want: protocol.Action{Action: protocol.ActCallTool, ToolName: "parse_logs"},
		},
		{
			name: "click action with Set-of-Marks mark ID",
			raw:  `{"action":"click","mark":5}`,
			want: protocol.Action{Action: protocol.ActClick, Mark: 5},
		},
		{
			name: "deep_search action with query",
			raw:  `{"action":"deep_search","query":"latest golang release notes"}`,
			want: protocol.Action{Action: protocol.ActDeepSearch, Query: "latest golang release notes"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAction(tc.raw)
			if err != nil {
				t.Fatalf("ParseAction() error = %v", err)
			}
			if got.Action != tc.want.Action {
				t.Errorf("Action = %q, want %q", got.Action, tc.want.Action)
			}
			if got.Target != tc.want.Target {
				t.Errorf("Target = %q, want %q", got.Target, tc.want.Target)
			}
			if got.Text != tc.want.Text {
				t.Errorf("Text = %q, want %q", got.Text, tc.want.Text)
			}
			if got.Key != tc.want.Key {
				t.Errorf("Key = %q, want %q", got.Key, tc.want.Key)
			}
			if tc.want.Thought != "" && got.Thought != tc.want.Thought {
				t.Errorf("Thought = %q, want %q", got.Thought, tc.want.Thought)
			}
			if !equalInts(got.Coordinates, tc.want.Coordinates) {
				t.Errorf("Coordinates = %v, want %v", got.Coordinates, tc.want.Coordinates)
			}
		})
	}
}

// ---------------------------------------------------------------- validation ---

func TestParseActionRejects(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"empty input", "", "no JSON object"},
		{"whitespace only", "   \n\t ", "no JSON object"},
		{"prose with no JSON at all", "I am not sure what to do next.", "no JSON object"},
		{"opening brace never closed", `{"action":"click"`, "no JSON object"},
		{"malformed JSON", `{"action": click}`, "malformed action JSON"},
		{"unknown action", `{"action":"teleport","target":"Mars"}`, "unknown action"},
		{"missing action field", `{"thought":"hmm"}`, "unknown action"},
		{"click with neither coordinates nor target", `{"action":"click"}`, "needs coordinates or a target"},
		{"click with blank target", `{"action":"click","target":"   "}`, "needs coordinates or a target"},
		{"right_click with neither", `{"action":"right_click"}`, "needs coordinates or a target"},
		{"type with no text", `{"action":"type","text":""}`, "type needs text"},
		{"key with no key", `{"action":"key"}`, "key needs a key combination"},
		{"drag missing to", `{"action":"drag","coordinates":[1,2]}`, "drag needs coordinates and to"},
		{"drag missing coordinates", `{"action":"drag","to":[3,4]}`, "drag needs coordinates and to"},
		{"wait_for with no text", `{"action":"wait_for"}`, "wait_for needs the text"},
		{"shell with no command", `{"action":"shell"}`, "needs a command in text"},
		{"assert with no command", `{"action":"assert"}`, "needs a command in text"},
		{"coordinates of length 1", `{"action":"click","coordinates":[7]}`, "coordinates must be [x,y]"},
		{"coordinates of length 3", `{"action":"click","coordinates":[1,2,3]}`, "coordinates must be [x,y]"},
		{"mount_tool missing tool_name", `{"action":"mount_tool","tool_handler":"def foo(): pass"}`, "mount_tool needs tool_name"},
		{"mount_tool missing tool_handler", `{"action":"mount_tool","tool_name":"foo"}`, "mount_tool needs tool_handler code"},
		{"unmount_tool missing tool_name", `{"action":"unmount_tool"}`, "unmount_tool needs tool_name"},
		{"call_tool missing tool_name", `{"action":"call_tool"}`, "call_tool needs tool_name"},
		{"deep_search missing query", `{"action":"deep_search"}`, "deep_search needs query"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAction(tc.raw)
			if err == nil {
				t.Fatalf("ParseAction(%q) = nil error, want %q", tc.raw, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestParseActionAcceptsTargetOnlyClick(t *testing.T) {
	for _, kind := range []string{"click", "double_click", "right_click"} {
		got, err := ParseAction(`{"action":"` + kind + `","target":"Save"}`)
		if err != nil {
			t.Fatalf("%s with only a target: unexpected error %v", kind, err)
		}
		if len(got.Coordinates) != 0 {
			t.Errorf("%s: coordinates should stay empty, got %v", kind, got.Coordinates)
		}
	}
}

func TestParseActionAcceptsEmptyCoordinates(t *testing.T) {
	// A zero-length coordinate array is not the same as a malformed one.
	got, err := ParseAction(`{"action":"click","target":"Save","coordinates":[]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != protocol.ActClick {
		t.Errorf("Action = %q", got.Action)
	}
}

// ---------------------------------------------------------------- defaulting ---

func TestParseActionDefaults(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		check func(t *testing.T, a protocol.Action)
	}{
		{
			name: "wait_for gets a 120s timeout when unset",
			raw:  `{"action":"wait_for","text":"Build Succeeded"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Timeout != 120 {
					t.Errorf("Timeout = %d, want 120", a.Timeout)
				}
			},
		},
		{
			name: "wait_for keeps an explicit timeout",
			raw:  `{"action":"wait_for","text":"Done","timeout":45}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Timeout != 45 {
					t.Errorf("Timeout = %d, want 45", a.Timeout)
				}
			},
		},
		{
			name: "wait_for repairs a negative timeout",
			raw:  `{"action":"wait_for","text":"Done","timeout":-9}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Timeout != 120 {
					t.Errorf("Timeout = %d, want 120", a.Timeout)
				}
			},
		},
		{
			name: "wait with no amount defaults to 3",
			raw:  `{"action":"wait"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Amount != 3 {
					t.Errorf("Amount = %d, want 3", a.Amount)
				}
			},
		},
		{
			name: "wait with a negative amount is repaired",
			raw:  `{"action":"wait","amount":-30}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Amount != 3 {
					t.Errorf("Amount = %d, want 3", a.Amount)
				}
			},
		},
		{
			name: "wait is clamped at 300 seconds",
			raw:  `{"action":"wait","amount":99999}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Amount != 300 {
					t.Errorf("Amount = %d, want 300", a.Amount)
				}
			},
		},
		{
			name: "wait inside the band is untouched",
			raw:  `{"action":"wait","amount":17}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Amount != 17 {
					t.Errorf("Amount = %d, want 17", a.Amount)
				}
			},
		},
		{
			name: "scroll amount defaults to 3",
			raw:  `{"action":"scroll","coordinates":[400,300]}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Amount != 3 {
					t.Errorf("Amount = %d, want 3", a.Amount)
				}
			},
		},
		{
			name: "scroll keeps a negative amount so the page can scroll up",
			raw:  `{"action":"scroll","amount":-5}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Amount != -5 {
					t.Errorf("Amount = %d, want -5", a.Amount)
				}
			},
		},
		{
			name: "ask_human falls back to the summary",
			raw:  `{"action":"ask_human","summary":"the login needs a 2FA code","thought":"stuck"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Question != "the login needs a 2FA code" {
					t.Errorf("Question = %q, want the summary", a.Question)
				}
			},
		},
		{
			name: "ask_human falls back to the thought when there is no summary",
			raw:  `{"action":"ask_human","thought":"I cannot find the Save button"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Question != "I cannot find the Save button" {
					t.Errorf("Question = %q, want the thought", a.Question)
				}
			},
		},
		{
			name: "ask_human falls back to a canned question",
			raw:  `{"action":"ask_human"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Question == "" {
					t.Error("Question is empty, want a canned fallback")
				}
			},
		},
		{
			name: "ask_human keeps an explicit question",
			raw:  `{"action":"ask_human","question":"which account?","summary":"ignored"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Question != "which account?" {
					t.Errorf("Question = %q", a.Question)
				}
			},
		},
		{
			name: "done needs nothing but the kind",
			raw:  `{"action":"done","summary":"invoice filed"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Summary != "invoice filed" {
					t.Errorf("Summary = %q", a.Summary)
				}
			},
		},
		{
			name: "fail needs nothing but the kind",
			raw:  `{"action":"fail","summary":"the site is down"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Action != protocol.ActFail {
					t.Errorf("Action = %q", a.Action)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAction(tc.raw)
			if err != nil {
				t.Fatalf("ParseAction() error = %v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestParseActionDoesNotPanic(t *testing.T) {
	// Every one of these has tripped a naive brace scanner at some point.
	inputs := []string{
		"", "{", "}", "{{{{", `"`, `{"`, "```", "```json", "```json\n```",
		`{"action":"type","text":"\`, `{"a":"\\"}`, "\x00{", strings.Repeat("{", 1000),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseAction(%q) panicked: %v", in, r)
				}
			}()
			_, _ = ParseAction(in)
		}()
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
