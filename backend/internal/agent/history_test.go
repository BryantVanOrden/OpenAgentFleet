package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Builder read its own index.html five turns running because the history
// showed it 500 characters of a 1.3 KB file. The newest outcome is shown in
// full now; older ones are trimmed so the prompt does not grow without bound.
func TestRenderHistoryShowsTheNewestOutcomeInFull(t *testing.T) {
	long := strings.Repeat("<div>row</div>\n", 200) // ~3 KB
	history := []turnSummary{
		{Step: 1, Action: `shell "cat index.html"`, Outcome: long},
		{Step: 2, Action: `shell "cat index.html"`, Outcome: long},
	}
	var sb strings.Builder
	renderHistory(&sb, history)
	out := sb.String()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Header, one trimmed line for step 1, then step 2's outcome spread over
	// its own lines: the newest is not flattened or cut.
	if len(lines) < 100 {
		t.Fatalf("newest outcome should be kept whole, got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[1], "1. ") || len(lines[1]) > olderOutputChars+80 {
		t.Fatalf("older outcome should be one trimmed line, got %d chars: %.80s", len(lines[1]), lines[1])
	}
	if strings.Count(out, "<div>row</div>") < 200 {
		t.Fatalf("newest outcome lost content: %d rows of 200", strings.Count(out, "<div>row</div>"))
	}
}

// Reading four files to understand a project must leave all four in view.
func TestRenderHistoryKeepsSeveralRecentOutputsWhole(t *testing.T) {
	body := func(name string) string { return strings.Repeat("// "+name+" line\n", 120) } // ~2 KB each
	history := []turnSummary{
		{Step: 1, Action: `shell "cat app.js"`, Outcome: body("app")},
		{Step: 2, Action: `shell "cat index.html"`, Outcome: body("index")},
		{Step: 3, Action: `shell "cat styles.css"`, Outcome: body("styles")},
		{Step: 4, Action: `shell "cat tests.html"`, Outcome: body("tests")},
	}
	var sb strings.Builder
	renderHistory(&sb, history)
	out := sb.String()
	for _, name := range []string{"app", "index", "styles", "tests"} {
		if n := strings.Count(out, "// "+name+" line"); n < 120 {
			t.Errorf("%s: %d of 120 lines survived; recent reads must all stay whole", name, n)
		}
	}
}

func TestRenderHistoryDoesNotSpendTheBudgetOnRepeats(t *testing.T) {
	long := strings.Repeat("x", 9000)
	history := []turnSummary{
		{Step: 1, Action: `shell "cat other.js"`, Outcome: strings.Repeat("y", 9000)},
		{Step: 2, Action: `shell "cat app.js"`, Outcome: long},
		{Step: 3, Action: `shell "cat app.js"`, Outcome: long},
		{Step: 4, Action: `shell "cat app.js"`, Outcome: long},
	}
	var sb strings.Builder
	renderHistory(&sb, history)
	out := sb.String()
	if strings.Count(out, long) != 1 {
		t.Fatalf("the repeated file should be whole once, got %d", strings.Count(out, long))
	}
	if !strings.Contains(out, strings.Repeat("y", 9000)) {
		t.Fatal("the other file should still fit in the budget once repeats are skipped")
	}
}

func TestIsReadTellsLookingFromChanging(t *testing.T) {
	reads := []string{"cat app.js", "ls -la /home/agent", "cd /home/agent/fleet-notes && cat -n app.js | head -80", "wc -l app.js styles.css", "grep -n store app.js"}
	writes := []string{"cat > app.js <<'EOF'\nx\nEOF", "python3 -m http.server 8000 &", "mkdir -p x", "cat a.js > b.js", "sed -i 's/a/b/' app.js"}
	for _, c := range reads {
		if !isRead(protocol.Action{Action: protocol.ActShell, Text: c}) {
			t.Errorf("%q should be a read", c)
		}
	}
	for _, c := range writes {
		if isRead(protocol.Action{Action: protocol.ActShell, Text: c}) {
			t.Errorf("%q should not be a read", c)
		}
	}
	h := make([]turnSummary, 8)
	for i := range h {
		h[i] = turnSummary{Read: true}
	}
	if !readsOnly(h, 8) {
		t.Fatal("eight reads are reads only")
	}
	h[5].Read = false
	if readsOnly(h, 8) {
		t.Fatal("one write breaks the streak")
	}
}

func TestRepeatsCountsTheSameCommandInRecentTurns(t *testing.T) {
	cat := `shell "cat index.html"`
	history := []turnSummary{
		{Step: 1, Action: `shell "ls"`},
		{Step: 2, Action: cat},
		{Step: 3, Action: `shell "ls"`},
		{Step: 4, Action: cat},
	}
	if n := repeats(history, cat); n != 2 {
		t.Fatalf("repeats = %d, want 2", n)
	}
	if n := repeats(history, `shell "pwd"`); n != 0 {
		t.Fatalf("repeats of a new command = %d, want 0", n)
	}
	// Only the recent window counts: a command from long ago is not a loop.
	old := []turnSummary{{Step: 0, Action: cat}, {Step: 0, Action: cat}, {Step: 0, Action: cat}}
	for i := 0; i < 6; i++ {
		old = append(old, turnSummary{Step: i + 1, Action: `shell "ls"`})
	}
	if n := repeats(old, cat); n != 0 {
		t.Fatalf("repeats outside the last six = %d, want 0", n)
	}
}

func TestShellQuoteKeepsAPathWhole(t *testing.T) {
	if got := shellQuote("/home/agent/fleet-notes/index.html"); got != "'/home/agent/fleet-notes/index.html'" {
		t.Fatalf("got %s", got)
	}
	if got := shellQuote("/tmp/it's here/a b.txt"); got != `'/tmp/it'\''s here/a b.txt'` {
		t.Fatalf("got %s", got)
	}
}

func TestCorrectionNamesTheActualProblem(t *testing.T) {
	if c := correctionFor(true); !strings.Contains(c, "cut off") || !strings.Contains(c, "smaller") {
		t.Fatalf("truncated correction should ask for smaller steps: %q", c)
	}
	if c := correctionFor(false); !strings.Contains(c, "described") || !strings.Contains(c, "JSON action") {
		t.Fatalf("narration correction should ask for the action: %q", c)
	}
}

func TestOutputLimitNoteSpeaksInLines(t *testing.T) {
	note := outputLimitNote(2048)
	for _, want := range []string{"2048 tokens", "170 lines", "append the rest with >>"} {
		if !strings.Contains(note, want) {
			t.Errorf("note should say %q: %s", want, note)
		}
	}
	if capHint(2048, false) != "" || !strings.Contains(capHint(2048, true), "2048") {
		t.Fatal("the hint names the limit only for a truncated reply")
	}
}
