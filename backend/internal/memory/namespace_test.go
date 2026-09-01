package memory

import (
	"context"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// One agent's private memories must not surface in another agent's recall,
// while anything filed fleet-wide must reach both.
func TestBotMemoryIsPrivateButFleetIsShared(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	store := func(ns, title, content string) {
		t.Helper()
		if err := e.StoreMemory(ctx, protocol.MemoryRecord{
			Namespace: ns, Title: title, Content: content,
		}); err != nil {
			t.Fatalf("store %s: %v", title, err)
		}
	}

	store(BotNamespace("alpha"), "alpha secret", "the deploy key lives in alpha home")
	store(BotNamespace("beta"), "beta secret", "the deploy key lives in beta home")
	store("fleet", "shared runbook", "the deploy key rotates every friday")

	titles := func(hits []protocol.MemoryRecord) map[string]bool {
		out := map[string]bool{}
		for _, h := range hits {
			out[h.Title] = true
		}
		return out
	}

	got := titles(e.SearchScoped(ctx,
		[]string{BotNamespace("alpha"), "fleet"}, "deploy key", 10))

	if !got["alpha secret"] {
		t.Error("alpha cannot recall its own memory")
	}
	if !got["shared runbook"] {
		t.Error("alpha cannot recall the shared fleet memory")
	}
	if got["beta secret"] {
		t.Error("alpha recalled beta's private memory; namespaces are not isolating")
	}
}

// An empty namespace still means "everything", so callers that never opted into
// scoping keep working.
func TestEmptyNamespaceSearchesEverything(t *testing.T) {
	e := NewEngine()
	ctx := context.Background()

	if err := e.StoreMemory(ctx, protocol.MemoryRecord{
		Namespace: BotNamespace("alpha"), Title: "note", Content: "a widget broke",
	}); err != nil {
		t.Fatal(err)
	}

	if len(e.Search(ctx, "", "widget", 5)) != 1 {
		t.Error("unscoped search should see every namespace")
	}
}
