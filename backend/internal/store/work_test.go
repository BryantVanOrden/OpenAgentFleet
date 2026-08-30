package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
