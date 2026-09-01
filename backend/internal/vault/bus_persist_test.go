package vault

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// fakePeerStore stands in for the database: enough to prove the bus writes
// through and reads back, without a live Postgres.
type fakePeerStore struct {
	mu       sync.Mutex
	rows     []protocol.PeerMessage
	secrets  map[string]protocol.SharedSecret
	sessions map[string]protocol.SharedSession
	failNew  bool
}

func (f *fakePeerStore) InsertPeerMessage(_ context.Context, m protocol.PeerMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNew {
		return errors.New("database is down")
	}
	f.rows = append(f.rows, m)
	return nil
}

func (f *fakePeerStore) UpsertSharedSecret(_ context.Context, s protocol.SharedSecret) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secrets == nil {
		f.secrets = map[string]protocol.SharedSecret{}
	}
	f.secrets[s.Key] = s
	return nil
}

func (f *fakePeerStore) ListSharedSecrets(_ context.Context) ([]protocol.SharedSecret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.SharedSecret, 0, len(f.secrets))
	for _, s := range f.secrets {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakePeerStore) DeleteSharedSecret(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.secrets, key)
	return nil
}

func (f *fakePeerStore) UpsertSharedSession(_ context.Context, s protocol.SharedSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]protocol.SharedSession{}
	}
	f.sessions[s.ID] = s
	return nil
}

func (f *fakePeerStore) ListSharedSessions(_ context.Context) ([]protocol.SharedSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.SharedSession, 0, len(f.sessions))
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakePeerStore) ListPeerMessages(_ context.Context, instanceID string, limit int) ([]protocol.PeerMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.PeerMessage, 0, len(f.rows))
	for _, m := range f.rows {
		if instanceID == "" || m.ToInstanceID == "broadcast" || m.ToInstanceID == instanceID || m.FromInstanceID == instanceID {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (f *fakePeerStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// The round trip that matters: what one process sent is what the next process
// sees after a restart.
func TestPeerMessagesSurviveARestart(t *testing.T) {
	bg := context.Background()
	db := &fakePeerStore{}

	before := NewBus()
	if err := before.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	before.SendMessage(bg, "inst-a", "Alpha", "broadcast", "report", "scan finished", map[string]any{"cves": 3})
	before.SendMessage(bg, "inst-a", "Alpha", "inst-b", "question", "can you review it", nil)

	if db.count() != 2 {
		t.Fatalf("store holds %d messages, want 2", db.count())
	}

	// A fresh process: new bus, same table.
	after := NewBus()
	if err := after.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	got := after.ListMessages(bg, "inst-b", 10)
	if len(got) != 2 {
		t.Fatalf("restarted bus sees %d messages, want 2", len(got))
	}
	// ListMessages is newest first.
	if got[0].Content != "can you review it" {
		t.Errorf("newest message = %q, want the direct question", got[0].Content)
	}
	if got[1].Kind != "report" || got[1].FromInstanceName != "Alpha" {
		t.Errorf("fields lost across the restart: %+v", got[1])
	}
	if got[1].Data["cves"] != float64(3) && got[1].Data["cves"] != 3 {
		t.Errorf("payload lost across the restart: %+v", got[1].Data)
	}
}

// Messages sent before a store is attached are kept, not dropped, and the
// loaded history sits in front of them.
func TestAttachStoreKeepsMessagesSentBeforeIt(t *testing.T) {
	bg := context.Background()
	db := &fakePeerStore{rows: []protocol.PeerMessage{
		{ID: "old-1", FromInstanceID: "inst-x", ToInstanceID: "broadcast", Content: "from the last run"},
	}}

	b := NewBus()
	b.SendMessage(bg, "inst-a", "Alpha", "broadcast", "message", "sent during boot", nil)
	if err := b.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}

	got := b.ListMessages(bg, "", 10)
	if len(got) != 2 {
		t.Fatalf("bus holds %d messages, want 2", len(got))
	}
	if got[0].Content != "sent during boot" {
		t.Errorf("the in-flight message should be the newest, got %q", got[0].Content)
	}
	if got[1].ID != "old-1" {
		t.Errorf("loaded history should sit behind it, got %q", got[1].ID)
	}
}

// A database that refuses the write must not lose the message or break
// delivery: durability is the only thing that degrades.
func TestSendSurvivesAFailedWrite(t *testing.T) {
	bg := context.Background()
	db := &fakePeerStore{failNew: true}

	b := NewBus()
	if err := b.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	msg := b.SendMessage(bg, "inst-a", "Alpha", "broadcast", "message", "still delivered", nil)
	if msg.ID == "" {
		t.Fatal("SendMessage returned an empty message")
	}
	if got := b.ListMessages(bg, "", 10); len(got) != 1 {
		t.Fatalf("message lost on a failed write: %d in the bus", len(got))
	}
}

// A bus with no store is the test and pre-boot configuration, and must behave
// exactly as it always did.
func TestBusWithoutAStoreStillWorks(t *testing.T) {
	bg := context.Background()
	b := NewBus()
	b.SendMessage(bg, "inst-a", "Alpha", "broadcast", "message", "hello", nil)
	if got := b.ListMessages(bg, "", 10); len(got) != 1 {
		t.Fatalf("in-memory bus holds %d messages, want 1", len(got))
	}
}

// IDs must stay unique inside one nanosecond tick, or a burst of messages
// collapses into one row on the ON CONFLICT DO NOTHING insert.
func TestPeerMessageIDsAreUniqueInABurst(t *testing.T) {
	bg := context.Background()
	b := NewBus()
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		id := b.SendMessage(bg, "inst-a", "Alpha", "broadcast", "message", "burst", nil).ID
		if seen[id] {
			t.Fatalf("duplicate peer message id %q", id)
		}
		seen[id] = true
	}
}
