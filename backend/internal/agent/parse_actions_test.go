package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ------------------------------------------------- vocabulary consistency ---

// promptActionVocabulary pulls the action enum out of the schema block in the
// system prompt: "action": "click|double_click|...".
func promptActionVocabulary(t *testing.T) []protocol.ActionKind {
	t.Helper()
	const marker = `"action": "`
	i := strings.Index(systemPrompt, marker)
	if i < 0 {
		t.Fatal("could not find the action enum in the system prompt")
	}
	rest := systemPrompt[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatal("unterminated action enum in the system prompt")
	}
	var out []protocol.ActionKind
	for _, name := range strings.Split(rest[:j], "|") {
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, protocol.ActionKind(name))
		}
	}
	if len(out) < 5 {
		t.Fatalf("parsed only %d actions out of the prompt, that cannot be right", len(out))
	}
	return out
}

// TestPromptAndParserAgreeOnTheActionVocabulary is the regression guard for a
// real bug: the prompt advertised "remember", "recall" and "speak" (with three
// dedicated rules telling the model to use them) while validActions rejected
// them. A model that obeyed the prompt got "unknown action", and three of those
// in a row fails the whole task in Runner.loop.
func TestPromptAndParserAgreeOnTheActionVocabulary(t *testing.T) {
	advertised := promptActionVocabulary(t)

	inPrompt := map[protocol.ActionKind]bool{}
	for _, a := range advertised {
		inPrompt[a] = true
		if !validActions[a] {
			t.Errorf("the system prompt advertises %q but ParseAction rejects it", a)
		}
	}
	for a := range validActions {
		if !inPrompt[a] {
			t.Errorf("ParseAction accepts %q but the system prompt never offers it", a)
		}
	}
}

func TestEveryAdvertisedActionParses(t *testing.T) {
	// A minimal well-formed reply for each action the prompt offers. If someone
	// adds an action to the prompt, this fails until the parser handles it.
	minimal := map[protocol.ActionKind]string{
		protocol.ActClick:        `{"action":"click","target":"Save"}`,
		protocol.ActDoubleClick:  `{"action":"double_click","coordinates":[5,5]}`,
		protocol.ActRightClick:   `{"action":"right_click","mark":2}`,
		protocol.ActType:         `{"action":"type","text":"hello"}`,
		protocol.ActKey:          `{"action":"key","key":"ctrl+s"}`,
		protocol.ActScroll:       `{"action":"scroll","amount":5}`,
		protocol.ActDrag:         `{"action":"drag","coordinates":[1,2],"to":[3,4]}`,
		protocol.ActWait:         `{"action":"wait"}`,
		protocol.ActWaitFor:      `{"action":"wait_for","text":"Ready"}`,
		protocol.ActFocus:        `{"action":"focus","target":"Terminal"}`,
		protocol.ActShell:        `{"action":"shell","text":"ls"}`,
		protocol.ActPython:       `{"action":"python","code":"x=1"}`,
		protocol.ActSpawnAgent:   `{"action":"spawn_agent","sub_goal":"build it"}`,
		protocol.ActMsgPeer:      `{"action":"message_peer","target":"Research Bot","text":"found the invoice"}`,
		protocol.ActDelegateTask: `{"action":"delegate_task","target":"Research Bot","sub_goal":"summarise Q3"}`,
		protocol.ActMountTool:    `{"action":"mount_tool","tool_name":"t","tool_handler":"def t(): pass"}`,
		protocol.ActUnmountTool:  `{"action":"unmount_tool","tool_name":"t"}`,
		protocol.ActCallTool:     `{"action":"call_tool","tool_name":"t"}`,
		protocol.ActDeepSearch:   `{"action":"deep_search","query":"go 1.23"}`,
		protocol.ActRemember:     `{"action":"remember","text":"the build flag is -tags prod"}`,
		protocol.ActRecall:       `{"action":"recall","query":"how did we log in last time"}`,
		protocol.ActSpeak:        `{"action":"speak","text":"the deploy finished"}`,
		protocol.ActAssert:       `{"action":"assert","text":"test -f /tmp/out"}`,
		protocol.ActAskHuman:     `{"action":"ask_human","question":"which account?"}`,
		protocol.ActDone:         `{"action":"done","summary":"finished"}`,
		protocol.ActFail:         `{"action":"fail","summary":"blocked"}`,
	}

	for _, kind := range promptActionVocabulary(t) {
		raw, ok := minimal[kind]
		if !ok {
			t.Errorf("no sample reply for advertised action %q — add one", kind)
			continue
		}
		t.Run(string(kind), func(t *testing.T) {
			got, err := ParseAction(raw)
			if err != nil {
				t.Fatalf("ParseAction(%s) = %v", raw, err)
			}
			if got.Action != kind {
				t.Errorf("Action = %q, want %q", got.Action, kind)
			}
		})
	}
}

// --------------------------------------------- field fallbacks / aliasing ---

// Models routinely put the payload in the generic "text" field instead of the
// action-specific one. The parser repairs that rather than burning a retry;
// these pin the repairs down.
func TestParseActionFieldFallbacks(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		check func(t *testing.T, a protocol.Action)
	}{
		{
			name: "python takes code from text when code is absent",
			raw:  `{"action":"python","text":"print(1)"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Code != "print(1)" {
					t.Errorf("Code = %q, want %q", a.Code, "print(1)")
				}
			},
		},
		{
			name: "python prefers code over text when both are present",
			raw:  `{"action":"python","code":"real()","text":"decoy()"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Code != "real()" {
					t.Errorf("Code = %q, want %q", a.Code, "real()")
				}
			},
		},
		{
			name: "spawn_agent takes sub_goal from text",
			raw:  `{"action":"spawn_agent","text":"run the integration suite"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.SubGoal != "run the integration suite" {
					t.Errorf("SubGoal = %q", a.SubGoal)
				}
			},
		},
		{
			name: "spawn_agent keeps sub_skill_id, sub_params and wait_child",
			raw:  `{"action":"spawn_agent","sub_goal":"go","sub_skill_id":"sk-1","sub_params":{"repo":"acme"},"wait_child":true}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.SubSkillID != "sk-1" {
					t.Errorf("SubSkillID = %q", a.SubSkillID)
				}
				if a.SubParams["repo"] != "acme" {
					t.Errorf("SubParams = %v", a.SubParams)
				}
				if !a.WaitChild {
					t.Error("WaitChild = false, want true")
				}
			},
		},
		{
			name: "mount_tool takes tool_name from target",
			raw:  `{"action":"mount_tool","target":"parse_logs","tool_handler":"def parse_logs(): pass"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolName != "parse_logs" {
					t.Errorf("ToolName = %q", a.ToolName)
				}
			},
		},
		{
			name: "mount_tool takes tool_handler from code",
			raw:  `{"action":"mount_tool","tool_name":"t","code":"def t(): return 1"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolHandler != "def t(): return 1" {
					t.Errorf("ToolHandler = %q", a.ToolHandler)
				}
			},
		},
		{
			name: "mount_tool takes tool_handler from text when there is no code",
			raw:  `{"action":"mount_tool","tool_name":"t","text":"def t(): return 2"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolHandler != "def t(): return 2" {
					t.Errorf("ToolHandler = %q", a.ToolHandler)
				}
			},
		},
		{
			name: "mount_tool keeps description and parameters",
			raw:  `{"action":"mount_tool","tool_name":"t","tool_handler":"def t(): pass","tool_description":"does a thing","tool_parameters":{"n":1}}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolDescription != "does a thing" {
					t.Errorf("ToolDescription = %q", a.ToolDescription)
				}
				if _, ok := a.ToolParameters["n"]; !ok {
					t.Errorf("ToolParameters = %v", a.ToolParameters)
				}
			},
		},
		{
			name: "unmount_tool takes tool_name from target",
			raw:  `{"action":"unmount_tool","target":"parse_logs"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolName != "parse_logs" {
					t.Errorf("ToolName = %q", a.ToolName)
				}
			},
		},
		{
			name: "unmount_tool takes tool_name from text when target is empty",
			raw:  `{"action":"unmount_tool","text":"parse_logs"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolName != "parse_logs" {
					t.Errorf("ToolName = %q", a.ToolName)
				}
			},
		},
		{
			name: "call_tool takes tool_name from target and keeps parameters",
			raw:  `{"action":"call_tool","target":"parse_logs","tool_parameters":{"path":"/var/log/x"}}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.ToolName != "parse_logs" {
					t.Errorf("ToolName = %q", a.ToolName)
				}
				if a.ToolParameters["path"] != "/var/log/x" {
					t.Errorf("ToolParameters = %v", a.ToolParameters)
				}
			},
		},
		{
			name: "deep_search takes query from text",
			raw:  `{"action":"deep_search","text":"weather in Boise"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Query != "weather in Boise" {
					t.Errorf("Query = %q", a.Query)
				}
			},
		},
		{
			name: "deep_search falls back to sub_goal when there is no text",
			raw:  `{"action":"deep_search","sub_goal":"find the changelog"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Query != "find the changelog" {
					t.Errorf("Query = %q", a.Query)
				}
			},
		},
		{
			name: "recall takes query from text",
			raw:  `{"action":"recall","text":"the selector for the login button"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Query != "the selector for the login button" {
					t.Errorf("Query = %q", a.Query)
				}
			},
		},
		{
			name: "remember takes text from summary",
			raw:  `{"action":"remember","summary":"staging needs the VPN"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Text != "staging needs the VPN" {
					t.Errorf("Text = %q", a.Text)
				}
			},
		},
		{
			name: "ask_human synthesises a question from the summary",
			raw:  `{"action":"ask_human","summary":"a CAPTCHA is blocking me"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Question != "a CAPTCHA is blocking me" {
					t.Errorf("Question = %q", a.Question)
				}
			},
		},
		{
			name: "ask_human falls back to the thought",
			raw:  `{"action":"ask_human","thought":"I cannot read this dialog"}`,
			check: func(t *testing.T, a protocol.Action) {
				if a.Question != "I cannot read this dialog" {
					t.Errorf("Question = %q", a.Question)
				}
			},
		},
		{
			name: "ask_human has a last-resort question",
			raw:  `{"action":"ask_human"}`,
			check: func(t *testing.T, a protocol.Action) {
				if strings.TrimSpace(a.Question) == "" {
					t.Error("Question is empty; the operator alert would have no body")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAction(tc.raw)
			if err != nil {
				t.Fatalf("ParseAction(%s) = %v", tc.raw, err)
			}
			tc.check(t, got)
		})
	}
}

// ------------------------------------------------------- validation rules ---

func TestParseActionRejectsNewActionsMissingFields(t *testing.T) {
	cases := []struct{ name, raw, wantErr string }{
		{"python with nothing", `{"action":"python"}`, "python needs python code"},
		{"python with blank code", `{"action":"python","code":"   "}`, "python needs python code"},
		{"python with blank text", `{"action":"python","text":"  \n "}`, "python needs python code"},
		{"spawn_agent with nothing", `{"action":"spawn_agent"}`, "spawn_agent needs a sub-goal"},
		{"spawn_agent with blank sub_goal", `{"action":"spawn_agent","sub_goal":"  "}`, "spawn_agent needs a sub-goal"},
		{"mount_tool with blank tool_name", `{"action":"mount_tool","tool_name":"  ","tool_handler":"x"}`, "mount_tool needs tool_name"},
		{"mount_tool with blank handler", `{"action":"mount_tool","tool_name":"t","tool_handler":"  "}`, "mount_tool needs tool_handler"},
		{"unmount_tool with blank tool_name", `{"action":"unmount_tool","tool_name":"  "}`, "unmount_tool needs tool_name"},
		{"call_tool with blank tool_name", `{"action":"call_tool","tool_name":" "}`, "call_tool needs tool_name"},
		{"deep_search with blank query", `{"action":"deep_search","query":"  "}`, "deep_search needs query"},
		{"recall with nothing", `{"action":"recall"}`, "recall needs query"},
		{"recall with blank query", `{"action":"recall","query":"  "}`, "recall needs query"},
		{"remember with nothing", `{"action":"remember"}`, "remember needs text"},
		{"remember with blank text", `{"action":"remember","text":"   "}`, "remember needs text"},
		{"speak with nothing", `{"action":"speak"}`, "speak needs text"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAction(tc.raw)
			if err == nil {
				t.Fatalf("ParseAction(%s) succeeded, want an error", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// -------------------------------------------------------- set-of-marks ---

func TestParseActionMarkField(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantMark int
		wantErr  bool
	}{
		{"a mark alone is enough to click", `{"action":"click","mark":7}`, 7, false},
		{"mark alongside a target", `{"action":"click","mark":3,"target":"Save"}`, 3, false},
		{"mark alongside coordinates", `{"action":"click","mark":1,"coordinates":[10,20]}`, 1, false},
		{"double_click by mark", `{"action":"double_click","mark":12}`, 12, false},
		{"right_click by mark", `{"action":"right_click","mark":4}`, 4, false},
		{"mark 0 is absent, so a bare click is rejected", `{"action":"click","mark":0}`, 0, true},
		{"a negative mark does not count as targeting", `{"action":"click","mark":-1}`, 0, true},
		{"a negative mark is fine when a target is also given", `{"action":"click","mark":-1,"target":"Save"}`, -1, false},
		{"marks are irrelevant to non-pointer actions", `{"action":"key","key":"ctrl+s","mark":9}`, 9, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAction(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseAction(%s) succeeded, want an error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAction(%s) = %v", tc.raw, err)
			}
			if got.Mark != tc.wantMark {
				t.Errorf("Mark = %d, want %d", got.Mark, tc.wantMark)
			}
		})
	}
}

// ------------------------------------------------------------- summarise ---

func TestSummariseCoversTheNewActions(t *testing.T) {
	cases := []struct {
		name string
		in   protocol.Action
		want string
	}{
		{"python uses code", protocol.Action{Action: protocol.ActPython, Code: "print(1)"}, `python "print(1)"`},
		{"python falls back to text", protocol.Action{Action: protocol.ActPython, Text: "print(2)"}, `python "print(2)"`},
		{"spawn_agent uses sub_goal", protocol.Action{Action: protocol.ActSpawnAgent, SubGoal: "build"}, `spawn_agent "build"`},
		{"mount_tool names the tool", protocol.Action{Action: protocol.ActMountTool, ToolName: "t"}, "mount_tool t"},
		{"unmount_tool names the tool", protocol.Action{Action: protocol.ActUnmountTool, ToolName: "t"}, "unmount_tool t"},
		{"call_tool names the tool", protocol.Action{Action: protocol.ActCallTool, ToolName: "t"}, "call_tool t"},
		{"deep_search uses query", protocol.Action{Action: protocol.ActDeepSearch, Query: "go"}, `deep_search "go"`},
		{"deep_search falls back to text", protocol.Action{Action: protocol.ActDeepSearch, Text: "rust"}, `deep_search "rust"`},
		{"a mark is shown in the history", protocol.Action{Action: protocol.ActClick, Mark: 5}, "click [5]"},
		{"a mark with a label", protocol.Action{Action: protocol.ActClick, Mark: 5, Target: "Save"}, `click [5] "Save"`},
		{"coordinates when there is no mark", protocol.Action{Action: protocol.ActClick, Coordinates: []int{3, 4}}, "click at 3,4"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := summarise(tc.in); got != tc.want {
				t.Errorf("summarise() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSummariseNeverReturnsEmpty(t *testing.T) {
	// The history line goes into the next prompt; a blank one teaches the model
	// nothing and looks like a dropped turn.
	for kind := range validActions {
		if got := summarise(protocol.Action{Action: kind}); strings.TrimSpace(got) == "" {
			t.Errorf("summarise(%q) is empty", kind)
		}
	}
}

// ------------------------------------------------------- peer messaging ---

// message_peer and delegate_task were declared in the protocol but rejected by
// the parser and never offered by the prompt, so fleet-wide collaboration was
// unreachable. These pin the shapes the runner relies on.
func TestMessagePeerNeedsSomethingToSay(t *testing.T) {
	if _, err := ParseAction(`{"action":"message_peer","target":"Bob"}`); err == nil {
		t.Fatal("expected an error for message_peer with no text or question")
	}
	// A question counts as something to say.
	if _, err := ParseAction(`{"action":"message_peer","question":"did you finish?"}`); err != nil {
		t.Fatalf("question form should parse: %v", err)
	}
}

// Omitting target is how an agent broadcasts, so it must stay legal.
func TestMessagePeerWithoutTargetIsABroadcast(t *testing.T) {
	got, err := ParseAction(`{"action":"message_peer","text":"the build is green"}`)
	if err != nil {
		t.Fatalf("broadcast form should parse: %v", err)
	}
	if got.Target != "" {
		t.Fatalf("expected no target, got %q", got.Target)
	}
}

func TestDelegateTaskNeedsATargetAndAGoal(t *testing.T) {
	if _, err := ParseAction(`{"action":"delegate_task","sub_goal":"do it"}`); err == nil {
		t.Fatal("delegation without a target should be rejected")
	}
	if _, err := ParseAction(`{"action":"delegate_task","target":"Bob"}`); err == nil {
		t.Fatal("delegation without a sub_goal should be rejected")
	}
	got, err := ParseAction(`{"action":"delegate_task","target":"Bob","sub_goal":"summarise Q3"}`)
	if err != nil {
		t.Fatalf("well-formed delegation failed: %v", err)
	}
	if got.Target != "Bob" || got.SubGoal != "summarise Q3" {
		t.Fatalf("fields not carried through: %+v", got)
	}
}
