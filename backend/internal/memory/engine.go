package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Store is the persistence an Engine writes memories through to.
//
// An interface rather than *store.Store so the engine works with no database,
// which is how the tests build it and how GlobalEngine behaves until the
// server attaches a store on boot.
type Store interface {
	UpsertMemory(ctx context.Context, m protocol.MemoryRecord) error
	ListMemories(ctx context.Context, namespace string, limit int) ([]protocol.MemoryRecord, error)
	DeleteMemory(ctx context.Context, id string) error
}

// hydrateLimit caps what is loaded back on boot. Search is a linear scan over
// the working set, so the whole point of the index is that it stays small
// enough to scan; older memories remain in the table.
const hydrateLimit = 2000

// vectorDim must match computeBagOfWordsVector; a stored embedding of any other
// length is from a different hashing scheme and gets recomputed on load.
const vectorDim = 128

// Embedder is the optional real-embedding backend.
//
// An interface here rather than a dependency on connectors: the memory package
// is imported by the agent loop and the API, and reaching back into the provider
// registry from it would be a cycle.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dim() int
}

// Engine manages long-term episodic memory indexing and semantic search across the fleet.
type Engine struct {
	mu       sync.RWMutex
	memories map[string]protocol.MemoryRecord
	// seq disambiguates records created within the same nanosecond tick.
	seq uint64

	// store is optional. When nil the engine is in-memory only and every
	// memory dies with the process.
	store Store
	log   *slog.Logger

	// embedder is optional. With one attached, memories and queries are scored
	// by real semantic similarity; without one the engine falls back to the
	// 128-dimensional hashed bag of words, which only matches when the query
	// reuses the memory's own words.
	//
	// embedModel is the model those vectors came from. A memory embedded by a
	// different model is not comparable — the vectors live in unrelated spaces —
	// so it is re-embedded rather than scored against nonsense.
	embedder   Embedder
	embedModel string
}

// SetEmbedder attaches a real embedding backend and re-embeds what is already
// indexed.
//
// The re-embedding matters: without it the fleet's existing memories keep their
// hashed vectors while new ones get real ones, and a search compares vectors
// from two unrelated spaces, which is worse than either on its own. It runs in
// the background because a fleet with a couple of thousand memories is a couple
// of thousand vectors to fetch and boot should not wait for it.
func (e *Engine) SetEmbedder(ctx context.Context, emb Embedder) {
	e.mu.Lock()
	e.embedder = emb
	log := e.log
	e.mu.Unlock()
	if log == nil {
		log = slog.Default()
	}

	go func() {
		bg := context.WithoutCancel(ctx)
		if n, err := e.reembedAll(bg, emb); err != nil {
			log.Warn("existing memories were not re-embedded; recall mixes two vector spaces "+
				"until they are", "err", err)
		} else if n > 0 {
			log.Info("memories re-embedded with the configured model",
				"count", n, "model", emb.Model())
		}
	}()
}

// UsingEmbeddings reports what is scoring searches, for the API to surface.
//
// Reported rather than assumed: "semantic search" that has silently fallen back
// to word overlap is the kind of thing that looks fine until someone relies on
// it.
func (e *Engine) UsingEmbeddings() (model string, real bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.embedder == nil {
		return "hashed-bag-of-words-128", false
	}
	return e.embedder.Model(), true
}

// reembedAll recomputes every vector with emb, in batches.
func (e *Engine) reembedAll(ctx context.Context, emb Embedder) (int, error) {
	e.mu.RLock()
	ids := make([]string, 0, len(e.memories))
	for id, m := range e.memories {
		// Skip what is already embedded by this model at this dimension.
		if m.EmbedModel == emb.Model() && len(m.Embedding) == emb.Dim() && emb.Dim() > 0 {
			continue
		}
		ids = append(ids, id)
	}
	e.mu.RUnlock()
	if len(ids) == 0 {
		e.mu.Lock()
		e.embedModel = emb.Model()
		e.mu.Unlock()
		return 0, nil
	}

	// Batched: one request per memory would be thousands of round trips, and
	// every provider here accepts a list.
	const batch = 64
	done := 0
	for start := 0; start < len(ids); start += batch {
		end := min(start+batch, len(ids))
		chunk := ids[start:end]

		texts := make([]string, 0, len(chunk))
		present := make([]string, 0, len(chunk))
		e.mu.RLock()
		for _, id := range chunk {
			m, ok := e.memories[id]
			if !ok {
				continue // forgotten while this was running
			}
			texts = append(texts, embedText(m))
			present = append(present, id)
		}
		e.mu.RUnlock()
		if len(texts) == 0 {
			continue
		}

		vecs, err := emb.Embed(ctx, texts)
		if err != nil {
			return done, err
		}

		e.mu.Lock()
		for i, id := range present {
			m, ok := e.memories[id]
			if !ok || i >= len(vecs) {
				continue
			}
			m.Embedding = vecs[i]
			m.EmbedModel = emb.Model()
			e.memories[id] = m
			done++
		}
		st := e.store
		e.mu.Unlock()

		// Persisted so the next boot does not repeat the work, and pay for it,
		// every time the orchestrator restarts.
		if st != nil {
			e.mu.RLock()
			toWrite := make([]protocol.MemoryRecord, 0, len(present))
			for _, id := range present {
				if m, ok := e.memories[id]; ok {
					toWrite = append(toWrite, m)
				}
			}
			e.mu.RUnlock()
			for _, m := range toWrite {
				if err := st.UpsertMemory(ctx, m); err != nil && e.log != nil {
					e.log.Warn("re-embedded memory not persisted", "id", m.ID, "err", err)
				}
			}
		}
	}

	e.mu.Lock()
	e.embedModel = emb.Model()
	e.mu.Unlock()
	return done, nil
}

// embedText is the text a memory is embedded from. One definition, so a stored
// vector and a later recomputation cannot disagree about what was embedded.
func embedText(m protocol.MemoryRecord) string {
	return strings.TrimSpace(m.Title + "\n" + m.Content + "\n" + strings.Join(m.Tags, " "))
}

// vectorFor embeds text, falling back to the hashed vector.
//
// The fallback is not silent about which one it produced: the returned model
// name is stored on the memory, so a search can tell a real vector from a hashed
// one and refuse to compare them.
func (e *Engine) vectorFor(ctx context.Context, text string) ([]float32, string) {
	e.mu.RLock()
	emb, log := e.embedder, e.log
	e.mu.RUnlock()

	if emb == nil {
		return computeBagOfWordsVector(text), ""
	}
	vecs, err := emb.Embed(ctx, []string{text})
	if err != nil || len(vecs) == 0 || len(vecs[0]) == 0 {
		// Degraded rather than failed. An agent's remember should not fail
		// because an embedding endpoint is briefly down, and a hashed vector is
		// still searchable — just less well, and the mismatch is recorded.
		if log != nil && err != nil {
			log.Warn("embedding failed; falling back to the hashed vector", "err", err)
		}
		return computeBagOfWordsVector(text), ""
	}
	return vecs[0], emb.Model()
}

// GlobalEngine is the fleet-wide episodic memory, shared across instances so a
// discovery made by one agent is retrievable by another.
var GlobalEngine = NewEngine()

func NewEngine() *Engine {
	return &Engine{
		memories: make(map[string]protocol.MemoryRecord),
	}
}

// AttachStore makes the index durable: what the fleet has already learned is
// loaded back, and everything stored afterwards is written through.
//
// Records whose persisted embedding is missing or from a different vector
// scheme are re-embedded here rather than dropped, so a change to the hashing
// degrades to a one-off recompute instead of a silent search blind spot.
func (e *Engine) AttachStore(ctx context.Context, st Store, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	saved, err := st.ListMemories(ctx, "", hydrateLimit)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = st
	e.log = log
	for _, m := range saved {
		// Anything stored before the attach wins: it is newer than the table.
		if _, live := e.memories[m.ID]; live {
			continue
		}
		// A record with no usable vector is re-hashed so it is at least
		// searchable. One embedded by a real model keeps its vector whatever its
		// length: only the hashed scheme has a fixed dimension, and recomputing
		// a 1536-dimension OpenAI vector as a 128-dimension hash would throw
		// away the good one.
		if m.EmbedModel == "" && len(m.Embedding) != vectorDim {
			m.Embedding = computeBagOfWordsVector(embedText(m))
		}
		e.memories[m.ID] = m
	}
	return nil
}

// StoreMemory registers a new episodic memory item into the index.
func (e *Engine) StoreMemory(ctx context.Context, mem protocol.MemoryRecord) error {
	e.mu.Lock()

	if mem.ID == "" {
		// A timestamp alone is not an identity. This formatted to second
		// resolution, so two memories stored in the same second produced the
		// same key and the second silently overwrote the first — losing exactly
		// the discoveries an agent bothered to record, and most often during a
		// burst, which is when they matter.
		e.seq++
		mem.ID = fmt.Sprintf("mem-%d-%d", time.Now().UnixNano(), e.seq)
	}
	if mem.Namespace == "" {
		mem.Namespace = "global"
	}
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = time.Now().UTC()
	}

	// Indexed under the hashed vector first so the memory is searchable the
	// instant this returns, then upgraded below. Embedding is a network call and
	// cannot happen under the lock — every recall in the fleet would wait on it.
	mem.Embedding = computeBagOfWordsVector(embedText(mem))
	e.memories[mem.ID] = mem
	st, log, emb := e.store, e.log, e.embedder
	// Unlocked explicitly rather than by defer: the database round trip below
	// has to happen outside the lock, or every recall in the fleet waits on it.
	e.mu.Unlock()

	if emb != nil {
		if vec, model := e.vectorFor(ctx, embedText(mem)); model != "" {
			mem.Embedding = vec
			mem.EmbedModel = model
			e.mu.Lock()
			// Re-checked: the memory may have been forgotten while the
			// embedding request was in flight, and reinserting it would
			// resurrect something the operator deleted.
			if _, still := e.memories[mem.ID]; still {
				e.memories[mem.ID] = mem
			}
			e.mu.Unlock()
		}
	}

	if st != nil {
		if err := st.UpsertMemory(ctx, mem); err != nil {
			// Not returned: the memory IS stored and recallable right now, and
			// the caller is an agent that would report the whole remember
			// action as failed and retry it.
			log.Warn("episodic memory not persisted", "id", mem.ID, "err", err)
		}
	}
	return nil
}

// BotNamespace is the private memory namespace of a single instance.
//
// Each agent remembers into its own namespace so that what one agent learned
// about its own desktop, its own credentials, its own half-finished work does
// not surface as advice in another agent's recall. Shared knowledge still has a
// home: anything written to "fleet" or "global" is visible to everyone.
func BotNamespace(instanceID string) string {
	if instanceID == "" {
		return FleetNamespace
	}
	return "bot:" + instanceID
}

// FleetNamespace is the shared pool every agent can read and write.
//
// Named rather than spelled out at each use: recall already searched it and
// nothing ever wrote to it, which is the kind of mismatch a string literal in
// two places invites.
const FleetNamespace = "fleet"

// TasksNamespace holds auto-indexed task trajectories, written by AutoIndexTask.
const TasksNamespace = "tasks"

// RecallScope is the set of namespaces a recall searches.
//
// One definition, because it was wrong in a way that only shows up by reading
// two files at once: AutoIndexTask wrote every completed task's summary to
// "tasks", and recall searched the bot's namespace and "fleet" — so the entire
// auto-indexed trajectory history was written, stored, paid for, and never read
// by anything.
func RecallScope(instanceID string) []string {
	return []string{BotNamespace(instanceID), FleetNamespace, TasksNamespace}
}

// Search retrieves the top-K relevant memories matching a query in one
// namespace, plus whatever is shared.
func (e *Engine) Search(ctx context.Context, namespace, query string, limit int) []protocol.MemoryRecord {
	return e.SearchScoped(ctx, []string{namespace}, query, limit)
}

// SearchScoped is Search across several namespaces at once, which is what a
// recall actually wants: the agent's own memory and the shared pool ranked
// together, so the best answer wins rather than whichever pool was asked first.
func (e *Engine) SearchScoped(ctx context.Context, namespaces []string, query string, limit int) []protocol.MemoryRecord {
	if limit <= 0 {
		limit = 5
	}

	// Embedded outside the lock: this is a network call when a real embedder is
	// attached, and holding the read lock across it would block every remember
	// in the fleet behind one recall.
	qVec, qModel := e.vectorFor(ctx, query)

	e.mu.RLock()
	defer e.mu.RUnlock()

	// "global" is readable from every namespace, so an empty or global-only
	// scope means "no restriction" exactly as it did before.
	allowed := make(map[string]bool, len(namespaces))
	unrestricted := len(namespaces) == 0
	for _, ns := range namespaces {
		if ns == "" || ns == "global" {
			unrestricted = true
			continue
		}
		allowed[ns] = true
	}

	type scoredMemory struct {
		mem   protocol.MemoryRecord
		score float64
	}
	// The hashed vector for the query, computed lazily: it is only needed if
	// some memory in scope was never embedded by the current model.
	var fallbackVec []float32
	qLower := strings.ToLower(strings.TrimSpace(query))

	var scored []scoredMemory
	for _, m := range e.memories {
		if !unrestricted && !allowed[m.Namespace] && m.Namespace != "global" {
			continue
		}

		// Vectors are only comparable within one model's space. Scoring an
		// OpenAI embedding against a hashed bag of words produces a number, and
		// that number is meaningless — which is the failure mode that makes a
		// half-migrated index worse than either scheme alone. Mismatched records
		// are scored on the hashed vector both sides can produce.
		var score float64
		switch {
		case m.EmbedModel == qModel:
			score = cosineSimilarity(qVec, m.Embedding)
		default:
			if fallbackVec == nil {
				fallbackVec = computeBagOfWordsVector(query)
			}
			hashed := m.Embedding
			if m.EmbedModel != "" || len(hashed) != vectorDim {
				// This memory's stored vector is from another space, so it
				// cannot be used; the text is re-hashed to compare like with
				// like. A linear scan already reads every record, so this costs
				// no extra pass.
				hashed = computeBagOfWordsVector(embedText(m))
			}
			score = cosineSimilarity(fallbackVec, hashed)
		}

		// A literal match beats any similarity score. Kept from the original
		// implementation: an agent recalling an exact error string or a hostname
		// wants that record, and no embedding ranks a verbatim quote reliably
		// above a topical near-miss.
		if qLower != "" &&
			(strings.Contains(strings.ToLower(m.Title), qLower) ||
				strings.Contains(strings.ToLower(m.Content), qLower)) {
			score += 0.5
		}

		if score > 0.1 {
			scored = append(scored, scoredMemory{mem: m, score: score})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	out := make([]protocol.MemoryRecord, 0, limit)
	for i := 0; i < len(scored) && i < limit; i++ {
		out = append(out, scored[i].mem)
	}
	return out
}

// AutoIndexTask converts a successfully executed task trajectory into a fleet episodic memory.
func (e *Engine) AutoIndexTask(ctx context.Context, task *protocol.Task, summary string) error {
	if task == nil || task.Goal == "" {
		return nil
	}

	title := "Task Resolution: " + task.Goal
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	mem := protocol.MemoryRecord{
		ID:               "task-mem-" + task.ID,
		Namespace:        "tasks",
		Title:            title,
		Content:          summary,
		Tags:             []string{"task", "trajectory", task.InstanceID},
		SourceTaskID:     task.ID,
		SourceInstanceID: task.InstanceID,
		CreatedAt:        time.Now().UTC(),
	}

	return e.StoreMemory(ctx, mem)
}

// Vector math helpers:

func computeBagOfWordsVector(text string) []float32 {
	words := strings.Fields(strings.ToLower(text))
	freqs := make(map[string]float32)
	for _, w := range words {
		cleaned := strings.Trim(w, ".,!?;:\"'()[]{}")
		if len(cleaned) > 2 {
			freqs[cleaned]++
		}
	}

	// 128-dimensional hashed embedding
	vec := make([]float32, 128)
	for word, count := range freqs {
		hash := fnv32(word) % 128
		vec[hash] += count
	}

	// L2 Normalize
	var sumSq float32
	for _, v := range vec {
		sumSq += v * v
	}
	if sumSq > 0 {
		norm := float32(math.Sqrt(float64(sumSq)))
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

func fnv32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h *= 16777619
		h ^= uint32(s[i])
	}
	return h
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ListNamespace returns everything remembered in one namespace, newest first.
//
// Unlike Search this does no ranking and takes no query: it answers "what has
// this agent chosen to keep", which is a question about the whole namespace
// rather than about relevance to anything.
func (e *Engine) ListNamespace(ctx context.Context, namespace string, limit int) []protocol.MemoryRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}
	out := make([]protocol.MemoryRecord, 0)
	for _, m := range e.memories {
		if m.Namespace == namespace {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Forget removes one memory. An agent that recorded something wrong keeps
// recalling it until someone can take it out.
func (e *Engine) Forget(ctx context.Context, id string) bool {
	e.mu.Lock()
	_, existed := e.memories[id]
	delete(e.memories, id)
	st, log := e.store, e.log
	e.mu.Unlock()

	if existed && st != nil {
		if err := st.DeleteMemory(ctx, id); err != nil && log != nil {
			log.Warn("memory not deleted from store", "id", id, "err", err)
		}
	}
	return existed
}

// AboutUser returns what this agent has noted about one person.
//
// Not a Search: this is "who is this", not "what is relevant to a query", and
// ranking notes about a person by similarity to nothing would return them in
// an arbitrary order. Newest first, because a preference someone stated
// recently supersedes one they stated a year ago.
func (e *Engine) AboutUser(ctx context.Context, namespace, userID string, limit int) []protocol.MemoryRecord {
	if userID == "" {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 {
		limit = 5
	}
	out := make([]protocol.MemoryRecord, 0, limit)
	for _, m := range e.memories {
		if m.AboutUserID != userID {
			continue
		}
		// A note about a person is still scoped to the agent that made it —
		// what one bot learned about a colleague is not every bot's to know —
		// but shared namespaces stay readable, as they are for recall.
		if namespace != "" && m.Namespace != namespace &&
			m.Namespace != "global" && m.Namespace != "fleet" {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
