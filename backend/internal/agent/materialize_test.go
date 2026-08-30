package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

type fakePlacer struct {
	path    string
	content []byte
	err     error
}

func (f *fakePlacer) PlaceFile(_ context.Context, _, path string, content []byte) error {
	f.path, f.content = path, content
	return f.err
}

func TestRenderableWork(t *testing.T) {
	yes := []protocol.WorkItem{
		{Kind: protocol.WorkApp, Content: "anything"},
		{Kind: protocol.WorkFile, Content: "<!DOCTYPE html><body>hi</body>"},
		{Kind: protocol.WorkFile, Content: "<html><body>hi</body></html>"},
	}
	for _, w := range yes {
		if !renderable(w) {
			t.Errorf("%s %q should be renderable", w.Kind, w.Content[:10])
		}
	}
	no := []protocol.WorkItem{
		{Kind: protocol.WorkFile, Content: "# a markdown defect report"},
		{Kind: protocol.WorkFile, Content: "plain notes"},
	}
	for _, w := range no {
		if renderable(w) {
			t.Errorf("%q should not be renderable", w.Content)
		}
	}
}

// A catalog name becomes a file name, so it must not be able to climb out of
// the directory it is written into.
func TestSafeFileNameCannotEscape(t *testing.T) {
	for in, want := range map[string]string{
		"rollr":            "rollr",
		"convtest_review":  "convtest_review",
		"../../etc/passwd": "etc-passwd",
		"a b c":            "a-b-c",
		"Rollr.HTML":       "rollr-html",
		"/absolute":        "absolute",
	} {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", in, got, want)
		}
		if got := safeFileName(in); strings.Contains(got, "/") || strings.Contains(got, "..") {
			t.Errorf("safeFileName(%q) = %q, which can escape", in, got)
		}
	}
}

func TestMaterializeWritesTheAppAndReportsItsPath(t *testing.T) {
	fp := &fakePlacer{}
	r := &Runner{placer: fp, log: testLogger()}
	w := protocol.WorkItem{Name: "rollr", Kind: protocol.WorkApp, Content: "<html>dice</html>"}

	path, ok := r.materialize(context.Background(), "inst-1", w)
	if !ok {
		t.Fatal("materialize reported failure")
	}
	if path != "/home/agent/work/rollr.html" {
		t.Errorf("path = %q", path)
	}
	if string(fp.content) != "<html>dice</html>" {
		t.Errorf("wrong content written: %q", fp.content)
	}
}

// A tester with no way to place files still gets the source, not an error.
func TestMaterializeIsOptional(t *testing.T) {
	r := &Runner{placer: nil, log: testLogger()}
	if _, ok := r.materialize(context.Background(), "inst-1",
		protocol.WorkItem{Name: "rollr", Kind: protocol.WorkApp, Content: "<html></html>"}); ok {
		t.Error("materialize should report failure with no placer")
	}

	fp := &fakePlacer{err: errors.New("container gone")}
	r = &Runner{placer: fp, log: testLogger()}
	if _, ok := r.materialize(context.Background(), "inst-1",
		protocol.WorkItem{Name: "rollr", Kind: protocol.WorkApp, Content: "<html></html>"}); ok {
		t.Error("materialize should report failure when the write fails")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
