package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The reply that failed Builder's run: Qwen's XML tool call around a shell
// heredoc. The heredoc is full of braces, so the plain JSON search would have
// seized on a JavaScript object inside it.
const qwenXMLReply = "I'll create the Fleet Notes app files. Let me start by writing the index.html file.\n\n" +
	"<tool_call>\n<function=shell>\n<parameter=command>\n" +
	"mkdir -p /home/agent/fleet-notes && cat > /home/agent/fleet-notes/app.js <<'EOF'\n" +
	"const store = { notes: [] };\nfunction add(n) { store.notes.push(n); }\nEOF\n" +
	"</parameter>\n</function>\n</tool_call>"

func TestParseActionAcceptsQwenXMLToolCall(t *testing.T) {
	a, err := ParseAction(qwenXMLReply)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell {
		t.Fatalf("action = %q, want shell", a.Action)
	}
	if !strings.HasPrefix(a.Text, "mkdir -p /home/agent/fleet-notes && cat > ") {
		t.Fatalf("command lost its head: %q", a.Text)
	}
	if !strings.Contains(a.Text, "function add(n) { store.notes.push(n); }") {
		t.Fatalf("heredoc body lost: %q", a.Text)
	}
	if !strings.HasSuffix(a.Text, "\nEOF") {
		t.Fatalf("heredoc terminator lost, command ends %q", a.Text[len(a.Text)-12:])
	}
}

func TestParseActionAcceptsXMLToolCallWithSeveralParameters(t *testing.T) {
	raw := "<tool_call>\n<function=click>\n<parameter=label>Save</parameter>\n<parameter=mark>7</parameter>\n</function>\n</tool_call>"
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActClick || a.Target != "Save" || a.Mark != 7 {
		t.Fatalf("got %+v", a)
	}
}

func TestParseActionAcceptsHermesJSONToolCall(t *testing.T) {
	raw := "<tool_call>\n{\"name\": \"python\", \"arguments\": {\"code\": \"print(1+1)\"}}\n</tool_call>"
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActPython || a.Code != "print(1+1)" {
		t.Fatalf("got %+v", a)
	}
}

func TestParseActionAcceptsOpenAIShapedToolCall(t *testing.T) {
	// Arguments as a JSON string, the way the OpenAI wire format carries them.
	raw := `{"function": {"name": "type", "arguments": "{\"text\": \"hello\", \"target\": \"Search\"}"}}`
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActType || a.Text != "hello" || a.Target != "Search" {
		t.Fatalf("got %+v", a)
	}
}

func TestParseActionStillPrefersPlainActionJSON(t *testing.T) {
	// A normal reply is untouched: the tool-call path must not claim it.
	raw := `{"thought": "open it", "action": "click", "mark": 3}`
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActClick || a.Mark != 3 || a.Thought != "open it" {
		t.Fatalf("got %+v", a)
	}
	// And a tool call naming an action we do not have is still rejected, with
	// the name in the error so the model's next try can be steered.
	if _, err := ParseAction("<function=teleport><parameter=to>moon</parameter></function>"); err == nil || !strings.Contains(err.Error(), "teleport") {
		t.Fatalf("unknown tool should be rejected by name, got %v", err)
	}
}
