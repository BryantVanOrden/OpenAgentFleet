package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/mcp"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/memory"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/pipeline"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/telemetry"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// telemetryStore adapts *store.Store to telemetry.Store.
//
// Only the totals need adapting: telemetry declares its own StoredTotals so the
// telemetry package does not import the store package, which would be an import
// cycle the first time the store wants to price anything. The other two methods
// pass straight through.
type telemetryStore struct{ db *store.Store }

func (t telemetryStore) InsertTelemetryTurn(ctx context.Context, rec protocol.TokenTelemetryRecord) error {
	return t.db.InsertTelemetryTurn(ctx, rec)
}

func (t telemetryStore) RecentTelemetryTurns(ctx context.Context, limit int) ([]protocol.TokenTelemetryRecord, error) {
	return t.db.RecentTelemetryTurns(ctx, limit)
}

func (t telemetryStore) TelemetryTotals(ctx context.Context) (telemetry.StoredTotals, error) {
	got, err := t.db.TelemetryTotals(ctx)
	if err != nil {
		return telemetry.StoredTotals{}, err
	}
	return telemetry.StoredTotals{
		PromptTokens:     got.PromptTokens,
		CompletionTokens: got.CompletionTokens,
		CachedTokens:     got.CachedTokens,
		CostUSD:          got.CostUSD,
		LatencyMS:        got.LatencyMS,
		Turns:            got.Turns,
	}, nil
}

// StartBackground gives the fleet-wide singletons their persistence and starts
// the cron engine. Call it once, after Migrate, before serving.
//
// Everything it wires was previously process-local: peer messages, episodic
// memory and the trigger configuration all lived in maps that a restart
// emptied, against tables that already existed and nothing read.
func (s *Server) StartBackground(ctx context.Context) {
	if s.db != nil {
		if err := s.loadTriggers(ctx); err != nil {
			s.logger().Error("triggers not loaded; webhooks and cron start empty", "err", err)
		}
		if err := vault.GlobalBus.AttachStore(ctx, s.db, s.logger()); err != nil {
			s.logger().Error("peer message history not loaded; the bus stays in-memory", "err", err)
		}
		if err := vault.GlobalBus.AttachConversationStore(ctx, s.db); err != nil {
			s.logger().Error("conversations not loaded; threads stay in-memory", "err", err)
		}
		if err := pipeline.GlobalEngine.AttachStore(ctx, s.db); err != nil {
			s.logger().Error("pipelines not loaded; they stay in-memory", "err", err)
		}
		if err := memory.GlobalEngine.AttachStore(ctx, s.db, s.logger()); err != nil {
			s.logger().Error("episodic memory not loaded; the index stays in-memory", "err", err)
		}
		if err := telemetry.GlobalTracker.AttachStore(ctx, telemetryStore{s.db}, s.logger()); err != nil {
			s.logger().Error("spend history not loaded; the cost dashboard starts at zero", "err", err)
		}
		if err := mcp.GlobalMCP.AttachStore(ctx, s.db, s.logger()); err != nil {
			s.logger().Error("MCP registrations not loaded; the hub starts empty", "err", err)
		}
		if err := globalSwarmCoordinator.AttachStore(ctx, s.db); err != nil {
			s.logger().Error("swarm missions not loaded; the blackboard starts empty", "err", err)
		}
	}

	// Gives the swarm coordinator a way to start real work. Without it,
	// creating a swarm is refused rather than recording a mission nothing will
	// ever act on.
	s.wireSwarms()
	// Gives the pipeline engine a way to actually run a node. Without this it
	// refuses to start a run rather than reporting invented success.
	pipeline.GlobalEngine.SetNodeRunner(s.runPipelineNode)
	// And only now — with both the store and the runner attached — resume any
	// runs the previous process died under. Before this existed, a restart left
	// every in-flight run recorded as `running` forever and its settled node
	// results lost with the process.
	if n := pipeline.GlobalEngine.ResumeInterrupted(ctx); n > 0 {
		s.logger().Info("resuming pipeline runs interrupted by the last shutdown", "count", n)
	}

	// Real embeddings for episodic memory, when a provider can produce them.
	//
	// In the background and after the store is attached, in that order: probing
	// a provider is a network round trip that must not delay boot, and attaching
	// the embedder triggers a re-embed of what was just hydrated.
	go s.attachMemoryEmbedder(ctx)

	// Live model pricing, refreshed daily. Off with PRICING_REFRESH=off; the
	// built-in list-price table remains the fallback either way.
	go telemetry.StartPriceRefresher(ctx, s.logger())

	go s.RunCronScheduler(ctx)
	// Idle agents answer messages too; without this a broadcast to a fleet
	// with nothing running is met with silence.
	go s.RunPeerResponder(ctx)
	// Finishing a part of a shared job wakes whoever the next part belongs
	// to. Without this an agent publishes and stops, and the colleague who
	// would review it never hears.
	go s.watchForHandoffs(ctx)
	// And says so when a job cannot start at all, rather than leaving agents
	// waiting on work nobody was asked to make.
	go s.watchForStalledJobs(ctx)
}

// attachMemoryEmbedder gives episodic memory real semantic search if it can.
//
// The index scored with a 128-dimensional hashed bag of words, which matches a
// memory when the query reuses its words and misses it otherwise — so "how do I
// sign in to the billing portal" never found "logged into the invoicing site
// with the shared credential", which is the recall a fleet memory exists for.
//
// A deployment with no embedding-capable provider is a normal choice, not an
// error: it keeps the hashed vectors, and /api/memories reports which scheme is
// in use so nobody assumes semantic search they do not have.
func (s *Server) attachMemoryEmbedder(ctx context.Context) {
	if s.models == nil {
		return
	}
	probe, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	emb, err := s.models.BestEmbedder(probe)
	if err != nil {
		if errors.Is(err, connectors.ErrNoEmbedder) {
			s.logger().Info("no embedding-capable provider configured; " +
				"episodic memory keeps its hashed keyword index")
			return
		}
		s.logger().Warn("could not choose an embedding provider; "+
			"episodic memory keeps its hashed keyword index", "err", err)
		return
	}

	s.logger().Info("episodic memory using real embeddings", "model", emb.Model())
	// Not bound to the probe context, which is about to be cancelled: the
	// re-embed of the existing index runs well past this function.
	memory.GlobalEngine.SetEmbedder(context.WithoutCancel(ctx), emb)
}

// RunCronScheduler fires due triggers once a minute until ctx is cancelled.
//
// A minute is cron's own resolution, so nothing finer buys anything. The first
// tick is aligned to the top of a minute: an unaligned ticker started at
// :00:37 would evaluate at :01:37, :02:37 and so on, which still fires every
// minute exactly once but puts every trigger's real fire time a random offset
// after the minute the operator asked for.
func (s *Server) RunCronScheduler(ctx context.Context) {
	align := time.NewTimer(time.Until(time.Now().Truncate(time.Minute).Add(time.Minute)))
	defer align.Stop()
	select {
	case <-ctx.Done():
		return
	case <-align.C:
	}

	t := time.NewTicker(time.Minute)
	defer t.Stop()
	s.fireDueTriggers(ctx, time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.fireDueTriggers(ctx, now.UTC())
		}
	}
}

// fireDueTriggers dispatches every trigger due in now's minute.
func (s *Server) fireDueTriggers(ctx context.Context, now time.Time) {
	minute := now.UTC().Truncate(time.Minute)
	for _, cr := range dueCronTriggers(minute) {
		// The database is the arbiter when there is one: the guarded UPDATE
		// fails for anyone who did not get there first, so a second
		// orchestrator against the same database cannot double-fire a nightly
		// scan. Its failure is not fatal here -- the in-memory claim above
		// already stops this process repeating within the minute.
		if s.db != nil {
			won, err := s.db.ClaimCronRun(ctx, cr.ID, minute)
			if err != nil {
				s.logger().Warn("cron run not recorded; firing anyway", "trigger", cr.Name, "err", err)
			} else if !won {
				continue
			}
		}

		goal := renderGoal(cr.GoalTemplate, nil)
		task, err := s.dispatchTrigger(ctx, cr.TargetInstanceID, cr.TargetArchetype, goal, "cron:"+cr.Name)
		if err != nil {
			// Not retried inside the minute, and not reset so it retries next
			// minute: a nightly scan whose bot is down should report one
			// failure at 02:00, not sixty.
			msg := "cron trigger could not be dispatched"
			if errors.Is(err, errNoTarget) {
				msg = "cron trigger has nothing to run on"
			}
			s.logger().Warn(msg, "trigger", cr.Name, "schedule", cr.ScheduleCron, "err", err)
			continue
		}
		s.logger().Info("cron trigger fired", "trigger", cr.Name, "task", task.ID, "instance", task.InstanceID)
	}
}

// dueCronTriggers returns the active triggers whose schedule matches `minute`
// and which have not already run in it, marking each as run as it goes.
//
// The claim is part of the selection on purpose: it is what makes a trigger
// fire once rather than on every tick of the minute it is due in, and -- since
// last_run_at is persisted -- what stops a restart at 02:00:30 from re-running
// everything that already fired at 02:00:00.
func dueCronTriggers(minute time.Time) []CronTriggerRecord {
	cronMu.Lock()
	defer cronMu.Unlock()

	var due []CronTriggerRecord
	for id, cr := range crons {
		if !cr.Active {
			continue
		}
		if !cronSchedules[id].Matches(minute) {
			continue
		}
		// Already ran in this minute (or a later one, after a clock jump).
		if cr.LastRunAt != nil && !cr.LastRunAt.Before(minute) {
			continue
		}
		ran := minute
		cr.LastRunAt = &ran
		crons[id] = cr
		due = append(due, cr)
	}
	return due
}
