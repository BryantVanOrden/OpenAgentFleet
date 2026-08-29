package store

import (
	"context"
	"testing"
)

// The point of a new chat: its history is its own. This history is replayed
// into the model, so a leak here means a chat you deliberately started fresh
// still carries every earlier conversation.
func TestChatSessionsAreIsolated(t *testing.T) {
	s, ctx := testStore(t)
	inst := seedInstanceForChat(t, s, ctx)

	a, err := s.CreateChatSession(ctx, inst, "First")
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := s.CreateChatSession(ctx, inst, "Second")
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	mustAppend(t, s, ctx, &ChatMessage{InstanceID: inst, Role: "user", Body: "in A", SessionID: a.ID})
	mustAppend(t, s, ctx, &ChatMessage{InstanceID: inst, Role: "user", Body: "in B", SessionID: b.ID})
	mustAppend(t, s, ctx, &ChatMessage{InstanceID: inst, Role: "user", Body: "in the old chat"})

	inA, err := s.ListChatSession(ctx, inst, a.ID, 50)
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(inA) != 1 || inA[0].Body != "in A" {
		t.Fatalf("chat A saw %d messages: %+v", len(inA), inA)
	}

	// The original chat holds only what predates sessions.
	old, err := s.ListChatSession(ctx, inst, DefaultChatSessionID, 50)
	if err != nil {
		t.Fatalf("list default: %v", err)
	}
	if len(old) != 1 || old[0].Body != "in the old chat" {
		t.Fatalf("the earlier chat saw %d messages: %+v", len(old), old)
	}

	// ListChat with no session is the earlier chat, so older callers are
	// unchanged rather than silently reading everything.
	legacy, err := s.ListChat(ctx, inst, 50)
	if err != nil {
		t.Fatalf("legacy list: %v", err)
	}
	if len(legacy) != 1 {
		t.Errorf("unscoped read returned %d messages, want 1", len(legacy))
	}
}

// Deleting a chat takes its messages with it — otherwise the chat you deleted
// keeps shaping what the agent says next.
func TestDeleteChatSessionRemovesItsMessages(t *testing.T) {
	s, ctx := testStore(t)
	inst := seedInstanceForChat(t, s, ctx)

	a, err := s.CreateChatSession(ctx, inst, "Doomed")
	if err != nil {
		t.Fatal(err)
	}
	keep, err := s.CreateChatSession(ctx, inst, "Kept")
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, ctx, &ChatMessage{InstanceID: inst, Role: "user", Body: "goes away", SessionID: a.ID})
	mustAppend(t, s, ctx, &ChatMessage{InstanceID: inst, Role: "user", Body: "stays", SessionID: keep.ID})

	if err := s.DeleteChatSession(ctx, inst, a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if got, _ := s.ListChatSession(ctx, inst, a.ID, 50); len(got) != 0 {
		t.Errorf("deleted chat still has %d messages", len(got))
	}
	if got, _ := s.ListChatSession(ctx, inst, keep.ID, 50); len(got) != 1 {
		t.Errorf("deleting one chat disturbed another: %d messages left", len(got))
	}

	sessions, err := s.ListChatSessions(ctx, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range sessions {
		if c.ID == a.ID {
			t.Error("deleted chat is still listed")
		}
	}
}

// Pinned chats sort above the rest regardless of when anyone last spoke.
func TestPinnedChatsSortFirst(t *testing.T) {
	s, ctx := testStore(t)
	inst := seedInstanceForChat(t, s, ctx)

	older, err := s.CreateChatSession(ctx, inst, "Older")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateChatSession(ctx, inst, "Newer")
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, s, ctx, &ChatMessage{InstanceID: inst, Role: "user", Body: "hi", SessionID: newer.ID})

	// Newer has the most recent activity, so it leads until Older is pinned.
	sessions, err := s.ListChatSessions(ctx, inst)
	if err != nil {
		t.Fatal(err)
	}
	if sessions[0].ID != newer.ID {
		t.Fatalf("expected the most recent chat first, got %q", sessions[0].Title)
	}

	if err := s.PinChatSession(ctx, inst, older.ID, true); err != nil {
		t.Fatal(err)
	}
	sessions, err = s.ListChatSessions(ctx, inst)
	if err != nil {
		t.Fatal(err)
	}
	if sessions[0].ID != older.ID || !sessions[0].Pinned {
		t.Errorf("pinned chat did not sort first: %+v", sessions[0])
	}
}

func TestRenameChatSession(t *testing.T) {
	s, ctx := testStore(t)
	inst := seedInstanceForChat(t, s, ctx)

	c, err := s.CreateChatSession(ctx, inst, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameChatSession(ctx, inst, c.ID, "Deploy notes"); err != nil {
		t.Fatal(err)
	}

	sessions, _ := s.ListChatSessions(ctx, inst)
	for _, got := range sessions {
		if got.ID == c.ID && got.Title != "Deploy notes" {
			t.Errorf("title is %q, want %q", got.Title, "Deploy notes")
		}
	}
}

// A bot with no history must not show a phantom "earlier chat".
func TestEmptyDefaultChatIsNotListed(t *testing.T) {
	s, ctx := testStore(t)
	inst := seedInstanceForChat(t, s, ctx)

	sessions, err := s.ListChatSessions(ctx, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range sessions {
		if c.ID == DefaultChatSessionID {
			t.Error("an empty earlier chat was listed")
		}
	}
}

// seedInstanceForChat returns an instance ID unique to this test. chat_messages
// carries no foreign key, so no row is needed -- and a per-test ID keeps these
// from colliding when run against a database that already holds real chats.
func seedInstanceForChat(t *testing.T, s *Store, ctx context.Context) string {
	t.Helper()
	id := "inst-chat-" + NewID()
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM chat_messages WHERE instance_id=$1`, id)
		_, _ = s.pool.Exec(ctx, `DELETE FROM chat_sessions WHERE instance_id=$1`, id)
	})
	return id
}

func mustAppend(t *testing.T, s *Store, ctx context.Context, m *ChatMessage) {
	t.Helper()
	if err := s.AppendChat(ctx, m); err != nil {
		t.Fatalf("append %q: %v", m.Body, err)
	}
}
