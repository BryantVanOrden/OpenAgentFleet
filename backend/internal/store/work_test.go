package store

import "testing"

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
