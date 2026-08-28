package memory

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Engine manages long-term episodic memory indexing and semantic search across the fleet.
type Engine struct {
	mu       sync.RWMutex
	memories map[string]protocol.MemoryRecord
}

func NewEngine() *Engine {
	return &Engine{
		memories: make(map[string]protocol.MemoryRecord),
	}
}

// StoreMemory registers a new episodic memory item into the index.
func (e *Engine) StoreMemory(ctx context.Context, mem protocol.MemoryRecord) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if mem.ID == "" {
		mem.ID = "mem-" + time.Now().Format("20060102150405")
	}
	if mem.Namespace == "" {
		mem.Namespace = "global"
	}
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = time.Now().UTC()
	}

	mem.Embedding = computeBagOfWordsVector(mem.Title + " " + mem.Content + " " + strings.Join(mem.Tags, " "))
	e.memories[mem.ID] = mem
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
