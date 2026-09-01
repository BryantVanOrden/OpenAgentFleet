package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Generated IDs used to be time.Now().Format("20060102150405") — second
// resolution — so anything stored inside the same second collided and the later
// record silently replaced the earlier one in the map. Bursts are exactly when
// an agent records several findings at once, so this lost the discoveries most
// worth keeping.
func TestStoreMemoryDoesNotCollideWithinTheSameSecond(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	const n = 50
	for i := 0; i < n; i++ {
		if err := e.StoreMemory(ctx, protocol.MemoryRecord{
			Namespace: "fleet",
			Title:     fmt.Sprintf("finding %d", i),
			Content:   fmt.Sprintf("the %dth thing worth remembering", i),
		}); err != nil {
			t.Fatalf("StoreMemory(%d) = %v", i, err)
		}
	}

	e.mu.RLock()
	got := len(e.memories)
	e.mu.RUnlock()

	if got != n {
		t.Fatalf("stored %d records but the index holds %d — ids collided", n, got)
	}
}

func TestStoreMemoryIsSafeUnderConcurrentWriters(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	const writers, each = 8, 20
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				_ = e.StoreMemory(ctx, protocol.MemoryRecord{
					Namespace: "fleet",
					Title:     fmt.Sprintf("w%d-%d", w, i),
					Content:   "concurrent write",
				})
			}
		}(w)
	}
	wg.Wait()

	e.mu.RLock()
	got := len(e.memories)
	e.mu.RUnlock()

	if got != writers*each {
		t.Fatalf("expected %d records after concurrent writes, got %d", writers*each, got)
	}
}

// An explicitly supplied ID still wins — AutoIndexTask relies on that to make a
// task's memory idempotent under retry.
func TestStoreMemoryKeepsAnExplicitID(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := e.StoreMemory(ctx, protocol.MemoryRecord{
			ID:      "task-mem-abc",
			Title:   "same task, retried",
			Content: fmt.Sprintf("attempt %d", i),
		}); err != nil {
			t.Fatalf("StoreMemory = %v", err)
		}
	}

	e.mu.RLock()
	got := len(e.memories)
	e.mu.RUnlock()

	if got != 1 {
		t.Fatalf("an explicit id should overwrite in place; got %d records", got)
	}
}
