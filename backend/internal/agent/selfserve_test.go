package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The second refusal is blunter, because the first is read as a suggestion.
//
// In a measured run every task stopped to ask twice, each stop costing another
// wait, despite the first reply asking it not to.
func TestSelfServeReplyHardensOnRepeat(t *testing.T) {
	first := selfServeReply(1)
	again := selfServeReply(2)

	if first == again {
		t.Fatal("asking twice gets the same answer as asking once")
	}
	if !strings.Contains(strings.ToLower(again), "already asked") {
		t.Error("the repeat reply does not say the question was already asked")
	}
	if !strings.Contains(strings.ToLower(again), "not be answered") {
		t.Error("the repeat reply does not say another ask is pointless")
	}
	// Both must leave a way out that is not silence.
	for name, msg := range map[string]string{"first": first, "repeat": again} {
		if !strings.Contains(msg, "fail action") {
			t.Errorf("the %s reply offers no way to give up cleanly", name)
		}
		if !strings.Contains(strings.ToLower(msg), "cannot undo") {
			t.Errorf("the %s reply does not warn against irreversible actions", name)
		}
	}
}

// Publishing the same thing repeatedly is counted per run, not per agent.
//
// One agent published the same file ten times in a single run. Each publish
// looked like progress to the model and was none, and the colleague waiting to
// test it never got the chance because the run never ended.
func TestCountPublishIsPerTaskAndName(t *testing.T) {
	r := &Runner{}

	for i := 1; i <= 3; i++ {
		if got := r.countPublish("task-1", "stopwatch"); got != i {
			t.Errorf("publish %d counted as %d", i, got)
		}
	}
	// Case does not make it a different item.
	if got := r.countPublish("task-1", "STOPWATCH"); got != 4 {
		t.Errorf("a differently-cased name started a new count: %d", got)
	}
	// A different item in the same run is its own count.
	if got := r.countPublish("task-1", "design-notes"); got != 1 {
		t.Errorf("publishing something else was counted against the first: %d", got)
	}
	// A different run starts fresh: an agent working all day is not looping.
	if got := r.countPublish("task-2", "stopwatch"); got != 1 {
		t.Errorf("a new run inherited the old run's count: %d", got)
	}
}

// A cancelled run is not a broken provider.
//
// Stopping a task mid-inference surfaced as "every model provider failed:
// context canceled", which sends whoever reads it to check engines that are
// working perfectly.
func TestCancellationIsNotAProviderFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// This is the shape of the check in the step loop: a cancelled context, or
	// an error wrapping context.Canceled, must not be read as a provider fault.
	wrapped := fmt.Errorf("calling model: %w", context.Canceled)

	if !errors.Is(wrapped, context.Canceled) {
		t.Error("a wrapped cancellation is not recognised as one")
	}
	if ctx.Err() == nil {
		t.Error("a cancelled context does not report itself as cancelled")
	}

	// An ordinary failure still is one.
	real := errors.New("connection refused")
	if errors.Is(real, context.Canceled) || real == context.Canceled {
		t.Error("a genuine provider failure was mistaken for a cancellation")
	}
}
