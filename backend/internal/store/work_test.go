package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// An "app" is rendered in a web view, so anything that is not a web page
// produces a Play button showing a wall of source code.
//
// A bot did exactly this: it published a Python file as an app and nothing
// stopped it.
func TestAppMustBeAWebPage(t *testing.T) {
	good := `<!DOCTYPE html><html><body><canvas id="g"></canvas>
	<script>const c=document.getElementById('g')</script></body></html>`
	if err := checkAppDocument(good); err != nil {
		t.Errorf("a real page was rejected: %v", err)
	}

	// A canvas without a body is still a page.
	if err := checkAppDocument(`<html><canvas></canvas></html>`); err != nil {
		t.Errorf("a canvas-only page was rejected: %v", err)
	}

	for _, bad := range []struct {
		name    string
		content string
	}{
		{"python", "class Game:\n    def __init__(self):\n        pass\n"},
		{"markdown", "# Game design\n\nTap to move.\n"},
		{"fragment", "<script>alert(1)</script>"},
		{"empty", ""},
	} {
		if err := checkAppDocument(bad.content); err == nil {
			t.Errorf("%s was accepted as an app", bad.name)
		}
	}
}

// The web view has no network, so an external reference is not a style
// choice -- it is a resource that will never arrive.
func TestAppMustBeSelfContained(t *testing.T) {
	for _, bad := range []string{
		`<!DOCTYPE html><html><body><script src="https://cdn.example.com/p.js"></script></body></html>`,
		`<!DOCTYPE html><html><body><img src='http://example.com/a.png'></body></html>`,
		`<!DOCTYPE html><html><head><link href="https://fonts.example.com/f.css"></head><body></body></html>`,
	} {
		if err := checkAppDocument(bad); err == nil {
			t.Errorf("an app reaching the network was accepted: %s", bad[:60])
		}
	}

	// A URL in text is not a resource load, and refusing it would stop an
	// agent writing a game that mentions a website.
	ok := `<!DOCTYPE html><html><body><p>See https://example.com for rules</p></body></html>`
	if err := checkAppDocument(ok); err != nil {
		t.Errorf("a mentioned URL was treated as a resource load: %v", err)
	}
}

// No SQL may still reference instances.org_id.
//
// That column was replaced by the instance_orgs join table and dropped. Two
// raw SQL statements kept using it and nothing caught them, because a search
// for the Go field name does not look inside query strings -- /api/orgs
// returned 500 for every caller until a live smoke test hit it. A compiler
// cannot check SQL, so this does.
func TestNoSQLReferencesTheDroppedOrgColumn(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// Matches "instances ... org_id" and "org_id ... instances" within one
	// statement, while allowing the join table's own name.
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		for _, stmt := range strings.Split(text, "`") {
			lower := strings.ToLower(stmt)
			if !strings.Contains(lower, "org_id") {
				continue
			}
			// The join table and other tables legitimately have org_id.
			cleaned := strings.ReplaceAll(lower, "instance_orgs", "")
			if strings.Contains(cleaned, "instances") && strings.Contains(cleaned, "org_id") {
				t.Errorf("%s: SQL still joins instances to org_id, which no longer exists:\n%s",
					f, strings.TrimSpace(stmt))
			}
		}
	}
}

// A finished web page is an app even when the agent files it as a file.
//
// Asked for work_kind "app", agents repeatedly published a complete HTML
// document as a "file", so it appeared in the catalog as a wall of source with
// no way to run it — the one thing the operator wanted from it.
func TestAWebPageFiledAsAFileBecomesAnApp(t *testing.T) {
	page := `<!DOCTYPE html><html><body><canvas id="c"></canvas>
	<script>document.getElementById('c')</script></body></html>`

	// It passes the app check, so filing it as a file is a mislabel.
	if err := checkAppDocument(page); err != nil {
		t.Fatalf("a real page failed the app check: %v", err)
	}

	// Ordinary text is left alone: a design note is not an app.
	for _, notAnApp := range []string{
		"# Design notes\n\nTap to score.",
		"class Game:\n    pass\n",
		"",
	} {
		if checkAppDocument(notAnApp) == nil {
			t.Errorf("ordinary text would be promoted to an app: %q", notAnApp)
		}
	}

	// A page that reaches the network is not promoted either — it would fail
	// to run, and a broken Play button is worse than plain text.
	networked := `<!DOCTYPE html><html><body><script src="https://x.example/a.js"></script></body></html>`
	if checkAppDocument(networked) == nil {
		t.Error("a page that loads over the network would be promoted")
	}
}

// A tester that had just finished testing an app published its report as an
// app, was refused, and never tried again: the run's whole output was lost to
// one wrong word.
func TestProseLabelledAsAnAppIsFiledAsAFile(t *testing.T) {
	w := &protocol.WorkItem{
		Name:    "rollr_test_report",
		Kind:    protocol.WorkApp,
		Content: "# Rollr test report\n\nRolling 3d6 shows \"Rolling...\" and never settles.\n",
	}
	if err := validateWorkItem(w); err != nil {
		t.Fatalf("a mislabelled report should be filed, not refused: %v", err)
	}
	if w.Kind != protocol.WorkFile {
		t.Errorf("kind = %q, want file", w.Kind)
	}
}

// A page that was meant to be a page and is broken is still worth refusing.
func TestBrokenPageLabelledAsAnAppIsStillRefused(t *testing.T) {
	w := &protocol.WorkItem{
		Name:    "halfbuilt",
		Kind:    protocol.WorkApp,
		Content: "<div id=\"app\"><script>start()</script></div>",
	}
	if err := validateWorkItem(w); err == nil {
		t.Error("a broken page should be refused, not quietly filed as a file")
	}
}

// The catalog was one flat list because publishing only ever sent a name.
func TestBelongsIn(t *testing.T) {
	yes := [][2]string{
		{"dice", "dice"},                   // the app itself
		{"dice-spec", "dice"},              // hyphen
		{"convtest_review.md", "convtest"}, // underscore
		{"mdbox.notes", "mdbox"},           // dot
		{"mini game design", "mini game"},  // space, and a folder with one
	}
	for _, c := range yes {
		if !belongsIn(c[0], c[1]) {
			t.Errorf("%q should belong in %q", c[0], c[1])
		}
	}
	no := [][2]string{
		{"convtest", "convert"}, // not a prefix
		{"convert", "convtest"}, // nor the other way
		{"dicey", "dice"},       // prefix but no separator
		{"dice-spec", ""},       // no folder
		{"stopwatch", "stop"},   // prefix but no separator
	}
	for _, c := range no {
		if belongsIn(c[0], c[1]) {
			t.Errorf("%q should not belong in %q", c[0], c[1])
		}
	}
}
