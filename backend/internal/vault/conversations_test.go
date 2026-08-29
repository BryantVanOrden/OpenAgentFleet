package vault

import (
	"context"
	"errors"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The thread between two agents must be one thread regardless of who opened it
// or which way the first message went.
func TestPairThreadIsDirectionIndependent(t *testing.T) {
	if ConversationIDFor([]string{"a", "b"}) != ConversationIDFor([]string{"b", "a"}) {
		t.Fatal("A→B and B→A resolved to different threads")
	}
	if ConversationIDFor([]string{"a", "b"}) == ConversationIDFor([]string{"a", "c"}) {
		t.Fatal("different pairs collided on one thread")
	}
}

func TestKindForClassifiesMembers(t *testing.T) {
	cases := []struct {
		members []string
		want    string
	}{
		{[]string{protocol.OperatorMemberID, "a"}, protocol.ConversationDirect},
		{[]string{"a", "b"}, protocol.ConversationPair},
		{[]string{protocol.OperatorMemberID, "a", "b"}, protocol.ConversationGroup},
		{[]string{"a", "b", "c"}, protocol.ConversationGroup},
	}
	for _, c := range cases {
		if got := KindFor(c.members); got != c.want {
			t.Errorf("KindFor(%v) = %q, want %q", c.members, got, c.want)
		}
	}
}

// Creating the same two-party thread twice must return the existing one, or
// the pair's history splits across duplicates.
func TestCreateConversationIsFindOrCreateForPairs(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	first := b.CreateConversation(ctx, "Scouting", []string{"a", "b"})
	second := b.CreateConversation(ctx, "", []string{"b", "a"})

	if first.ID != second.ID {
		t.Fatalf("pair thread duplicated: %s vs %s", first.ID, second.ID)
	}
	if second.Title != "Scouting" {
		t.Errorf("re-opening lost the title: %q", second.Title)
	}
	if got := len(b.ListConversations(ctx)); got != 2 { // broadcast + the pair
		t.Errorf("expected broadcast plus one thread, got %d", got)
	}
}

// A message with no conversation is filed by its recipient, so agents that
// know nothing about conversations still land in the right thread.
func TestUnfiledMessagesArePlacedByRecipient(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	b.SendMessage(ctx, "a", "Alpha", "b", "message", "are you free?", nil)
	b.SendMessage(ctx, "", "Operator", "broadcast", "message", "status?", nil)

	pair := b.ListConversationMessages(ctx, ConversationIDFor([]string{"a", "b"}), 0)
	if len(pair) != 1 || pair[0].Content != "are you free?" {
		t.Fatalf("agent-to-agent message not filed in the pair thread: %+v", pair)
	}

	bc := b.ListConversationMessages(ctx, protocol.BroadcastConversationID, 0)
	if len(bc) != 1 || bc[0].Content != "status?" {
		t.Fatalf("broadcast not filed in the broadcast channel: %+v", bc)
	}
}

// Deleting a thread must not destroy what was said in it.
func TestDeleteConversationKeepsMessages(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "temp", []string{"a", "b"})
	b.SendMessageIn(ctx, c.ID, "a", "Alpha", "b", "message", "hello", nil)

	if !b.DeleteConversation(ctx, c.ID) {
		t.Fatal("delete reported the thread missing")
	}
	if b.DeleteConversation(ctx, c.ID) {
		t.Error("deleting twice reported success the second time")
	}
	if got := len(b.ListMessages(ctx, "", 10)); got != 1 {
		t.Errorf("message lost with the thread: %d remain", got)
	}
}

func TestBroadcastChannelCannotBeDeleted(t *testing.T) {
	b := NewBus()
	if b.DeleteConversation(context.Background(), protocol.BroadcastConversationID) {
		t.Fatal("the broadcast channel was deleted")
	}
}

// Compaction replaces history with a summary: the old messages stop being
// listed and stop being replayed to agents, and the summary takes their place.
func TestCompactConversationReplacesHistory(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "long", []string{"a", "b"})
	for _, line := range []string{"one", "two", "three"} {
		b.SendMessageIn(ctx, c.ID, "a", "Alpha", "b", "message", line, nil)
	}

	summary, err := b.CompactConversation(ctx, c.ID,
		func(context.Context, string) (string, error) { return "they agreed on a plan", nil })
	if err != nil {
		t.Fatalf("compact: %v", err)
	}

	left := b.ListConversationMessages(ctx, c.ID, 0)
	if len(left) != 1 {
		t.Fatalf("expected only the summary, got %d messages", len(left))
	}
	if left[0].ID != summary.ID || left[0].Content != "they agreed on a plan" {
		t.Errorf("the remaining message is not the summary: %+v", left[0])
	}
	if got := len(b.ListMessages(ctx, "", 50)); got != 1 {
		t.Errorf("compacted messages are still replayed to agents: %d", got)
	}
}

// A failing summariser must leave the thread exactly as it was. Losing history
// because a model call failed is the one outcome compaction must never have.
func TestCompactLeavesThreadIntactOnFailure(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "long", []string{"a", "b"})
	b.SendMessageIn(ctx, c.ID, "a", "Alpha", "b", "message", "one", nil)
	b.SendMessageIn(ctx, c.ID, "b", "Beta", "a", "message", "two", nil)

	if _, err := b.CompactConversation(ctx, c.ID,
		func(context.Context, string) (string, error) { return "", errors.New("model down") }); err == nil {
		t.Fatal("expected an error when the summariser fails")
	}
	if got := len(b.ListConversationMessages(ctx, c.ID, 0)); got != 2 {
		t.Errorf("thread was damaged by a failed compaction: %d messages left", got)
	}

	// An empty summary is a failure too: it would blank the thread.
	if _, err := b.CompactConversation(ctx, c.ID,
		func(context.Context, string) (string, error) { return "   ", nil }); err == nil {
		t.Fatal("expected an error when the summariser returns nothing")
	}
	if got := len(b.ListConversationMessages(ctx, c.ID, 0)); got != 2 {
		t.Errorf("empty summary damaged the thread: %d messages left", got)
	}
}

// The bug this pins: an operator question inside a pair thread has no instance
// ID to address a reply to, so the reply fell back to broadcast and the whole
// exchange escaped into the fleet channel.
func TestOperatorQuestionInPairThreadKeepsRepliesInThread(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "pair", []string{"a", "b"})

	// A reply from "a" must be addressed to "b" — the other agent in the room.
	other, ok := b.OtherAgentMember(c.ID, "a")
	if !ok || other != "b" {
		t.Fatalf("OtherAgentMember(%s, a) = %q, %v; want b, true", c.ID, other, ok)
	}

	// And the broadcast channel has no other member to pick, so a reply there
	// falls back to the fleet as it always did.
	if _, ok := b.OtherAgentMember(protocol.BroadcastConversationID, "a"); ok {
		t.Error("the broadcast channel resolved a single recipient")
	}
	if _, ok := b.OtherAgentMember("", "a"); ok {
		t.Error("an unfiled message resolved a single recipient")
	}
}

// A group thread has no single recipient, so nothing is addressed on its
// behalf — a question meant for four agents must not be handed to one.
func TestGroupThreadHasNoSingleRecipient(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "group", []string{"a", "b", "c"})
	if _, ok := b.OtherAgentMember(c.ID, "a"); ok {
		t.Error("a three-agent thread resolved one recipient")
	}
}

// The operator writing into a thread between two other agents is not a member
// of it, and must not have their message addressed to an arbitrary half of it.
func TestNonMemberGetsNoRecipient(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "pair", []string{"a", "b"})
	if b.IsMember(c.ID, protocol.OperatorMemberID) {
		t.Error("the operator counted as a member of an agents-only thread")
	}
	if !b.IsMember(c.ID, "a") || !b.IsMember(c.ID, "b") {
		t.Error("an actual member was not recognised")
	}
}
