package store

import (
	"context"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func TestConversationRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	c := protocol.Conversation{
		ID:        "conv-test-1",
		Kind:      protocol.ConversationPair,
		Title:     "Scouting",
		Members:   []string{"inst-a", "inst-b"},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	t.Cleanup(func() { _ = s.DeleteConversation(ctx, c.ID) })

	if err := s.UpsertConversation(ctx, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got := findConversation(t, s, c.ID)
	if got.Title != "Scouting" || got.Kind != protocol.ConversationPair {
		t.Errorf("round trip changed the thread: %+v", got)
	}
	if len(got.Members) != 2 {
		t.Fatalf("members lost: %v", got.Members)
	}

	// Members are replaced, not merged: a stale row would widen who can read
	// the thread.
	c.Members = []string{"inst-a", "inst-c"}
	c.Title = "Renamed"
	if err := s.UpsertConversation(ctx, c); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got = findConversation(t, s, c.ID)
	if got.Title != "Renamed" {
		t.Errorf("title not updated: %q", got.Title)
	}
	if len(got.Members) != 2 || got.Members[0] != "inst-a" || got.Members[1] != "inst-c" {
		t.Errorf("members merged instead of replaced: %v", got.Members)
	}
}

// Deleting a thread must unfile its messages, never delete them.
func TestDeleteConversationKeepsItsMessages(t *testing.T) {
	s, ctx := testStore(t)

	c := protocol.Conversation{
		ID: "conv-test-2", Kind: protocol.ConversationPair,
		Members: []string{"inst-a", "inst-b"}, CreatedAt: time.Now().UTC(),
	}
	if err := s.UpsertConversation(ctx, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	m := protocol.PeerMessage{
		ID: "peer-conv-test-2", ConversationID: c.ID,
		FromInstanceID: "inst-a", FromInstanceName: "Alpha",
		ToInstanceID: "inst-b", Kind: "message", Content: "hello",
		CreatedAt: time.Now().UTC(),
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM peer_messages WHERE id=$1`, m.ID)
	})
	if err := s.InsertPeerMessage(ctx, m); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if err := s.DeleteConversation(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	msgs, err := s.ListPeerMessages(ctx, "inst-a", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, got := range msgs {
		if got.ID != m.ID {
			continue
		}
		found = true
		if got.ConversationID != "" {
			t.Errorf("message still filed in the deleted thread: %q", got.ConversationID)
		}
	}
	if !found {
		t.Error("deleting the thread destroyed its message")
	}
}

// Compaction marks history through a point, and leaves later messages alone.
func TestCompactConversationMarksThroughAPoint(t *testing.T) {
	s, ctx := testStore(t)

	convID := "conv-test-3"
	base := time.Now().UTC()
	ids := []string{"peer-c3-1", "peer-c3-2", "peer-c3-3"}
	for i, id := range ids {
		m := protocol.PeerMessage{
			ID: id, ConversationID: convID,
			FromInstanceID: "inst-a", FromInstanceName: "Alpha",
			ToInstanceID: "inst-b", Kind: "message", Content: id,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := s.InsertPeerMessage(ctx, m); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM peer_messages WHERE conversation_id=$1`, convID)
	})

	// Compact through the second message; the third must stay live.
	if err := s.CompactConversation(ctx, convID, ids[1]); err != nil {
		t.Fatalf("compact: %v", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id,compacted FROM peer_messages WHERE conversation_id=$1 ORDER BY created_at`, convID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	want := map[string]bool{ids[0]: true, ids[1]: true, ids[2]: false}
	for rows.Next() {
		var id string
		var compacted bool
		if err := rows.Scan(&id, &compacted); err != nil {
			t.Fatal(err)
		}
		if compacted != want[id] {
			t.Errorf("%s: compacted=%v, want %v", id, compacted, want[id])
		}
	}
}

func findConversation(t *testing.T, s *Store, id string) protocol.Conversation {
	t.Helper()
	all, err := s.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	for _, c := range all {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("conversation %s not found", id)
	return protocol.Conversation{}
}
