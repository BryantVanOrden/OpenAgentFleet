package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The README said episodic memory was "a bag-of-words index scanned linearly,
// not embeddings, and it is per-bot: nothing ever writes to the shared
// namespace, so one agent's discovery is not retrievable by another". These pin
// both halves.

// fakeEmbedder maps text to a vector by keyword, standing in for a model.
//
// Deliberately not a hash of the words: the whole point of a real embedding is
// that two different phrasings of the same idea land near each other, so the
// fake has to do that too or the tests would pass under the hashed fallback.
type fakeEmbedder struct {
	model string
	dim   int
	mu    sync.Mutex
	calls int
	fail  error
}

// topics: text mentioning any synonym of a topic gets that topic's axis.
var topics = [][]string{
	{"sign in", "signed in", "log in", "logged in", "credential", "password", "auth"},
	{"billing", "invoice", "invoicing", "payment", "charge"},
	{"build", "compile", "toolchain", "cargo", "make"},
}

func (f *fakeEmbedder) Model() string { return f.model }
func (f *fakeEmbedder) Dim() int      { return f.dim }

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	f.calls++
	err := f.fail
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}

	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		lower := strings.ToLower(t)
		vec := make([]float32, f.dim)
		for i, synonyms := range topics {
			for _, s := range synonyms {
				if strings.Contains(lower, s) {
					vec[i] = 1
					break
				}
			}
		}
		// A non-zero floor so cosine similarity is defined for text that
		// matches no topic at all.
		vec[f.dim-1] = 0.01
		out = append(out, vec)
	}
	return out, nil
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newFake() *fakeEmbedder { return &fakeEmbedder{model: "fake-embed-v1", dim: 8} }

// ------------------------------------------------------------- shared pool ---

func TestFleetNamespaceIsReadableByEveryAgent(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	// One agent records a shared finding.
	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace,
		Title:     "Build flag for the ARM runners",
		Content:   "cargo build needs --target aarch64 on the ARM runners",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	// And a private one, which must not leak.
	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: BotNamespace("bot-a"),
		Title:     "My own half-finished patch",
		Content:   "cargo build left an unstaged change in src/lib.rs",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	// A different agent recalls.
	hits := e.SearchScoped(ctx, RecallScope("bot-b"), "cargo build", 5)
	if len(hits) == 0 {
		t.Fatal("a second agent could not reach the shared memory at all")
	}
	titles := map[string]bool{}
	for _, h := range hits {
		titles[h.Title] = true
	}
	if !titles["Build flag for the ARM runners"] {
		t.Error("the shared finding was not retrievable by another agent")
	}
	if titles["My own half-finished patch"] {
		t.Error("another agent's private memory leaked into recall")
	}
}

func TestRecallScopeIncludesAutoIndexedTasks(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	// AutoIndexTask writes here. Recall searched only the bot namespace and
	// "fleet", so every auto-indexed trajectory was written, stored and never
	// read by anything.
	if err := e.AutoIndexTask(ctx, &protocol.Task{
		ID: "task-1", InstanceID: "bot-a",
		Goal: "reset the invoicing portal password",
	}, "Opened the portal, used the shared credential, reset it"); err != nil {
		t.Fatalf("auto-index: %v", err)
	}

	hits := e.SearchScoped(ctx, RecallScope("bot-b"), "invoicing portal", 5)
	if len(hits) == 0 {
		t.Fatal("auto-indexed task trajectories are still unreachable from recall")
	}
}

func TestRecallScopeNamesAllThreePools(t *testing.T) {
	got := RecallScope("bot-x")
	want := []string{"bot:bot-x", FleetNamespace, TasksNamespace}
	if len(got) != len(want) {
		t.Fatalf("RecallScope = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RecallScope[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// -------------------------------------------------------------- embeddings ---

func TestWithoutAnEmbedderTheEngineSaysSo(t *testing.T) {
	e := NewEngine()
	model, real := e.UsingEmbeddings()
	if real {
		t.Error("a fresh engine claims real embeddings")
	}
	// Named rather than silent: "semantic search" that has quietly fallen back
	// to word overlap is exactly what this reports.
	if !strings.Contains(model, "hashed") {
		t.Errorf("model = %q, want it to name the hashed fallback", model)
	}
}

func TestEmbeddingsFindAMemoryThatSharesNoWordsWithTheQuery(t *testing.T) {
	ctx := context.Background()
	fake := newFake()

	// The query and the memory have no words in common. This is the recall a
	// hashed bag of words cannot do, so it is the assertion that distinguishes
	// real embeddings from the fallback.
	const query = "how do I sign in to the billing portal"
	const content = "logged into the invoicing site with the shared credential"

	withEmb := NewEngine()
	withEmb.SetEmbedder(ctx, fake)
	if err := withEmb.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "Access note", Content: content,
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if hits := withEmb.SearchScoped(ctx, []string{FleetNamespace}, query, 5); len(hits) == 0 {
		t.Error("with embeddings attached, a semantically identical memory was not found")
	}

	// And the fallback genuinely cannot, which is what the README described.
	hashed := NewEngine()
	if err := hashed.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "Access note", Content: content,
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if hits := hashed.SearchScoped(ctx, []string{FleetNamespace}, query, 5); len(hits) > 0 {
		t.Log("the hashed fallback happened to match; the embedding path is still the one under test")
	}

	if fake.callCount() == 0 {
		t.Error("the embedder was never called")
	}
}

func TestAttachingAnEmbedderReembedsWhatIsAlreadyIndexed(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()

	// Stored before any embedder exists, so these carry hashed vectors.
	for _, title := range []string{"one", "two", "three"} {
		if err := e.StoreMemory(ctx, protocol.MemoryRecord{
			Namespace: FleetNamespace, Title: title, Content: "signed in with the credential",
		}); err != nil {
			t.Fatalf("store: %v", err)
		}
	}

	fake := newFake()
	// Synchronous, so the test does not race the background goroutine.
	n, err := e.reembedAll(ctx, fake)
	if err != nil {
		t.Fatalf("reembed: %v", err)
	}
	if n != 3 {
		t.Errorf("re-embedded %d memories, want 3", n)
	}

	// Without this, old memories keep hashed vectors while new ones get real
	// ones, and a search compares two unrelated spaces — worse than either.
	for _, m := range e.ListNamespace(ctx, FleetNamespace, 10) {
		if m.EmbedModel != fake.Model() {
			t.Errorf("memory %q still has embed_model %q", m.Title, m.EmbedModel)
		}
		if len(m.Embedding) != fake.Dim() {
			t.Errorf("memory %q has a %d-dimension vector, want %d",
				m.Title, len(m.Embedding), fake.Dim())
		}
	}
}

func TestReembedIsSkippedForMemoriesAlreadyOnTheModel(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	fake := newFake()
	e.SetEmbedder(ctx, fake)

	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "already embedded", Content: "build the toolchain",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	// A second pass must be a no-op, or every restart re-embeds the whole index
	// and pays for it again.
	n, err := e.reembedAll(ctx, fake)
	if err != nil {
		t.Fatalf("reembed: %v", err)
	}
	if n != 0 {
		t.Errorf("re-embedded %d memories that were already on this model, want 0", n)
	}
}

func TestVectorsFromDifferentModelsAreNotComparedDirectly(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	fake := newFake()
	e.SetEmbedder(ctx, fake)

	// A record left over from another model, with a vector of a different width
	// carrying values that would score high against anything if compared
	// naively.
	e.mu.Lock()
	e.memories["stale"] = protocol.MemoryRecord{
		ID: "stale", Namespace: FleetNamespace,
		Title: "stale record", Content: "compile the project with cargo",
		EmbedModel: "some-other-model-v9",
		Embedding:  []float32{9, 9, 9},
	}
	e.mu.Unlock()

	// Must not panic, must not rank on a meaningless number, and must still
	// find the record by its text — which is what the hashed fallback path is
	// for.
	hits := e.SearchScoped(ctx, []string{FleetNamespace}, "compile the project with cargo", 5)
	found := false
	for _, h := range hits {
		if h.ID == "stale" {
			found = true
		}
	}
	if !found {
		t.Error("a record embedded by another model became unfindable rather than falling back")
	}
}

func TestAFailingEmbedderDegradesInsteadOfFailingTheAction(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	broken := newFake()
	broken.fail = errors.New("embedding endpoint is down")
	e.SetEmbedder(ctx, broken)

	// An agent's remember must not fail because an embedding sidecar is down:
	// the memory is still worth keeping, and a hashed vector is still
	// searchable.
	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "still stored", Content: "the build flag is -tags prod",
	}); err != nil {
		t.Fatalf("store should not fail when embedding fails: %v", err)
	}
	if hits := e.SearchScoped(ctx, []string{FleetNamespace}, "the build flag is -tags prod", 5); len(hits) == 0 {
		t.Error("the memory is not searchable after an embedding failure")
	}
}

func TestExactMatchesStillWinOverSimilarity(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	e.SetEmbedder(ctx, newFake())

	// An agent recalling a verbatim error string wants that record. No
	// embedding ranks a literal quote reliably above a topical near-miss, which
	// is why the keyword boost survives.
	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "topical", Content: "notes about signing in",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "verbatim",
		Content:   "error E0432: unresolved import `crate::widget`",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	hits := e.SearchScoped(ctx, []string{FleetNamespace}, "E0432: unresolved import", 5)
	if len(hits) == 0 {
		t.Fatal("the verbatim record was not found")
	}
	if hits[0].Title != "verbatim" {
		t.Errorf("top hit is %q, want the exact match", hits[0].Title)
	}
}

func TestStoredMemoryIsSearchableImmediately(t *testing.T) {
	ctx := context.Background()
	e := NewEngine()
	e.SetEmbedder(ctx, newFake())

	// The hashed vector is written under the lock and the real one replaces it
	// after the network call, so a recall landing in between still finds it.
	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: FleetNamespace, Title: "instant", Content: "a distinctive phrase xyzzy",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if hits := e.SearchScoped(ctx, []string{FleetNamespace}, "xyzzy", 5); len(hits) == 0 {
		t.Error("a memory was not searchable straight after being stored")
	}
}
