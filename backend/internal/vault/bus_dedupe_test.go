package vault

import (
	"context"
	"testing"
)

// An agent saying the same thing twice in a thread says it once.
//
// Two agents deadlocked in fleet comms and sent the same sentence three times
// in forty seconds -- one asking for a design the other was never going to
// produce. A model with nothing new to say repeats itself, and every repeat
// provokes another reply.
func TestAgentRepeatsAreDropped(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	const line = "Hi Researcher, could you share the design details?"
	first := b.SendMessageIn(ctx, "broadcast", "bot-b", "Builder", "bot-r", "message", line, nil)
	again := b.SendMessageIn(ctx, "broadcast", "bot-b", "Builder", "bot-r", "message", line, nil)

	msgs := b.ListConversationMessages(ctx, "broadcast", 50)
	if len(msgs) != 1 {
		t.Errorf("the repeat was stored: %d messages", len(msgs))
	}
	// The caller gets a real message either way, so anything showing what was
	// sent shows the one that exists.
	if again.ID != first.ID {
		t.Errorf("a repeat returned a different message: %q vs %q", again.ID, first.ID)
	}

	// Something new still goes through.
	b.SendMessageIn(ctx, "broadcast", "bot-b", "Builder", "bot-r", "message", "different", nil)
	if got := len(b.ListConversationMessages(ctx, "broadcast", 50)); got != 2 {
		t.Errorf("a new message was dropped: %d in the thread", got)
	}

	// A different agent saying the same thing is not a repeat.
	b.SendMessageIn(ctx, "broadcast", "bot-r", "Researcher", "bot-b", "message", line, nil)
	if got := len(b.ListConversationMessages(ctx, "broadcast", 50)); got != 3 {
		t.Errorf("another agent was silenced by the first one's message: %d", got)
	}
}

// The operator is never deduplicated: saying the same thing twice on purpose
// is a thing people do, and it is how you prod a stuck fleet.
func TestOperatorRepeatsAreKept(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	b.SendMessageIn(ctx, "broadcast", "", "Operator", "broadcast", "message", "status?", nil)
	b.SendMessageIn(ctx, "broadcast", "", "Operator", "broadcast", "message", "status?", nil)

	if got := len(b.ListConversationMessages(ctx, "broadcast", 50)); got != 2 {
		t.Errorf("an operator repeat was dropped: %d messages", got)
	}
}
