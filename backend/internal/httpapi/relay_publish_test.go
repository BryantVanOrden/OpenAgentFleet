package httpapi

import (
	"strings"
	"testing"
)

// Run 16 of the Fleet Tasks soak: Builder published five files at steps 12-16
// and spent twenty more steps checking them and composing the announcement
// with the address. The relay handed on at step 12, so Checker's brief named
// one file, gave no address and quoted a fragment of a thought. What a running
// part publishes has to travel with the part when it ends.
func TestPublishesFromARunningPartAreKeptForTheHandoff(t *testing.T) {
	r := newRelay()
	builder := member("b", "Builder", stageBuild)
	tester := member("t", "Checker", stageTest)
	r.join("task-1", "build it", "broadcast", builder)
	r.waitFor("build it", "broadcast", tester)

	r.notePublished("task-1", publishedItem{desc: `file "fleet-tasks/index.html" (version 1)`, name: "fleet-tasks/index.html"})
	r.notePublished("task-1", publishedItem{desc: `app "fleet-tasks/app.js" (version 1)`, name: "fleet-tasks/app.js"})

	// A retry or a marathon window keeps the same member, so the list must
	// follow the task id.
	if _, _, ok := r.retry("task-1", "task-2"); !ok {
		t.Fatal("retry should be allowed")
	}
	r.rebind("task-2", "task-3")

	items := r.published("task-3")
	if len(items) != 2 || items[1].name != "fleet-tasks/app.js" {
		t.Fatalf("published items did not survive retry and rebind: %+v", items)
	}
	if got := describePublished(items); !strings.Contains(got, "index.html") || !strings.Contains(got, "app.js") {
		t.Errorf("describePublished = %q", got)
	}
	// Handed on once: a second read is empty, so a later publish for a new
	// request does not carry the old files.
	if again := r.published("task-3"); len(again) != 0 {
		t.Errorf("published should be cleared once read, got %+v", again)
	}
	// Nothing is recorded for a task the relay does not know.
	r.notePublished("nope", publishedItem{desc: "x", name: "x"})
	if got := r.published("nope"); got != nil {
		t.Errorf("unknown task should have nothing, got %+v", got)
	}
}

// A stall is a tester stuck on an address bar, not a verdict on the work: it is
// restarted with a note, where handing it on as "did not finish" tested nothing.
func TestAStalledPartIsRetriedLikeASystemFailure(t *testing.T) {
	if !systemFailure("stalled 3 times without making progress; last action: click [51]") {
		t.Error("a stall should be retried")
	}
	if systemFailure("agent gave up") || systemFailure("ran out of steps") {
		t.Error("the model's own verdict is not retried")
	}
}
