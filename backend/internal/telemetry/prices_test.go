package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The price table was hand-maintained list prices and nothing fetched current
// ones. These pin the live table's contract: exact normalised matches only,
// the static table untouched as the fallback, and provenance reported.

func TestLivePricesAreFetchedAndMatchedExactly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"anthropic/claude-sonnet-4-5-20250929","pricing":{"prompt":"0.000004","completion":"0.00002","input_cache_read":"0.0000004"}},
			{"id":"openai/gpt-4o","pricing":{"prompt":"0.000005","completion":"0.000015"}},
			{"id":"qwen/qwen-2.5-vl-7b:free","pricing":{"prompt":"0","completion":"0"}}
		]}`))
	}))
	defer srv.Close()
	t.Cleanup(func() {
		livePrices.mu.Lock()
		livePrices.byModel = map[string]modelPrice{}
		livePrices.source = ""
		livePrices.mu.Unlock()
	})

	n, err := livePrices.fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("priced %d models, want 3", n)
	}

	// Exact normalised match: the vendor prefix is dropped, so the name a
	// connector reports finds the live rate.
	p := priceFor("claude-sonnet-4-5-20250929")
	if p.prompt != 0.000004 || p.completion != 0.00002 {
		t.Errorf("live price not used: %+v", p)
	}
	// The cache-read rate arrives absolute and is stored as a fraction of input.
	if p.cachedDiscount < 0.09 || p.cachedDiscount > 0.11 {
		t.Errorf("cachedDiscount = %v, want ~0.10", p.cachedDiscount)
	}

	// No exact live match -> the static substring table still answers. This is
	// the guard against a local model being billed at a hosted catalogue's rate.
	p = priceFor("claude-sonnet-9-experimental")
	if p.prompt != modelPrices["claude-sonnet"].prompt {
		t.Errorf("a fuzzy live match leaked through: %+v", p)
	}

	// Provenance is reported, not implied.
	info := livePrices.info()
	if info.Source != "openrouter" || info.LiveModels != 3 || info.FetchedAt == nil {
		t.Errorf("provenance = %+v", info)
	}
}

func TestWithNoLiveTableTheSummarySaysBuiltin(t *testing.T) {
	livePrices.mu.Lock()
	livePrices.byModel = map[string]modelPrice{}
	livePrices.source = ""
	livePrices.mu.Unlock()

	if info := livePrices.info(); info.Source != "builtin" || info.LiveModels != 0 {
		t.Errorf("empty table should report builtin pricing, got %+v", info)
	}
}

func TestAnEmptyCatalogueIsRejectedNotInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	if _, err := livePrices.fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("an empty catalogue replaced the table instead of being refused")
	}
}

func TestPricingCanBeDisabledByTheOperator(t *testing.T) {
	t.Setenv("PRICING_REFRESH", "off")
	if !PricingDisabled() {
		t.Fatal("PRICING_REFRESH=off did not disable the fetch")
	}
	t.Setenv("PRICING_REFRESH", "")
	if PricingDisabled() {
		t.Fatal("an empty PRICING_REFRESH should leave the fetch on")
	}
}
