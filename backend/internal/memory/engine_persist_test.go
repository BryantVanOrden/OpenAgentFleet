package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// fakeMemStore stands in for the episodic_memories table.
type fakeMemStore struct {
	mu   sync.Mutex
	rows map[string]protocol.MemoryRecord
}

func newFakeMemStore() *fakeMemStore {
	return &fakeMemStore{rows: map[string]protocol.MemoryRecord{}}
}

func (f *fakeMemStore) UpsertMemory(_ context.Context, m protocol.MemoryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[m.ID] = m
	return nil
}

func (f *fakeMemStore) ListMemories(_ context.Context, namespace string, limit int) ([]protocol.MemoryRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.MemoryRecord, 0, len(f.rows))
	for _, m := range f.rows {
		if namespace == "" || m.Namespace == namespace {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMemStore) get(id string) (protocol.MemoryRecord, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.rows[id]
	return m, ok
}

// What an agent learned on one run has to be recallable on the next, which is
// the entire reason for the table.
func TestMemoriesSurviveARestart(t *testing.T) {
	bg := context.Background()
	db := newFakeMemStore()

	before := NewEngine()
	if err := before.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	if err := before.StoreMemory(bg, protocol.MemoryRecord{
		ID:        "mem-godot",
		Namespace: "gamedev",
		Title:     "Godot 4 Spatial Shader Optimization",
		Content:   "Enable shader caching and compile asynchronously to fix stutter.",
		Tags:      []string{"godot", "shader"},
	}); err != nil {
		t.Fatal(err)
	}

	saved, ok := db.get("mem-godot")
	if !ok {
		t.Fatal("StoreMemory did not write through to the store")
	}
	if len(saved.Embedding) != vectorDim {
		t.Errorf("persisted embedding has %d dimensions, want %d", len(saved.Embedding), vectorDim)
	}
	if len(saved.Tags) != 2 {
		t.Errorf("tags lost on write: %+v", saved.Tags)
	}

	after := NewEngine()
	if err := after.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	hits := after.Search(bg, "gamedev", "how do I fix godot shader stutter", 3)
	if len(hits) == 0 || hits[0].ID != "mem-godot" {
		t.Fatalf("restarted engine cannot recall the memory: %+v", hits)
	}
	if hits[0].Content == "" || hits[0].Title == "" {
		t.Errorf("record came back hollow: %+v", hits[0])
	}
}

// A row whose embedding column is empty (written by an older build, or by hand)
// must be re-embedded on load rather than becoming invisible to search.
func TestLoadRecomputesAMissingEmbedding(t *testing.T) {
	bg := context.Background()
	db := newFakeMemStore()
	db.rows["mem-crm"] = protocol.MemoryRecord{
		ID:        "mem-crm",
		Namespace: "crm",
		Title:     "CRM Webhook Configuration",
		Content:   "Lead webhooks post JSON to /api/v1/leads with a bearer token.",
		CreatedAt: time.Now().UTC(),
		// no Embedding
	}

	e := NewEngine()
	if err := e.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	hits := e.Search(bg, "crm", "crm lead webhook bearer token", 3)
	if len(hits) == 0 || hits[0].ID != "mem-crm" {
		t.Fatalf("a memory with no stored embedding is unsearchable: %+v", hits)
	}
	if len(hits[0].Embedding) != vectorDim {
		t.Errorf("embedding not recomputed on load: %d dimensions", len(hits[0].Embedding))
	}
}

// An embedding from a different vector scheme is as unusable as a missing one.
func TestLoadRecomputesAWrongSizedEmbedding(t *testing.T) {
	bg := context.Background()
	db := newFakeMemStore()
	db.rows["mem-old"] = protocol.MemoryRecord{
		ID:        "mem-old",
		Namespace: "global",
		Title:     "Deployment runbook",
		Content:   "Roll the orchestrator before the sandboxes.",
		Embedding: make([]float32, 64), // half the current dimension
	}

	e := NewEngine()
	if err := e.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	hits := e.Search(bg, "global", "deployment runbook", 3)
	if len(hits) == 0 {
		t.Fatal("a record with a stale embedding must be re-embedded, not dropped")
	}
	if len(hits[0].Embedding) != vectorDim {
		t.Errorf("embedding = %d dimensions, want %d", len(hits[0].Embedding), vectorDim)
	}
}

// Anything stored before the attach is newer than the table and must not be
// clobbered by the load.
func TestAttachDoesNotOverwriteLiveMemories(t *testing.T) {
	bg := context.Background()
	db := newFakeMemStore()
	db.rows["mem-x"] = protocol.MemoryRecord{
		ID: "mem-x", Namespace: "global", Title: "stale", Content: "the old text",
	}

	e := NewEngine()
	if err := e.StoreMemory(bg, protocol.MemoryRecord{
		ID: "mem-x", Namespace: "global", Title: "fresh", Content: "the new text",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.AttachStore(bg, db, nil); err != nil {
		t.Fatal(err)
	}
	hits := e.Search(bg, "global", "the new text", 3)
	if len(hits) == 0 || hits[0].Title != "fresh" {
		t.Fatalf("the load overwrote a live memory: %+v", hits)
	}
}

// The engine with no store is what every existing test and the pre-boot
// singleton use.
func TestEngineWithoutAStoreStillWorks(t *testing.T) {
	bg := context.Background()
	e := NewEngine()
	if err := e.StoreMemory(bg, protocol.MemoryRecord{Title: "t", Content: "an isolated note"}); err != nil {
		t.Fatal(err)
	}
	if hits := e.Search(bg, "", "isolated note", 3); len(hits) != 1 {
		t.Fatalf("in-memory engine returned %d hits, want 1", len(hits))
	}
}
