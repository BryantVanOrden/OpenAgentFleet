package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/memory"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func requestAs(email, id string) *http.Request {
	c := &claims{Email: email}
	c.Subject = id
	r := httptest.NewRequest(http.MethodPost, "/api/chat/x", nil)
	return r.WithContext(context.WithValue(r.Context(), userKey, c))
}

// The display name is the local part: an agent addressing a colleague should
// say "alex", not read out an email address.
func TestSpeakerNameIsTheLocalPart(t *testing.T) {
	s := &Server{}
	got := s.speakerOf(requestAs("alex@example.com", "u1"))
	if got.Name != "alex" {
		t.Errorf("name = %q, want alex", got.Name)
	}
	if got.ID != "u1" {
		t.Errorf("id = %q", got.ID)
	}
}

// An unauthenticated request still produces something addressable rather than
// an empty name that would render as "You are speaking with .".
func TestSpeakerFallsBackToOperator(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/chat/x", nil)
	if got := s.speakerOf(r); got.Name != "Operator" {
		t.Errorf("name = %q, want Operator", got.Name)
	}
}

// The system prompt must actually name the person, or none of the attribution
// reaches the model.
func TestPromptNamesTheSpeaker(t *testing.T) {
	s := &Server{}
	inst := &protocol.Instance{ID: "bot-1", Name: "Builder"}

	out := s.aboutSpeaker(context.Background(), inst, speaker{ID: "u1", Name: "alex"})
	if !strings.Contains(out, "You are speaking with alex.") {
		t.Fatalf("prompt does not name the speaker: %q", out)
	}
	if !strings.Contains(out, "no notes about them yet") {
		t.Errorf("expected the no-notes case: %q", out)
	}
}

// Notes about a person come back for that person, and not for anyone else.
func TestPromptCarriesNotesAboutThatPersonOnly(t *testing.T) {
	memory.GlobalEngine = memory.NewEngine()
	ctx := context.Background()
	ns := memory.BotNamespace("bot-1")

	_ = memory.GlobalEngine.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: ns, Title: "prefers terse answers",
		Content: "alex asked for shorter replies", AboutUserID: "u1",
	})
	_ = memory.GlobalEngine.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: ns, Title: "owns billing",
		Content: "sam looks after billing", AboutUserID: "u2",
	})

	s := &Server{}
	inst := &protocol.Instance{ID: "bot-1", Name: "Builder"}

	alex := s.aboutSpeaker(ctx, inst, speaker{ID: "u1", Name: "alex"})
	if !strings.Contains(alex, "prefers terse answers") {
		t.Errorf("alex's own note is missing: %q", alex)
	}
	if strings.Contains(alex, "owns billing") {
		t.Errorf("another person's note leaked into alex's prompt: %q", alex)
	}

	sam := s.aboutSpeaker(ctx, inst, speaker{ID: "u2", Name: "sam"})
	if !strings.Contains(sam, "owns billing") || strings.Contains(sam, "terse") {
		t.Errorf("sam's prompt is wrong: %q", sam)
	}
}

// What one agent learned about a colleague is not every agent's to know.
func TestNotesAboutPeopleAreScopedToTheBot(t *testing.T) {
	memory.GlobalEngine = memory.NewEngine()
	ctx := context.Background()

	_ = memory.GlobalEngine.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: memory.BotNamespace("bot-1"), Title: "note",
		Content: "something private", AboutUserID: "u1",
	})

	other := memory.GlobalEngine.AboutUser(ctx, memory.BotNamespace("bot-2"), "u1", 5)
	if len(other) != 0 {
		t.Errorf("another bot read %d notes about the user", len(other))
	}
	own := memory.GlobalEngine.AboutUser(ctx, memory.BotNamespace("bot-1"), "u1", 5)
	if len(own) != 1 {
		t.Errorf("the bot cannot read its own note: %d", len(own))
	}
}

// No user, no notes — asking for notes about nobody must not return everyone's.
func TestNoUserReturnsNoNotes(t *testing.T) {
	memory.GlobalEngine = memory.NewEngine()
	_ = memory.GlobalEngine.StoreMemory(context.Background(), protocol.MemoryRecord{
		Namespace: memory.BotNamespace("bot-1"), Title: "n", Content: "c", AboutUserID: "u1",
	})
	if got := memory.GlobalEngine.AboutUser(context.Background(),
		memory.BotNamespace("bot-1"), "", 5); len(got) != 0 {
		t.Errorf("an empty user id returned %d notes", len(got))
	}
}
