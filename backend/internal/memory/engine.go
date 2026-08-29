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
}

// hydrateLimit caps what is loaded back on boot. Search is a linear scan over
// the working set, so the whole point of the index is that it stays small
// enough to scan; older memories remain in the table.
const hydrateLimit = 2000

// vectorDim must match computeBagOfWordsVector; a stored embedding of any other
// length is from a different hashing scheme and gets recomputed on load.
const vectorDim = 128

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
		if len(m.Embedding) != vectorDim {
			m.Embedding = computeBagOfWordsVector(m.Title + " " + m.Content + " " + strings.Join(m.Tags, " "))
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

	mem.Embedding = computeBagOfWordsVector(mem.Title + " " + mem.Content + " " + strings.Join(mem.Tags, " "))
	e.memories[mem.ID] = mem
	st, log := e.store, e.log
	// Unlocked explicitly rather than by defer: the database round trip below
	// has to happen outside the lock, or every recall in the fleet waits on it.
	e.mu.Unlock()

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

// Search retrieves the top-K relevant memories matching a query.
func (e *Engine) Search(ctx context.Context, namespace, query string, limit int) []protocol.MemoryRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 {
		limit = 5
	}

	qVec := computeBagOfWordsVector(query)
	type scoredMemory struct {
		mem   protocol.MemoryRecord
		score float64
	}

	var scored []scoredMemory
	for _, m := range e.memories {
		if namespace != "" && namespace != "global" && m.Namespace != namespace && m.Namespace != "global" {
			continue
		}

		score := cosineSimilarity(qVec, m.Embedding)
		// Keyword match boost
		qLower := strings.ToLower(query)
		if strings.Contains(strings.ToLower(m.Title), qLower) || strings.Contains(strings.ToLower(m.Content), qLower) {
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
