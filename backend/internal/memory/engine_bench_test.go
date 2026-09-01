package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// These benchmarks are the evidence behind hydrateLimit. The working set is a
// deliberate exact linear scan, and the cap is honest only if a full-cap scan
// is measured, not assumed. Run with:
//
//	go test ./internal/memory -bench BenchmarkSearchScoped -benchmem -run ^$
//
// The numbers that set hydrateLimit=50000 are in the constant's comment.

// fillEngine loads n records straight into the index, bypassing StoreMemory's
// persistence and embedding paths — the scan is what is being measured.
func fillEngine(n int, embedModel string, dim int) *Engine {
	e := NewEngine()
	for i := 0; i < n; i++ {
		m := protocol.MemoryRecord{
			ID:        fmt.Sprintf("bench-%d", i),
			Namespace: "fleet",
			Title:     fmt.Sprintf("lesson %d about service deploys", i),
			Content: fmt.Sprintf("attempt %d: the staging cluster rejects image digests "+
				"unless the registry mirror has warmed; retry after the sync job", i),
			EmbedModel: embedModel,
		}
		if embedModel == "" {
			m.Embedding = computeBagOfWordsVector(embedText(m))
		} else {
			// A fake real-model vector: content-dependent so cosine has work to do.
			v := make([]float32, dim)
			for j := range v {
				v[j] = float32((i*31+j*7)%97) / 97
			}
			m.Embedding = v
		}
		e.memories[m.ID] = m
	}
	return e
}

// The main path: every record's vector lives in the query's own space.
func BenchmarkSearchScoped_50k_SameSpace(b *testing.B) {
	e := fillEngine(50000, "", vectorDim)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := e.SearchScoped(ctx, []string{"fleet"}, "registry mirror sync job", 5)
		if len(got) == 0 {
			b.Fatal("scan found nothing; the benchmark is measuring an empty index")
		}
	}
}

// The degraded path: the embedder is unreachable, so the query is hashed and
// every real-embedded record must be compared through its hashed fallback.
// This is the path the cache exists for — without it, each query re-hashes
// the entire working set.
func BenchmarkSearchScoped_50k_MismatchedSpace(b *testing.B) {
	e := fillEngine(50000, "text-embedding-3-small", 256)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.SearchScoped(ctx, []string{"fleet"}, "registry mirror sync job", 5)
	}
}
