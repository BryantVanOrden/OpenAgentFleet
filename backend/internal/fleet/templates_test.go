package fleet

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The bot template catalog lives in pkg/protocol, which cannot import fleet
// without a cycle — so the cross-check that every template points at a tier that
// actually exists has to live here. A template naming a nonexistent tier is a
// real bug: TierByName silently swaps in "standard", so a developer-heavy
// archetype would quietly provision on 2 vCPU and 4 GB.

func TestEveryBotTemplateNamesARealTier(t *testing.T) {
	known := map[protocol.Tier]bool{}
	for _, tr := range DefaultTiers("agentfleet/sandbox:latest") {
		known[tr.Name] = true
	}

	for _, tmpl := range protocol.DefaultBotTemplates() {
		if !known[tmpl.RecommendedTier] {
			t.Errorf("template %q recommends tier %q, which is not in DefaultTiers (known: %v)",
				tmpl.ID, tmpl.RecommendedTier, keysOf(known))
			continue
		}
		// And resolution must be an exact hit, not the standard fallback.
		got := TierByName(DefaultTiers("img"), tmpl.RecommendedTier)
		if got.Name != tmpl.RecommendedTier {
			t.Errorf("template %q: TierByName(%q) fell back to %q",
				tmpl.ID, tmpl.RecommendedTier, got.Name)
		}
	}
}

func TestBotTemplateCatalogIsWellFormed(t *testing.T) {
	templates := protocol.DefaultBotTemplates()
	if len(templates) == 0 {
		t.Fatal("the shipped catalog is empty")
	}

	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	for _, tmpl := range templates {
		if strings.TrimSpace(tmpl.ID) == "" {
			t.Errorf("a template has an empty id: %+v", tmpl.Name)
			continue
		}
		if seenIDs[tmpl.ID] {
			t.Errorf("duplicate template id %q — BotTemplateByID would only ever return the first", tmpl.ID)
		}
		seenIDs[tmpl.ID] = true

		if strings.TrimSpace(tmpl.Name) == "" {
			t.Errorf("template %q has an empty name", tmpl.ID)
		}
		if seenNames[tmpl.Name] {
			t.Errorf("duplicate template name %q", tmpl.Name)
		}
		seenNames[tmpl.Name] = true

		if strings.TrimSpace(tmpl.SpecializedPrompt) == "" {
			t.Errorf("template %q has an empty system prompt — the persona would be a no-op", tmpl.ID)
		}
		if strings.TrimSpace(tmpl.Tagline) == "" {
			t.Errorf("template %q has no tagline for the admin panel", tmpl.ID)
		}
		if strings.TrimSpace(tmpl.Category) == "" {
			t.Errorf("template %q has no category", tmpl.ID)
		}
		if tmpl.ID != strings.ToLower(tmpl.ID) {
			t.Errorf("template id %q is not lower case", tmpl.ID)
		}
		if strings.ContainsAny(tmpl.ID, " /?#") {
			t.Errorf("template id %q contains characters that break its URL path", tmpl.ID)
		}
	}
}

func TestBotTemplateResourceRequestsAreUsable(t *testing.T) {
	// A template may legitimately ask for more than its tier's defaults — that
	// is what the override mechanism is for, and "media_studio" really does ask
	// for a GPU on the power-user tier. What is NOT legitimate is a request that
	// ApplyOverride would silently discard, i.e. a non-positive one.
	for _, tmpl := range protocol.DefaultBotTemplates() {
		if tmpl.VCPU <= 0 || tmpl.MemoryMB <= 0 || tmpl.DiskGB <= 0 {
			t.Errorf("template %q has a non-positive resource request that ApplyOverride would drop: vcpu=%v mem=%d disk=%d",
				tmpl.ID, tmpl.VCPU, tmpl.MemoryMB, tmpl.DiskGB)
		}
	}
}

func TestGPUTemplatesGetAGPUEvenOnANonGPUTier(t *testing.T) {
	// The tier is only the starting envelope; the template's own GPU flag is
	// layered on top. Verify that actually happens, because a media_studio bot
	// silently landing without a GPU is a slow, expensive kind of wrong.
	tiers := DefaultTiers("agentfleet/sandbox:latest")
	for _, tmpl := range protocol.DefaultBotTemplates() {
		if !tmpl.GPU {
			continue
		}
		base := TierByName(tiers, tmpl.RecommendedTier)
		gpu := tmpl.GPU
		if got := ApplyOverride(base, &protocol.ResourceOverride{GPU: &gpu}); !got.GPU {
			t.Errorf("template %q wants a GPU but ApplyOverride on tier %q left GPU off",
				tmpl.ID, base.Name)
		}
	}
}

func TestBotTemplateOverrideRoundTrip(t *testing.T) {
	// Provisioning applies a template's resources as an override on its tier.
	// Whatever the template asks for has to survive that layering intact.
	tiers := DefaultTiers("agentfleet/sandbox:latest")
	for _, tmpl := range protocol.DefaultBotTemplates() {
		base := TierByName(tiers, tmpl.RecommendedTier)
		vcpu, mem, disk, gpu := tmpl.VCPU, tmpl.MemoryMB, tmpl.DiskGB, tmpl.GPU
		got := ApplyOverride(base, &protocol.ResourceOverride{
			VCPU: &vcpu, MemoryMB: &mem, DiskGB: &disk, GPU: &gpu,
		})
		if got.VCPU != tmpl.VCPU || got.MemoryMB != tmpl.MemoryMB ||
			got.DiskGB != tmpl.DiskGB || got.GPU != tmpl.GPU {
			t.Errorf("template %q resources did not survive ApplyOverride: got %+v", tmpl.ID, got)
		}
		if got.Image != base.Image {
			t.Errorf("template %q: image changed from %q to %q", tmpl.ID, base.Image, got.Image)
		}
	}
}

func TestBotTemplateByIDReturnsACopy(t *testing.T) {
	// The catalog is rebuilt per call, so a caller mutating the returned pointer
	// must not be able to poison what the next caller sees.
	first := protocol.BotTemplateByID("fullstack_dev")
	if first == nil {
		t.Fatal("fullstack_dev is missing from the catalog")
	}
	original := first.Name
	first.Name = "tampered"

	second := protocol.BotTemplateByID("fullstack_dev")
	if second == nil {
		t.Fatal("fullstack_dev vanished after mutating an earlier result")
	}
	if second.Name != original {
		t.Errorf("BotTemplateByID leaked shared state: name = %q, want %q", second.Name, original)
	}
}

func keysOf(m map[protocol.Tier]bool) []protocol.Tier {
	out := make([]protocol.Tier, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
