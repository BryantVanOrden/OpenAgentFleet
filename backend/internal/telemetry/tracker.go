package telemetry

import (
	"context"
	"fmt"
	"log/slog"
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
	// cachedDiscount is the fraction of the prompt rate charged for a token the
	// provider served from its prompt cache. OpenAI and Anthropic both bill a
	// cache read at 10% of input; Gemini charges 25%. Zero means "use
	// defaultCachedDiscount", so entries that predate this keep working.
	cachedDiscount float64
}

// defaultCachedDiscount is the 10% cache-read rate OpenAI and Anthropic
// both charge, applied to any model whose table entry does not say otherwise.
const defaultCachedDiscount = 0.10

// modelPrices is a coarse per-model price table matched by substring, because a
// provider row's model name carries a version suffix (e.g.
// "claude-sonnet-4-5-20250929", "gpt-4o-2024-08-06") that we do not want to
// enumerate. First match wins; anything unrecognised falls back to
// defaultPrice. These are list prices and only approximate — the point is that
// billing an Opus turn at Sonnet rates (the previous single hardcoded pair) was
// wrong by 5x, not that this is invoice-accurate.
var modelPrices = map[string]modelPrice{
	"claude-opus":   {prompt: 0.000015, completion: 0.000075},
	"claude-sonnet": {prompt: 0.000003, completion: 0.000015},
	"claude-haiku":  {prompt: 0.0000008, completion: 0.000004},
	"gpt-4o-mini":   {prompt: 0.00000015, completion: 0.0000006},
	"gpt-4o":        {prompt: 0.0000025, completion: 0.00001},
	"gpt-4":         {prompt: 0.00003, completion: 0.00006},
	"o1":            {prompt: 0.000015, completion: 0.00006},
	// Gemini's context cache is charged at 25% of input, not 10%.
	"gemini-1.5-pro": {prompt: 0.00000125, completion: 0.000005, cachedDiscount: 0.25},
	"gemini":         {prompt: 0.0000003, completion: 0.0000012, cachedDiscount: 0.25},
}

// defaultPrice is the fallback for an unrecognised model. It matches the old
// hardcoded pair so nothing that was already priced changes silently.
var defaultPrice = modelPrice{prompt: 0.000003, completion: 0.000015}

func priceFor(model string) modelPrice {
	// A live price wins, but only on an exact normalised match — fuzzy matching
	// against a hosted catalogue is how a local model gets billed at someone
	// else's rate. See prices.go for where the live table comes from.
	if p, ok := livePrices.lookup(model); ok {
		return p
	}
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

// Store is the durable half: one row per turn, plus the all-time totals.
//
// An interface rather than *store.Store so the tracker works with no database,
// which is how the tests build it and how GlobalTracker behaves until the server
// attaches a store on boot.
type Store interface {
	InsertTelemetryTurn(ctx context.Context, rec protocol.TokenTelemetryRecord) error
	TelemetryTotals(ctx context.Context) (StoredTotals, error)
	RecentTelemetryTurns(ctx context.Context, limit int) ([]protocol.TokenTelemetryRecord, error)
}

// StoredTotals mirrors store.TelemetryTotals. Declared here so the telemetry
// package does not import the store package — the dependency runs the other
// way for every other subsystem and reversing it for this one would make an
// import cycle the moment the store wants to price anything.
type StoredTotals struct {
	PromptTokens     int64
	CompletionTokens int64
	CachedTokens     int64
	CostUSD          float64
	LatencyMS        int64
	Turns            int64
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

	// store is optional. When nil the tracker is in-memory only, and every
	// figure on the cost dashboard resets to zero on the next deploy — which is
	// what it did for every fleet before this was wired up.
	store Store
	log   *slog.Logger
}

var GlobalTracker = NewTracker()

func NewTracker() *Tracker {
	return &Tracker{
		records: make([]protocol.TokenTelemetryRecord, 0),
	}
}

// AttachStore makes spend durable: the all-time totals and the recent turns are
// loaded back, and every turn afterwards is written through.
//
// The totals come from SQL rather than from replaying the loaded rows, because
// the retained window is a few hundred turns and the lifetime total is millions
// — adding up only what was hydrated would report a fleet's entire history as
// whatever happened since the last restart, which is the bug this replaces
// wearing a different hat.
func (t *Tracker) AttachStore(ctx context.Context, st Store, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	totals, err := st.TelemetryTotals(ctx)
	if err != nil {
		return err
	}
	recent, err := st.RecentTelemetryTurns(ctx, maxRecords/40) // 500
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.store = st
	t.log = log

	// Anything recorded before the attach is already in the aggregates and is
	// not yet in the table, so it is added on top rather than replaced.
	t.agg.TotalPromptTokens += totals.PromptTokens
	t.agg.TotalCompletionTokens += totals.CompletionTokens
	t.agg.TotalCachedTokens += totals.CachedTokens
	t.agg.TotalCostUSD += totals.CostUSD
	t.agg.TurnsCount += int(totals.Turns)
	t.sumLatency += totals.LatencyMS

	// Oldest first, matching the append order RecordTurn uses, so the drill-down
	// reads chronologically either way it was populated.
	for i := len(recent) - 1; i >= 0; i-- {
		t.records = append(t.records, recent[i])
	}
	if len(t.records) > maxRecords {
		keep := t.records[len(t.records)-maxRecords:]
		t.records = append(make([]protocol.TokenTelemetryRecord, 0, maxRecords), keep...)
	}
	return nil
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

	// CachedTokens is a subset of PromptTokens, so the cached part is billed at
	// the cache rate and only the remainder at the full input rate. Clamped
	// because a provider reporting more cached than prompt tokens (a
	// mis-mapped field on some OpenAI-compatible server) would otherwise make
	// the uncached count negative and credit the fleet money.
	if rec.CachedTokens < 0 {
		rec.CachedTokens = 0
	}
	if rec.CachedTokens > rec.PromptTokens {
		rec.CachedTokens = rec.PromptTokens
	}

	// Price by the record's own model rather than one fixed pair, so an Opus turn
	// and a Haiku turn are not billed identically.
	if rec.CostUSD == 0 {
		p := priceFor(rec.ModelName)
		discount := p.cachedDiscount
		if discount <= 0 {
			discount = defaultCachedDiscount
		}
		uncached := rec.PromptTokens - rec.CachedTokens
		rec.CostUSD = (float64(uncached) * p.prompt) +
			(float64(rec.CachedTokens) * p.prompt * discount) +
			(float64(rec.CompletionTokens) * p.completion)
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
	st, log := t.store, t.log

	if st != nil {
		// In a goroutine, and deliberately: RecordTurn is called from the agent
		// loop between a model turn and the next observation, holds the
		// tracker's write lock, and a database round trip here would put every
		// running agent behind Postgres on the hot path. The turn is already
		// counted in the aggregates above, so a failed write costs the row, not
		// the total.
		go func() {
			// Detached from the turn's context: the task can finish, and cancel
			// its context, before this write lands.
			if err := st.InsertTelemetryTurn(context.WithoutCancel(ctx), rec); err != nil && log != nil {
				log.Warn("turn cost not persisted", "id", rec.ID, "task", rec.TaskID, "err", err)
			}
		}()
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
	// Pricing says where the rates came from and how fresh they are, so the
	// dashboard states its own accuracy instead of implying invoice precision.
	Pricing PricingInfo `json:"pricing"`
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
	sum.Pricing = livePrices.info()
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
