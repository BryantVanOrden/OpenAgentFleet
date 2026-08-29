package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The list always offers the broadcast channel, even on a fleet that has never
// said anything — it is where an unaddressed message lands, so it cannot be
// absent.
func TestListConversationsAlwaysOffersBroadcast(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	rec := httptest.NewRecorder()
	(&Server{}).handleListConversations(rec,
		httptest.NewRequest(http.MethodGet, "/api/comms/conversations", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got []protocol.Conversation
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body)
	}
	if len(got) != 1 || got[0].ID != protocol.BroadcastConversationID {
		t.Fatalf("expected just the broadcast channel, got %+v", got)
	}
	// Members must marshal as a list, never null: a client that casts the
	// field would fail on null rather than show an empty thread.
	if !strings.Contains(rec.Body.String(), `"members":[]`) {
		t.Errorf("empty members marshalled as null: %s", rec.Body)
	}
}

func TestCreateConversationRejectsEmptyMembers(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	rec := httptest.NewRecorder()
	(&Server{}).handleCreateConversation(rec,
		httptest.NewRequest(http.MethodPost, "/api/comms/conversations",
			strings.NewReader(`{"title":"nobody","members":[]}`)))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("a thread with no members was accepted: status %d", rec.Code)
	}
}

func TestCreateConversationRejectsBadBody(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	rec := httptest.NewRecorder()
	(&Server{}).handleCreateConversation(rec,
		httptest.NewRequest(http.MethodPost, "/api/comms/conversations",
			strings.NewReader(`not json`)))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

// The broadcast channel must not be deletable through the API.
func TestDeleteBroadcastChannelIsRefused(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	req := httptest.NewRequest(http.MethodDelete,
		"/api/comms/conversations/broadcast", nil)
	req.SetPathValue("id", protocol.BroadcastConversationID)

	rec := httptest.NewRecorder()
	(&Server{}).handleDeleteConversation(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("the broadcast channel was deletable: status %d", rec.Code)
	}
}

func TestDeleteUnknownConversationIs404(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	req := httptest.NewRequest(http.MethodDelete,
		"/api/comms/conversations/conv-nope", nil)
	req.SetPathValue("id", "conv-nope")

	rec := httptest.NewRecorder()
	(&Server{}).handleDeleteConversation(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

// A thread's messages come back oldest-first, which is how a conversation
// reads.
func TestConversationMessagesReadOldestFirst(t *testing.T) {
	vault.GlobalBus = vault.NewBus()
	bus := vault.GlobalBus

	c := bus.CreateConversation(context.Background(), "pair", []string{"a", "b"}, "")
	for _, line := range []string{"first", "second", "third"} {
		bus.SendMessageIn(context.Background(), c.ID, "a", "Alpha", "b", "message", line, nil)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/comms/conversations/"+c.ID+"/messages", nil)
	req.SetPathValue("id", c.ID)

	rec := httptest.NewRecorder()
	(&Server{}).handleListConversationMessages(rec, req)

	var got []protocol.PeerMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].Content != "first" || got[2].Content != "third" {
		t.Errorf("thread is out of order: %s … %s", got[0].Content, got[2].Content)
	}
}

// Compacting a thread with nothing in it must fail rather than post an empty
// summary over it.
func TestCompactEmptyConversationIsRefused(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	req := httptest.NewRequest(http.MethodPost,
		"/api/comms/conversations/conv-empty/compact", strings.NewReader(`{}`))
	req.SetPathValue("id", "conv-empty")

	rec := httptest.NewRecorder()
	(&Server{}).handleCompactConversation(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

// Opening another everyone-channel needs no member list, and the thread that
// comes back must group with the built-in channel rather than beside it.
//
// The bug: creating a "new chat" from the broadcast sent the current roster as
// members, which made an ordinary group. It sorted into its own row instead of
// staying with the broadcast, and no bot added afterwards was in it.
func TestCreateBroadcastConversationNeedsNoMembers(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	rec := httptest.NewRecorder()
	(&Server{}).handleCreateConversation(rec,
		httptest.NewRequest(http.MethodPost, "/api/comms/conversations",
			strings.NewReader(`{"title":"Standup","kind":"broadcast","members":[]}`)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, rec.Body)
	}
	var got protocol.Conversation
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body)
	}
	if got.Kind != protocol.ConversationBroadcast {
		t.Errorf("kind = %q, want %q", got.Kind, protocol.ConversationBroadcast)
	}
	if got.Title != "Standup" {
		t.Errorf("title = %q, want Standup", got.Title)
	}
	// It is a second thread, not a rename of the built-in one.
	if got.ID == protocol.BroadcastConversationID {
		t.Error("the new chat replaced the built-in channel")
	}

	// Both channels are listed, and both read as everyone-channels.
	rec = httptest.NewRecorder()
	(&Server{}).handleListConversations(rec,
		httptest.NewRequest(http.MethodGet, "/api/comms/conversations", nil))
	var list []protocol.Conversation
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected the built-in channel and the new one, got %d", len(list))
	}
	for _, c := range list {
		if c.Kind != protocol.ConversationBroadcast {
			t.Errorf("%s: kind = %q, want %q", c.ID, c.Kind, protocol.ConversationBroadcast)
		}
	}
}

// A "kind" a client did not mean is ignored: the members decide.
func TestCreateConversationIgnoresUnknownKind(t *testing.T) {
	vault.GlobalBus = vault.NewBus()

	rec := httptest.NewRecorder()
	(&Server{}).handleCreateConversation(rec,
		httptest.NewRequest(http.MethodPost, "/api/comms/conversations",
			strings.NewReader(`{"title":"x","kind":"nonsense","members":[]}`)))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 -- an unknown kind must not skip the member check", rec.Code)
	}
}
