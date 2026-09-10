package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Three replies from one live run on 2026-09-09, each the right action in the
// wrong shape, each of which failed the run under strict decoding.

func TestParseActionAcceptsEverySchemaFieldPresentButEmpty(t *testing.T) {
	raw := `<tool_call>
{"action": "shell", "text": "ls -la /home/agent/fleet-notes/", "command": "ls -la /home/agent/fleet-notes/",
 "target": "", "coordinates": "", "to": "", "mark": "", "key": "", "code": "", "query": "",
 "sub_goal": "", "sub_params": "", "wait_child": "", "tool_name": "", "tool_parameters": "",
 "mcp_server_id": "", "mcp_tool_name": "", "mcp_params": "", "thought": "see what exists"}
</tool_call>`
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || a.Text != "ls -la /home/agent/fleet-notes/" || a.Thought != "see what exists" {
		t.Fatalf("got %+v", a)
	}
	if a.Mark != 0 || len(a.Coordinates) != 0 || a.MCPParams != nil {
		t.Fatalf("empty fields should be absent, got %+v", a)
	}
}

func TestParseActionAcceptsTheSchemaAsXMLParameters(t *testing.T) {
	raw := "<tool_call>\n<function=Tool>\n<parameter=action>\nshell\n</parameter>\n<parameter=amount>\n1\n</parameter>\n" +
		"<parameter=code>\n\n</parameter>\n<parameter=coordinates>\n\n</parameter>\n<parameter=text>\ncat app.js\n</parameter>\n" +
		"<parameter=mark>\n\n</parameter>\n</function>\n</tool_call>"
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || a.Text != "cat app.js" || a.Amount != 1 {
		t.Fatalf("got %+v", a)
	}
}

func TestParseActionRejectsAnEmptyFunctionCallClearly(t *testing.T) {
	_, err := ParseAction("<tool_call>\n<function=shell>\n</function>\n</tool_call>")
	if err == nil {
		t.Fatal("a shell call with no command has nothing to run")
	}
	if !strings.Contains(err.Error(), "shell") {
		t.Fatalf("error should name the action: %v", err)
	}
}

func TestLenientReadsAShellCommandFromTheCodeField(t *testing.T) {
	a, err := ParseAction(`{"action":"shell","code":"cd /home/agent/fleet-notes && ls -la"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || a.Text != "cd /home/agent/fleet-notes && ls -la" || a.Code != "" {
		t.Fatalf("got %+v", a)
	}
	p, err := ParseAction(`{"action":"python","text":"print(1)"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Action != protocol.ActPython || p.Code != "print(1)" {
		t.Fatalf("got %+v", p)
	}
}

func TestLenientCoercesEveryNumericFieldIncludingTimeout(t *testing.T) {
	// The reply that failed run five: a perfectly good XML call with a quoted
	// timeout, which a hand-written coercion list did not know about.
	raw := "<tool_call>\n<function=shell>\n<parameter=action>\nshell\n</parameter>\n<parameter=text>\ncat /home/agent/fleet-notes/app.js\n</parameter>\n<parameter=timeout>\n10\n</parameter>\n</function>\n</tool_call>"
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || a.Text != "cat /home/agent/fleet-notes/app.js" || a.Timeout != 10 {
		t.Fatalf("got %+v", a)
	}
}

func TestLenientFlattensANestedActionObject(t *testing.T) {
	a, err := ParseAction(`{"thought": "read it", "action": {"name": "bash", "arguments": {"command": "cat app.js"}}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || a.Text != "cat app.js" || a.Thought != "read it" {
		t.Fatalf("got %+v", a)
	}
}

func TestLenientReadsAFencedToolCallArray(t *testing.T) {
	a, err := ParseAction("```json\n[\n{\"name\": \"bash\", \"arguments\": {\"command\": \"cat app.js\"}}\n]\n```")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || a.Text != "cat app.js" {
		t.Fatalf("got %+v", a)
	}
}

func TestLenientCoercesQuotedNumbersAndLists(t *testing.T) {
	a, err := ParseAction(`{"action": "click", "mark": "7", "coordinates": "[10, 20]", "wait_child": "true"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Mark != 7 || len(a.Coordinates) != 2 || a.Coordinates[1] != 20 || !a.WaitChild {
		t.Fatalf("got %+v", a)
	}
}

// The reply that failed run eight: a complete action whose shell text writes a
// README containing a fenced code block. The fence is inside a JSON string and
// must not be mistaken for a wrapper around the object.
func TestParseActionIgnoresFencesInsideTheObject(t *testing.T) {
	raw := "{\n  \"thought\": \"All four files are written. Now the README.\",\n  \"action\": \"shell\",\n" +
		"  \"text\": \"cat > /home/agent/fleet-notes/README.md << 'EOF'\\n# Fleet Notes\\n\\n```bash\\npython3 -m http.server 8000\\n```\\n\\n## Tests\\nOpen tests.html\\nEOF\"\n}"
	a, err := ParseAction(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.Action != protocol.ActShell || !strings.Contains(a.Text, "```bash") || !strings.HasSuffix(a.Text, "EOF") {
		t.Fatalf("got %+v", a)
	}
	// A fence that wraps the object is still unwrapped.
	b, err := ParseAction("Here you go:\n```json\n{\"action\": \"done\", \"summary\": \"ok\"}\n```")
	if err != nil || b.Action != protocol.ActDone {
		t.Fatalf("wrapped object: %v %+v", err, b)
	}
}
