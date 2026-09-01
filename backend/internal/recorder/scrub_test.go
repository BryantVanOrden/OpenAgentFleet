package recorder

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A recording is keyboard bytes and accessibility labels, and both produce
// things that are not text. One NUL used to fail the whole save with
// "unsupported Unicode escape sequence", after the demonstration was already
// over and the events discarded.
func TestCompiledSkillIsStorable(t *testing.T) {
	events := []protocol.RawEvent{
		{Type: "click", X: 690, Y: 121, Window: "Firefox\x00", Role: "entry", Label: "Address\x00bar"},
		{Type: "key", Key: "a\x00", Window: "Firefox"},
		{Type: "key", Key: "b", Window: "Firefox"},
	}
	sk := Compile("Open the app\x00", events)

	if strings.ContainsRune(sk.Name, 0) {
		t.Error("skill name still carries a NUL")
	}
	if strings.ContainsRune(sk.Markdown, 0) {
		t.Error("rendered markdown still carries a NUL")
	}
	for _, s := range sk.Steps {
		for name, v := range map[string]string{
			"window": s.Window, "role": s.Role, "label": s.Label,
			"text": s.Text, "key": s.Key,
		} {
			if strings.ContainsRune(v, 0) {
				t.Errorf("step %d %s still carries a NUL: %q", s.Index, name, v)
			}
		}
	}
	// The real test: what goes to the database is JSON, and \u0000 in it is
	// what Postgres refuses.
	blob, err := json.Marshal(sk.Steps)
	if err != nil {
		t.Fatalf("steps do not marshal: %v", err)
	}
	if strings.Contains(string(blob), `\u0000`) {
		t.Errorf("encoded steps contain \\u0000:\n%s", blob)
	}
}

func TestScrubKeepsRealTyping(t *testing.T) {
	in := "line one\nline\ttwo\r\n"
	if got := scrub(in); got != in {
		t.Errorf("scrub(%q) = %q; tabs and newlines are typed on purpose", in, got)
	}
	if got := scrub("a\x00b\x07c"); got != "abc" {
		t.Errorf("scrub did not drop the controls: %q", got)
	}
}
