package memory

import (
	"context"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func TestVectorMemoryEngine(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine()

	// 1. Store memories
	mem1 := protocol.MemoryRecord{
		ID:        "mem-godot-shader",
		Namespace: "gamedev",
		Title:     "Godot 4 Spatial Shader Optimization",
		Content:   "To fix shader stutter in Godot 4, enable shader caching and compile shaders asynchronously.",
		Tags:      []string{"godot", "shader", "graphics"},
		CreatedAt: time.Now().UTC(),
	}
	mem2 := protocol.MemoryRecord{
		ID:        "mem-comp-crm",
		Namespace: "crm",
		Title:     "Comp AI CRM Webhook Configuration",
		Content:   "Comp AI CRM webhooks require JSON payloads sent to /api/v1/leads with Bearer token authentication.",
		Tags:      []string{"crm", "compai", "webhook"},
		CreatedAt: time.Now().UTC(),
	}

	if err := engine.StoreMemory(ctx, mem1); err != nil {
		t.Fatalf("failed to store memory 1: %v", err)
	}
	if err := engine.StoreMemory(ctx, mem2); err != nil {
		t.Fatalf("failed to store memory 2: %v", err)
	}

	// 2. Search for godot shader memory
	results := engine.Search(ctx, "gamedev", "how to optimize shader in godot", 3)
	if len(results) == 0 {
		t.Fatalf("expected search results for godot shader query, got 0")
	}
	if results[0].ID != "mem-godot-shader" {
		t.Errorf("expected top result to be mem-godot-shader, got %s", results[0].ID)
	}

	// 3. Search for CRM memory
	crmResults := engine.Search(ctx, "", "comp ai crm webhook bearer", 3)
	if len(crmResults) == 0 {
		t.Fatalf("expected search results for CRM query, got 0")
	}
	if crmResults[0].ID != "mem-comp-crm" {
		t.Errorf("expected top result to be mem-comp-crm, got %s", crmResults[0].ID)
	}
}

func TestContainsFold(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"Registry Mirror SYNC", "mirror sync", true},
		{"registry mirror sync", "MIRROR", false}, // sub must arrive pre-lowered
		{"registry mirror sync", "mirror", true},
		{"short", "much longer than s", false},
		{"anything", "", true},
		{"edge at the END", "end", true},
		{"Ｕｎｉｃｏｄｅ", "ｕｎｉｃｏｄｅ", false}, // non-ASCII compared verbatim, as before
		{"", "x", false},
	}
	for _, c := range cases {
		if got := containsFold(c.s, c.sub); got != c.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", c.s, c.sub, got, c.want)
		}
	}
}
