package store

import (
	"context"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Cost and latency accounting, made durable.
//
// The token_telemetry table has existed since migration 0007 and nothing ever
// wrote a row to it: the tracker was process memory only. So the financial
// dashboard was not merely missing its cached column — the whole thing reset to
// $0.00 on every deploy, which is the one number an operator wants to survive a
// restart. These are the write-through and the boot-time rehydration.

// InsertTelemetryTurn records one model turn's tokens, cost and latency.
//
// ON CONFLICT DO NOTHING rather than an update: a turn is an immutable fact
// about something that already happened, and the only way the same id arrives
// twice is a replay.
func (s *Store) InsertTelemetryTurn(ctx context.Context, rec protocol.TokenTelemetryRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO token_telemetry(id,task_id,instance_id,archetype_id,provider_id,model_name,
             prompt_tokens,completion_tokens,cached_tokens,cost_usd,latency_ms,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
         ON CONFLICT (id) DO NOTHING`,
		rec.ID, rec.TaskID, rec.InstanceID, nullIfEmpty(rec.ArchetypeID), rec.ProviderID,
		rec.ModelName, rec.PromptTokens, rec.CompletionTokens, rec.CachedTokens,
		rec.CostUSD, rec.LatencyMS, rec.CreatedAt)
	return norm(err)
}

// TelemetryTotals is the all-time aggregate, computed in the database.
//
// Summed by SQL rather than by loading every row: a long-lived fleet has
// millions of turns and the summary needs six numbers, so hydrating the tracker
// with the whole table to add it up in Go would be slower than the query and
// would not fit in memory.
type TelemetryTotals struct {
	PromptTokens     int64
	CompletionTokens int64
	CachedTokens     int64
	CostUSD          float64
	LatencyMS        int64
	Turns            int64
}

func (s *Store) TelemetryTotals(ctx context.Context) (TelemetryTotals, error) {
	var t TelemetryTotals
	// COALESCE throughout: every one of these aggregates is NULL on an empty
	// table, and scanning NULL into an int64 is an error rather than a zero.
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
                COALESCE(SUM(cached_tokens),0), COALESCE(SUM(cost_usd),0),
                COALESCE(SUM(latency_ms),0), COUNT(*)
           FROM token_telemetry`).
		Scan(&t.PromptTokens, &t.CompletionTokens, &t.CachedTokens,
			&t.CostUSD, &t.LatencyMS, &t.Turns)
	return t, norm(err)
}

// RecentTelemetryTurns returns the newest turns for the per-turn drill-down.
func (s *Store) RecentTelemetryTurns(ctx context.Context, limit int) ([]protocol.TokenTelemetryRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id,task_id,instance_id,COALESCE(archetype_id,''),provider_id,model_name,
                prompt_tokens,completion_tokens,cached_tokens,cost_usd,latency_ms,created_at
           FROM token_telemetry ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()

	out := make([]protocol.TokenTelemetryRecord, 0, limit)
	for rows.Next() {
		var r protocol.TokenTelemetryRecord
		if err := rows.Scan(&r.ID, &r.TaskID, &r.InstanceID, &r.ArchetypeID, &r.ProviderID,
			&r.ModelName, &r.PromptTokens, &r.CompletionTokens, &r.CachedTokens,
			&r.CostUSD, &r.LatencyMS, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
