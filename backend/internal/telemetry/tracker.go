package telemetry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// maxRecords caps how many individual turn records we retain. The tracker lives
// for the whole process lifetime and RecordTurn is called once per model turn
// per running agent, so an unbounded slice is a slow memory leak that grows with
// uptime and fleet size. We keep the most recent maxRecords for the per-turn
// drill-down (ListRecords); the financial totals are kept as running aggregates
// below so trimming old records never loses money from the summary.
const maxRecords = 20000

// modelPrice is the USD cost per single token, split by prompt vs completion.
type modelPrice struct {
	prompt     float64
	completion float64
}

// modelPrices is a coarse per-model price table matched by substring, because a
// provider row's model name carries a version suffix (e.g.
// "claude-sonnet-4-5-20250929", "gpt-4o-2024-08-06") that we do not want to
// enumerate. First match wins; anything unrecognised falls back to
// defaultPrice. These are list prices and only approximate — the point is that
// billing an Opus turn at Sonnet rates (the previous single hardcoded pair) was
// wrong by 5x, not that this is invoice-accurate.
var modelPrices = map[string]modelPrice{
	"claude-opus":    {prompt: 0.000015, completion: 0.000075},
	"claude-sonnet":  {prompt: 0.000003, completion: 0.000015},
	"claude-haiku":   {prompt: 0.0000008, completion: 0.000004},
	"gpt-4o-mini":    {prompt: 0.00000015, completion: 0.0000006},
	"gpt-4o":         {prompt: 0.0000025, completion: 0.00001},
	"gpt-4":          {prompt: 0.00003, completion: 0.00006},
	"o1":             {prompt: 0.000015, completion: 0.00006},
	"gemini-1.5-pro": {prompt: 0.00000125, completion: 0.000005},
	"gemini":         {prompt: 0.0000003, completion: 0.0000012},
}

// defaultPrice is the fallback for an unrecognised model. It matches the old
// hardcoded pair so nothing that was already priced changes silently.
var defaultPrice = modelPrice{prompt: 0.000003, completion: 0.000015}

func priceFor(model string) modelPrice {
	m := strings.ToLower(strings.TrimSpace(model))
	// Longest keys first so "gpt-4o-mini" wins over "gpt-4o" and "gpt-4".
	for _, k := range priceKeysByLength {
		if strings.Contains(m, k) {
			return modelPrices[k]
		}
	}
	return defaultPrice
}

// priceKeysByLength is modelPrices' keys sorted longest-first, so substring
// matching is deterministic regardless of Go's random map iteration order.
var priceKeysByLength = sortedKeysByLength(modelPrices)

func sortedKeysByLength(m map[string]modelPrice) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort by descending length; the table is tiny.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && len(keys[j]) > len(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// Tracker records token usage, API cost calculations, and response latencies.
type Tracker struct {
	mu      sync.RWMutex
	records []protocol.TokenTelemetryRecord

	// Running totals kept independently of the records slice so the financial
	// summary stays complete even after old records are trimmed.
	agg FinancialSummary
	// sumLatency is the un-averaged latency total behind agg.AvgLatencyMS.
	sumLatency int64
}

var GlobalTracker = NewTracker()

func NewTracker() *Tracker {
	return &Tracker{
		records: make([]protocol.TokenTelemetryRecord, 0),
	}
}

func (t *Tracker) RecordTurn(ctx context.Context, rec protocol.TokenTelemetryRecord) protocol.TokenTelemetryRecord {
	t.mu.Lock()
	defer t.mu.Unlock()

	if rec.ID == "" {
		rec.ID = fmt.Sprintf("telem-%d", time.Now().UnixNano())
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}

	// Price by the record's own model rather than one fixed pair, so an Opus turn
	// and a Haiku turn are not billed identically.
	if rec.CostUSD == 0 {
		p := priceFor(rec.ModelName)
		rec.CostUSD = (float64(rec.PromptTokens) * p.prompt) + (float64(rec.CompletionTokens) * p.completion)
	}

	// Fold into the running aggregates first, so trimming below cannot drop a
	// turn's spend from the totals.
	t.agg.TotalPromptTokens += int64(rec.PromptTokens)
	t.agg.TotalCompletionTokens += int64(rec.CompletionTokens)
	t.agg.TotalCachedTokens += int64(rec.CachedTokens)
	t.agg.TotalCostUSD += rec.CostUSD
	t.agg.TurnsCount++
	t.sumLatency += int64(rec.LatencyMS)

	t.records = append(t.records, rec)
	if len(t.records) > maxRecords {
		// Drop the oldest, keeping the slice bounded. Copy down rather than
		// reslicing so the backing array does not pin the trimmed records.
		keep := t.records[len(t.records)-maxRecords:]
		t.records = append(make([]protocol.TokenTelemetryRecord, 0, maxRecords), keep...)
	}
	return rec
}

type FinancialSummary struct {
	TotalPromptTokens     int64   `json:"total_prompt_tokens"`
	TotalCompletionTokens int64   `json:"total_completion_tokens"`
	TotalCachedTokens     int64   `json:"total_cached_tokens"`
	TotalCostUSD          float64 `json:"total_cost_usd"`
	AvgLatencyMS          int     `json:"avg_latency_ms"`
	TurnsCount            int     `json:"turns_count"`
}

func (t *Tracker) GetSummary(ctx context.Context) FinancialSummary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Read straight from the running aggregates: they cover every turn ever
	// recorded, not just the retained window.
	sum := t.agg
	if sum.TurnsCount > 0 {
		sum.AvgLatencyMS = int(t.sumLatency / int64(sum.TurnsCount))
	}
	return sum
}

func (t *Tracker) ListRecords(ctx context.Context, limit int) []protocol.TokenTelemetryRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.records) {
		limit = len(t.records)
	}
	out := make([]protocol.TokenTelemetryRecord, limit)
	copy(out, t.records[len(t.records)-limit:])
	return out
}
