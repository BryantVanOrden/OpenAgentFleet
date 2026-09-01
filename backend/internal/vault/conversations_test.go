package vault

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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

// Asking for a new chat with people you already have a chat with must give you
// a new chat. This was find-or-create, which meant the second request renamed
// the first thread and handed it back — so you could not have two
// conversations with the same bot, and trying appeared to corrupt the one you
// had.
func TestCreateConversationAlwaysCreates(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	first := b.CreateConversation(ctx, "Scouting", []string{"a", "b"}, "")
	second := b.CreateConversation(ctx, "Release checks", []string{"a", "b"}, "")

	if first.ID == second.ID {
		t.Fatal("a second chat with the same pair reused the first thread")
	}
	if first.Title != "Scouting" {
		t.Errorf("the first thread was renamed to %q", first.Title)
	}
	// And the original is still there, under its own name.
	var found bool
	for _, c := range b.ListConversations(ctx) {
		if c.ID == first.ID {
			found = true
			if c.Title != "Scouting" {
				t.Errorf("stored title is %q", c.Title)
			}
		}
	}
	if !found {
		t.Error("the first thread disappeared")
	}
}

// The fleet's own traffic still has one stable home per pair, so agents
// talking to each other do not mint a thread every time they speak.
func TestCanonicalThreadIsStable(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	one := b.CanonicalThread(ctx, []string{"a", "b"})
	two := b.CanonicalThread(ctx, []string{"b", "a"})
	if one.ID != two.ID {
		t.Fatal("the canonical thread differs by member order")
	}
	// An operator-created thread between the same pair is a separate thing.
	named := b.CreateConversation(ctx, "Side channel", []string{"a", "b"}, "")
	if named.ID == one.ID {
		t.Error("a named thread collided with the canonical one")
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
// Deleting a thread takes its messages with it, and says so only once.
//
// This used to assert the opposite -- that the messages were kept -- on the
// theory that closing a thread should not destroy the record. What actually
// happened is that they were unfiled, and an unfiled message is not kept, it
// is moved: it reappears in whatever channel takes unaddressed traffic. So
// deleting a chat poured its contents into a different one.
func TestDeleteConversationRemovesItsMessages(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "temp", []string{"a", "b"}, "")
	b.SendMessageIn(ctx, c.ID, "a", "Alpha", "b", "message", "hello", nil)

	if !b.DeleteConversation(ctx, c.ID) {
		t.Fatal("delete reported the thread missing")
	}
	if b.DeleteConversation(ctx, c.ID) {
		t.Error("deleting twice reported success the second time")
	}
	if got := len(b.ListMessages(ctx, "", 10)); got != 0 {
		t.Errorf("%d message(s) outlived the thread they were in", got)
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

	c := b.CreateConversation(ctx, "long", []string{"a", "b"}, "")
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

	c := b.CreateConversation(ctx, "long", []string{"a", "b"}, "")
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

	c := b.CreateConversation(ctx, "pair", []string{"a", "b"}, "")

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

	c := b.CreateConversation(ctx, "group", []string{"a", "b", "c"}, "")
	if _, ok := b.OtherAgentMember(c.ID, "a"); ok {
		t.Error("a three-agent thread resolved one recipient")
	}
}

// The operator writing into a thread between two other agents is not a member
// of it, and must not have their message addressed to an arbitrary half of it.
func TestNonMemberGetsNoRecipient(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "pair", []string{"a", "b"}, "")
	if b.IsMember(c.ID, protocol.OperatorMemberID) {
		t.Error("the operator counted as a member of an agents-only thread")
	}
	if !b.IsMember(c.ID, "a") || !b.IsMember(c.ID, "b") {
		t.Error("an actual member was not recognised")
	}
}

// The one channel every fleet has should not be the only one that cannot be
// labelled. It stays undeletable — it is where unaddressed messages land — but
// a name is just a label.
func TestBroadcastChannelCanBeRenamed(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	if _, ok := b.RenameConversation(ctx, protocol.BroadcastConversationID, "General"); !ok {
		t.Fatal("the broadcast channel refused a rename")
	}
	var seen bool
	for _, c := range b.ListConversations(ctx) {
		if c.ID == protocol.BroadcastConversationID {
			seen = true
			if c.Title != "General" {
				t.Errorf("title is %q, want General", c.Title)
			}
		}
	}
	if !seen {
		t.Error("the broadcast channel vanished after renaming")
	}
	// Still undeletable.
	if b.DeleteConversation(ctx, protocol.BroadcastConversationID) {
		t.Error("the broadcast channel was deleted")
	}
}

// An unfiled agent-to-agent message must produce a thread that actually shows
// up, or two agents can talk at length and appear silent.
func TestAutoFiledMessagesGetAVisibleThread(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	b.SendMessage(ctx, "a", "Alpha", "b", "message", "are you free?", nil)

	var found bool
	for _, c := range b.ListConversations(ctx) {
		if c.ID == ConversationIDFor([]string{"a", "b"}) {
			found = true
			if c.MessageCount != 1 {
				t.Errorf("thread shows %d messages", c.MessageCount)
			}
		}
	}
	if !found {
		t.Fatal("the pair's own thread is not listed, so their exchange is invisible")
	}
}

// Renaming the broadcast channel gives it a stored row, which must not then be
// listed alongside the implicit one.
func TestBroadcastIsListedOnce(t *testing.T) {
	b := NewBus()
	ctx := context.Background()
	b.RenameConversation(ctx, protocol.BroadcastConversationID, "General")

	seen := 0
	for _, c := range b.ListConversations(ctx) {
		if c.ID == protocol.BroadcastConversationID {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the broadcast channel appears %d times", seen)
	}
}

// A new chat opened from an everyone-channel must itself be an
// everyone-channel: heard by the whole fleet, including bots provisioned
// after it was made. Sending the current roster as a member list instead
// produced a thread that grouped apart from the broadcast in the UI and
// silently excluded every later bot.
func TestBroadcastKindConversationIncludesTheWholeFleet(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "Standup", nil, protocol.ConversationBroadcast)
	if c.Kind != protocol.ConversationBroadcast {
		t.Fatalf("kind = %q, want %q", c.Kind, protocol.ConversationBroadcast)
	}

	// A bot that did not exist when the thread was opened is still in it.
	if !b.IsMember(c.ID, "instance-provisioned-later") {
		t.Error("a later bot is not a member of an everyone-channel")
	}
	if !b.IsMember(c.ID, protocol.OperatorMemberID) {
		t.Error("the operator is not a member of an everyone-channel")
	}

	// Addressed to the room, so a reply is not pointed at one agent.
	if other, ok := b.OtherAgentMember(c.ID, protocol.OperatorMemberID); ok {
		t.Errorf("reply addressed to %q, want the whole room", other)
	}

	// Only the kind hint does this; a plain group is still a group.
	g := b.CreateConversation(ctx, "Two of them", []string{"a", "b"}, "")
	if g.Kind != protocol.ConversationGroup && g.Kind != protocol.ConversationPair {
		t.Errorf("kind = %q, want a member-derived kind", g.Kind)
	}
	if b.IsMember(g.ID, "someone-else") {
		t.Error("a non-member is in an ordinary thread")
	}
}

// The built-in everyone-channel can be deleted once another one exists.
//
// It used to be refused outright, on the grounds that unaddressed messages
// would have nowhere to land. That was true while it was the only
// everyone-channel and stopped being true when operators could open others --
// at which point the refusal was just a refusal.
func TestBuiltInBroadcastGoesOnceAnotherEveryoneChannelExists(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	// While it is the only one, it stays.
	if b.DeleteConversation(ctx, protocol.BroadcastConversationID) {
		t.Fatal("the last everyone-channel was deleted")
	}
	if b.DefaultChannel() != protocol.BroadcastConversationID {
		t.Error("unaddressed traffic lost its landing place")
	}

	// Open another, and now it can go.
	other := b.CreateConversation(ctx, "Standup", nil, protocol.ConversationBroadcast)
	if !b.DeleteConversation(ctx, protocol.BroadcastConversationID) {
		t.Fatal("the built-in channel was refused although another exists")
	}

	// It is gone from the listing...
	for _, c := range b.ListConversations(ctx) {
		if c.ID == protocol.BroadcastConversationID {
			t.Error("the deleted channel is still listed")
		}
	}
	// ...and unaddressed traffic goes to the survivor rather than to a
	// conversation id nothing lists.
	if got := b.DefaultChannel(); got != other.ID {
		t.Errorf("default channel = %q, want the surviving channel %q", got, other.ID)
	}

	// The survivor is now the last one, so it is refused in turn.
	if b.DeleteConversation(ctx, other.ID) {
		t.Error("the last remaining everyone-channel was deleted")
	}
}

// A message with nowhere else to go lands in the surviving channel.
func TestUnaddressedMessagesFollowTheSurvivingChannel(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	other := b.CreateConversation(ctx, "Standup", nil, protocol.ConversationBroadcast)
	if !b.DeleteConversation(ctx, protocol.BroadcastConversationID) {
		t.Fatal("could not delete the built-in channel")
	}

	msg := b.SendMessage(ctx, "bot-1", "Alpha", "", "message", "anyone about?", nil)
	if msg.ConversationID != other.ID {
		t.Errorf("message filed to %q, want the surviving channel %q",
			msg.ConversationID, other.ID)
	}
}

// Deleting a chat deletes what was said in it.
//
// Messages used to be unfiled rather than removed, on the theory that the
// record should survive the thread. But an unfiled message is not kept, it is
// moved: it has nowhere to belong, so it surfaces in whatever channel takes
// unaddressed traffic. Deleting a chat quietly poured its contents into
// another one, which is how a deleted thread's messages turned up in a
// different broadcast channel.
func TestDeletingAChatDeletesItsMessages(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	doomed := b.CreateConversation(ctx, "Scratch", []string{"a", "b"}, "")
	keep := b.CreateConversation(ctx, "Keep", []string{"a", "c"}, "")

	b.SendMessageIn(ctx, doomed.ID, "a", "Alpha", "b", "message", "in the doomed one", nil)
	b.SendMessageIn(ctx, doomed.ID, "a", "Alpha", "b", "message", "also doomed", nil)
	b.SendMessageIn(ctx, keep.ID, "a", "Alpha", "c", "message", "survives", nil)

	if !b.DeleteConversation(ctx, doomed.ID) {
		t.Fatal("the thread was not deleted")
	}

	// Nothing from the deleted thread anywhere, under any conversation.
	for _, m := range b.ListMessages(ctx, "", 100) {
		if strings.Contains(m.Content, "doomed") {
			t.Errorf("a deleted thread's message survived in conversation %q: %q",
				m.ConversationID, m.Content)
		}
	}

	// And the thread that was not deleted is untouched.
	left := b.ListConversationMessages(ctx, keep.ID, 100)
	if len(left) != 1 || left[0].Content != "survives" {
		t.Errorf("deleting one thread disturbed another: %+v", left)
	}
}

// An answer that arrives after its thread was deleted goes where unaddressed
// messages go, not into a void.
//
// Agents take a minute or two to reply and operators do not wait. Filing the
// answer to a conversation that no longer exists wrote it somewhere listed
// nowhere -- found only by reading the database.
func TestReplyToADeletedThreadLandsSomewhereVisible(t *testing.T) {
	b := NewBus()
	ctx := context.Background()

	c := b.CreateConversation(ctx, "short-lived", []string{"a", "b"}, "")
	if !b.DeleteConversation(ctx, c.ID) {
		t.Fatal("could not delete the thread")
	}

	// The agent answers the question it was asked before the thread went.
	msg := b.SendMessageIn(ctx, c.ID, "a", "Alpha", "b", "reply", "late answer", nil)

	if msg.ConversationID == c.ID {
		t.Error("the reply was filed to a conversation that no longer exists")
	}
	if msg.ConversationID != protocol.BroadcastConversationID {
		t.Errorf("reply landed in %q, want the default channel", msg.ConversationID)
	}
	// And it is actually readable there.
	found := false
	for _, m := range b.ListConversationMessages(ctx, protocol.BroadcastConversationID, 50) {
		if m.Content == "late answer" {
			found = true
		}
	}
	if !found {
		t.Error("the late reply is not visible in the channel it was filed to")
	}
}
