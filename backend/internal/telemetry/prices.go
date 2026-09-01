package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Live pricing.
//
// The price table in this package is hand-maintained list prices, matched by
// substring, and it goes stale the day a vendor changes a rate. Nothing fetched
// current pricing. This does: OpenRouter publishes a public, keyless model
// catalogue with per-token USD prices for the hosted models this fleet is
// likely to run (https://openrouter.ai/api/v1/models), refreshed here at boot
// and daily.
//
// The static table stays, deliberately, as more than a fallback:
//   - Air-gapped fleets set PRICING_REFRESH=off and keep working.
//   - A live price is used only on an exact model-name match. The static
//     table's coarse substrings ("gemini") stay for everything else, because a
//     fuzzy match against someone else's catalogue is how a local Ollama model
//     ends up billed at a hosted model's rate.
//
// GetSummary reports which source priced the fleet and when it was fetched, so
// "approximate list prices" is a stated property rather than a surprise.

const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

// PricingDisabled reports whether the operator turned the fetch off.
func PricingDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PRICING_REFRESH")))
	return v == "off" || v == "false" || v == "0" || v == "no"
}

type livePriceTable struct {
	mu        sync.RWMutex
	byModel   map[string]modelPrice // normalised name -> price
	fetchedAt time.Time
	source    string
}

var livePrices = &livePriceTable{byModel: map[string]modelPrice{}}

// PricingInfo is the provenance block in the financial summary.
type PricingInfo struct {
	// Source is "openrouter" when live prices are loaded, "builtin" otherwise.
	Source    string     `json:"source"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	// LiveModels is how many models the live table can price exactly.
	LiveModels int `json:"live_models"`
}

func (t *livePriceTable) info() PricingInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.byModel) == 0 {
		return PricingInfo{Source: "builtin"}
	}
	at := t.fetchedAt
	return PricingInfo{Source: t.source, FetchedAt: &at, LiveModels: len(t.byModel)}
}

// lookup returns a live price on an exact normalised match, and nothing else.
func (t *livePriceTable) lookup(model string) (modelPrice, bool) {
	key := normaliseModelName(model)
	if key == "" {
		return modelPrice{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.byModel[key]
	return p, ok
}

// normaliseModelName maps the spellings providers report onto one key:
// lowercased, the vendor prefix dropped ("anthropic/claude-x" -> "claude-x"),
// and OpenRouter's ":free" variant suffix removed.
func normaliseModelName(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	m = strings.TrimSuffix(m, ":free")
	return m
}

type openRouterCatalogue struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt          string `json:"prompt"`
			Completion      string `json:"completion"`
			InputCacheRead  string `json:"input_cache_read"`
			InputCacheWrite string `json:"input_cache_write"`
		} `json:"pricing"`
	} `json:"data"`
}

// fetchPrices loads the catalogue from one URL (injectable for tests).
func (t *livePriceTable) fetch(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("price catalogue returned %d", resp.StatusCode)
	}

	var cat openRouterCatalogue
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&cat); err != nil {
		return 0, fmt.Errorf("unreadable price catalogue: %w", err)
	}

	table := make(map[string]modelPrice, len(cat.Data))
	for _, m := range cat.Data {
		prompt, errP := strconv.ParseFloat(m.Pricing.Prompt, 64)
		completion, errC := strconv.ParseFloat(m.Pricing.Completion, 64)
		if errP != nil || errC != nil {
			continue
		}
		p := modelPrice{prompt: prompt, completion: completion}
		// The cache-read rate comes as an absolute per-token price; the table
		// stores it as a fraction of the prompt rate, matching the static rows.
		if cacheRead, err := strconv.ParseFloat(m.Pricing.InputCacheRead, 64); err == nil && prompt > 0 && cacheRead > 0 {
			p.cachedDiscount = cacheRead / prompt
		}
		key := normaliseModelName(m.ID)
		if key == "" {
			continue
		}
		// First writer wins on collisions: OpenRouter lists variants of the
		// same model, and the base entry comes first.
		if _, dup := table[key]; !dup {
			table[key] = p
		}
	}
	if len(table) == 0 {
		return 0, fmt.Errorf("the price catalogue parsed but priced nothing")
	}

	t.mu.Lock()
	t.byModel = table
	t.fetchedAt = time.Now().UTC()
	t.source = "openrouter"
	t.mu.Unlock()
	return len(table), nil
}

// StartPriceRefresher fetches at boot and then daily, until ctx ends.
//
// Failures degrade to the static table and are logged once per attempt — a
// fleet with no internet keeps billing at list prices, exactly as before this
// existed, and the summary says so via Pricing.Source.
func StartPriceRefresher(ctx context.Context, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	if PricingDisabled() {
		log.Info("live pricing is off (PRICING_REFRESH); using the built-in table")
		return
	}

	refresh := func() {
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		n, err := livePrices.fetch(fetchCtx, openRouterModelsURL)
		if err != nil {
			log.Warn("live price refresh failed; the built-in table stays in effect", "err", err)
			return
		}
		log.Info("model prices refreshed", "source", "openrouter", "models", n)
	}

	refresh()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
