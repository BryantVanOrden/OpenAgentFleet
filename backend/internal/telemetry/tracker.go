package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Tracker records token usage, API cost calculations, and response latencies.
type Tracker struct {
	mu      sync.RWMutex
	records []protocol.TokenTelemetryRecord
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

	// Calculate approximate USD cost ($0.003/1k prompt, $0.015/1k completion)
	if rec.CostUSD == 0 {
		rec.CostUSD = (float64(rec.PromptTokens) * 0.000003) + (float64(rec.CompletionTokens) * 0.000015)
	}

	t.records = append(t.records, rec)
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

	var sum FinancialSummary
	var totalLatency int64

	sum.TurnsCount = len(t.records)
	for _, r := range t.records {
		sum.TotalPromptTokens += int64(r.PromptTokens)
		sum.TotalCompletionTokens += int64(r.CompletionTokens)
		sum.TotalCachedTokens += int64(r.CachedTokens)
		sum.TotalCostUSD += r.CostUSD
		totalLatency += int64(r.LatencyMS)
	}

	if sum.TurnsCount > 0 {
		sum.AvgLatencyMS = int(totalLatency / int64(sum.TurnsCount))
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
