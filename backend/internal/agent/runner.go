package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/artifacts"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/bus"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/memory"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/telemetry"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Notifier delivers an alert to the operator's phone.
type Notifier interface {
	Send(ctx context.Context, a protocol.Alert) error
}

// Runner owns every in-flight task. One goroutine per task; the map exists so
// the API can cancel, and so a restart can tell which tasks it has resumed.
type Runner struct {
	cfg     *config.Config
	db      *store.Store
	models  *connectors.Registry
	bus     *bus.Bus
	notify  Notifier
	art     artifacts.Store
	refiner *Refiner
	log     *slog.Logger

	// placer puts a file inside a sandbox, through the orchestrator's channel
	// rather than the agent's, so it works with shell access turned off.
	placer FilePlacer

	mu      sync.Mutex
	running map[string]context.CancelFunc
	// published counts publishes per task per item name, to notice a run that
	// is republishing the same thing instead of finishing.
	published map[string]map[string]int
	// Sub-agent budget bookkeeping; see spawn_budget.go.
	depth    map[string]int
	children map[string]int

	// coordSpace caches the detected coordinate convention per provider, keyed
	// by provider id. Detection costs one model round trip, so it happens once
	// per provider per process rather than once per step.
	coordSpace sync.Map
}

// FilePlacer writes a file inside an instance's sandbox and shows it.
type FilePlacer interface {
	PlaceFile(ctx context.Context, instanceID, path string, content []byte) error
	OpenInBrowser(ctx context.Context, instanceID, url string) error
}

func NewRunner(
	cfg *config.Config,
	db *store.Store,
	models *connectors.Registry,
	b *bus.Bus,
	notifier Notifier,
	art artifacts.Store,
	log *slog.Logger,
) *Runner {
	return &Runner{
		cfg: cfg, db: db, models: models, bus: b, notify: notifier, art: art,
		refiner:  NewRefiner(db, models, b, log),
		log:      log,
		running:  map[string]context.CancelFunc{},
		depth:    map[string]int{},
		children: map[string]int{},
	}
}

func (r *Runner) Refiner() *Refiner {
	return r.refiner
}

// Start launches a task. It returns as soon as the goroutine is scheduled.
func (r *Runner) Start(parent context.Context, task *protocol.Task) error {
	r.mu.Lock()
	if _, busy := r.running[task.ID]; busy {
		r.mu.Unlock()
		return fmt.Errorf("task %s is already running", task.ID)
	}
	// Detach from the request context: the task outlives the HTTP call.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	r.running[task.ID] = cancel
	r.mu.Unlock()

	go func() {
		defer func() {
			// A panic in the loop must not take the orchestrator down with it,
			// and the task must not be left showing "running" forever.
			if rec := recover(); rec != nil {
				r.log.Error("agent panicked", "task", task.ID, "panic", rec)
				r.fail(context.WithoutCancel(ctx), task, fmt.Sprintf("agent panicked: %v", rec))
			}
			r.mu.Lock()
			delete(r.running, task.ID)
			// The publish counter is per run; keeping it would leak a map
			// entry for every task the orchestrator ever ran.
			delete(r.published, task.ID)
			r.mu.Unlock()
			r.forgetSpawn(task.ID)
			cancel()
		}()
		r.loop(ctx, task)
	}()
	return nil
}

func (r *Runner) Cancel(taskID string) bool {
	r.mu.Lock()
	cancel, ok := r.running[taskID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (r *Runner) IsRunning(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[taskID]
	return ok
}

// ResumeInterrupted re-queues tasks that were mid-flight when the orchestrator
// last stopped. They restart from the current screen, which is the right
// behaviour for a GUI agent: the desktop is the state, not our step counter.
func (r *Runner) ResumeInterrupted(ctx context.Context) {
	tasks, err := r.db.ResumeableTasks(ctx)
	if err != nil {
		r.log.Error("resume: list tasks", "err", err)
		return
	}
	for i := range tasks {
		t := tasks[i]
		inst, err := r.db.Instance(ctx, t.InstanceID)
		if err != nil || inst.State != protocol.InstanceRunning {
			_ = r.db.UpdateTaskState(ctx, t.ID, protocol.TaskFailed, t.Step,
				"orchestrator restarted while the sandbox was unavailable", "")
			continue
		}
		// A task that was waiting on a person is resumed without one: the
		// question went with the process that asked it, and nobody can answer
		// an alert whose waiter no longer exists. Its alerts are closed so
		// they stop showing as blocking something that is running again.
		if t.State == protocol.TaskAwaitingHuman {
			if err := r.db.ResolveTaskAlerts(ctx, t.ID,
				"The orchestrator restarted while this was waiting; the agent carried on."); err != nil {
				r.log.Warn("could not close alerts for a resumed task", "task", t.ID, "err", err)
			}
			r.log.Info("resuming a task that was waiting for a person", "task", t.ID, "step", t.Step)
		} else {
			r.log.Info("resuming task", "task", t.ID, "step", t.Step)
		}
		_ = r.Start(ctx, &t)
	}
}

// ---------------------------------------------------------------- the loop ---

func (r *Runner) loop(ctx context.Context, task *protocol.Task) {
	inst, err := r.db.Instance(ctx, task.InstanceID)
	if err != nil {
		r.fail(ctx, task, "instance not found: "+err.Error())
		return
	}
	if inst.State != protocol.InstanceRunning {
		r.fail(ctx, task, "instance is "+string(inst.State)+", not running")
		return
	}

	var skill *protocol.Skill
	if task.SkillID != "" {
		if skill, err = r.db.Skill(ctx, task.SkillID); err != nil {
			r.log.Warn("skill not found, running goal-only", "skill", task.SkillID)
		}
	}

	sc := NewSandboxClient(inst.AgentdURL)
	mountedTools := map[string]protocol.MountedTool{}
	defer func() {
		// Reversible Disposer: unmount any remaining dynamic tools to leave sandbox clean
		if len(mountedTools) > 0 {
			cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			for name := range mountedTools {
				_, _ = sc.Act(cleanCtx, protocol.Action{
					Action:   protocol.ActUnmountTool,
					ToolName: name,
				})
			}
		}
	}()

	task.State = protocol.TaskRunning
	_ = r.db.UpdateTaskState(ctx, task.ID, protocol.TaskRunning, task.Step, "", "")
	r.bus.Emit("task.state", inst.ID, task.ID, task)

	var (
		history     []turnSummary
		lastHash    string
		lastAction  protocol.ActionKind
		sameCount   int
		stalls      int
		parseErrors int
		humanReply  string
	)

	for task.Step < task.MaxSteps {
		if ctx.Err() != nil {
			r.cancelled(ctx, task)
			return
		}
		stepStart := time.Now()

		obsCtx, cancel := context.WithTimeout(ctx, r.cfg.StepTimeout)
		obs, err := sc.Observe(obsCtx, ObserveOptions{
			Screenshot: true,
			A11y:       true,
			MaxWidth:   r.cfg.ScreenshotMaxW,
			Quality:    70,
		})
		cancel()
		if err != nil {
			r.fail(ctx, task, "could not observe the desktop: "+err.Error())
			return
		}

		// Stall detection: an unchanged screen after an action means the agent is
		// clicking into the void. Escalate rather than burn the step budget.
		//
		// Only actions that are supposed to move the screen count. An agent
		// working the shared catalogue publishes and reads over the API and
		// never touches its desktop, so every one of those steps looked like a
		// stall: one run filed twenty-six "appears stuck" alerts in half an
		// hour, each of them a push notification, while the agent was doing
		// exactly what it had been asked to do and doing it correctly.
		if obs.Hash != "" && obs.Hash == lastHash && movesScreen(lastAction) {
			sameCount++
		} else if movesScreen(lastAction) {
			sameCount = 0
		}
		lastHash = obs.Hash

		if sameCount >= r.cfg.StallThreshold {
			// An agent that has stalled this many times and decided for itself
			// every time is not going to finish. Left alone it spends its whole
			// step budget alternating between stalling and being told to carry
			// on, and every round of that is another alert on the operator's
			// phone. Stop, and say so once.
			stalls++
			if stalls > maxStallEscalations {
				r.fail(ctx, task, fmt.Sprintf(
					"stalled %d times without making progress; last action: %s",
					stalls-1, lastHistory(history)))
				return
			}
			reply, err := r.escalate(ctx, task, inst, protocol.AlertStalled, "critical",
				"Agent appears stuck",
				fmt.Sprintf("The screen has not changed across %d actions on %q.\nLast action: %s",
					sameCount, inst.Name, lastHistory(history)),
				obs)
			if err != nil {
				// Cancelling a task that is waiting on a human is a
				// cancellation, not a failure to reach anyone. Reporting it as
				// the latter told the operator their own action was an error.
				if errors.Is(err, context.Canceled) || ctx.Err() != nil {
					r.cancelled(ctx, task)
					return
				}
				r.fail(ctx, task, "stalled and could not reach the operator: "+err.Error())
				return
			}
			humanReply = reply
			sameCount = 0
			continue
		}

		obsKey := r.storeObservation(ctx, task, obs)

		// This bot's own chain, resolved for the role this turn actually plays:
		// perceiving the screen and choosing the next action. A combination
		// sends this to whichever model it assigns to the hands.
		resp, err := r.models.CompleteRole(ctx,
			connectors.PreferredChain(task.ProviderID, inst.ProviderIDs),
			protocol.RoleVision, connectors.Request{
				System:   buildSystem(inst, mountedTools),
				JSONOnly: true,
				Messages: []connectors.Message{{
					Role: connectors.RoleUser,
					Text: buildTurn(task, skill, obs, history, humanReply,
						r.peerContext(ctx, inst.ID)),
					Image:     obs.ScreenshotB64,
					ImageMime: "image/webp",
				}},
			})
		humanReply = "" // consumed
		if err != nil {
			if errors.Is(err, connectors.ErrNoProvider) {
				r.fail(ctx, task, err.Error())
				return
			}
			// A cancelled run is not a broken provider.
			//
			// Stopping a task mid-inference surfaced as "every model provider
			// failed: context canceled", which sends whoever reads it to check
			// engines that are working perfectly. The providers did not fail;
			// somebody pressed stop.
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				r.log.Info("task stopped mid-step", "task", task.ID)
				_ = r.db.UpdateTaskState(context.WithoutCancel(ctx), task.ID,
					protocol.TaskCancelled, task.Step, "stopped while it was thinking", "")
				r.closeTaskAlerts(ctx, task.ID, "the task was stopped")
				return
			}
			r.fail(ctx, task, "every model provider failed: "+err.Error())
			return
		}

		action, err := ParseAction(resp.Text)
		if err != nil {
			parseErrors++
			if parseErrors >= 3 {
				r.fail(ctx, task, "model would not produce a valid action: "+err.Error())
				return
			}
			history = append(history, turnSummary{
				Step:    task.Step + 1,
				Action:  "(invalid reply)",
				Outcome: err.Error() + " — reply with one JSON object only",
			})
			continue
		}
		parseErrors = 0
		task.Step++

		r.bus.Emit("task.step", inst.ID, task.ID, map[string]any{
			"step": task.Step, "action": action, "provider": resp.Provider, "model": resp.Model,
		})

		lastAction = action.Action
		outcome, terminal := r.execute(ctx, task, inst, sc, action, obs, mountedTools)

		_ = r.db.AppendStep(ctx, &protocol.StepRecord{
			TaskID:       task.ID,
			Step:         task.Step,
			Action:       action,
			Observation:  obsKey,
			Outcome:      outcome,
			DurationMS:   time.Since(stepStart).Milliseconds(),
			PromptTokens: resp.PromptTokens,
			OutTokens:    resp.OutputTokens,
		})
		// Record the financial telemetry for this turn. The token counts already
		// flow into the step audit row above; RecordTurn is what turns them into
		// fleet-wide cost and latency figures. It had zero callers, so the whole
		// financial dashboard read as free — every model call cost $0. Priced by
		// resp.Model inside the tracker so mixed-model fleets bill correctly.
		telemetry.GlobalTracker.RecordTurn(ctx, protocol.TokenTelemetryRecord{
			TaskID:           task.ID,
			InstanceID:       inst.ID,
			ArchetypeID:      inst.ArchetypeID,
			ProviderID:       task.ProviderID,
			ModelName:        resp.Model,
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.OutputTokens,
			// A subset of PromptTokens, not an addition: the tracker prices the
			// uncached remainder at the full input rate and this at the cache
			// rate. Nothing populated it before, so the cached column read zero
			// on every fleet and the input spend was overstated by whatever the
			// provider had actually cached — on an agent loop resending the same
			// system prompt every turn, most of it.
			CachedTokens: resp.CachedTokens,
			LatencyMS:    int(resp.Latency.Milliseconds()),
		})

		history = append(history, turnSummary{Step: task.Step, Action: summarise(action), Outcome: outcome})
		if len(history) > 24 {
			history = history[len(history)-24:]
		}
		_ = r.db.UpdateTaskState(ctx, task.ID, protocol.TaskRunning, task.Step, "", "")

		switch terminal {
		case terminalDone:
			r.succeed(ctx, task, inst, action.Summary)
			return
		case terminalFail:
			r.fail(ctx, task, firstNonEmpty(action.Summary, outcome, "agent gave up"))
			return
		case terminalCancelled:
			r.cancelled(ctx, task)
			return
		case terminalHumanReply:
			humanReply = outcome
		}
	}

	r.fail(ctx, task, fmt.Sprintf("step budget exhausted (%d steps)", task.MaxSteps))
}

type terminalKind int

const (
	terminalNone terminalKind = iota
	terminalDone
	terminalFail
	terminalCancelled
	terminalHumanReply
)

// execute performs one action and returns a short outcome string that goes both
// into the audit log and back into the next prompt.
func (r *Runner) execute(
	ctx context.Context,
	task *protocol.Task,
	inst *protocol.Instance,
	sc *SandboxClient,
	a protocol.Action,
	obs *protocol.Observation,
	mountedTools map[string]protocol.MountedTool,
) (string, terminalKind) {
	switch a.Action {
	case protocol.ActDone:
		return "done: " + clip(a.Summary, 200), terminalDone

	case protocol.ActFail:
		return "failed: " + clip(a.Summary, 200), terminalFail

	// remember/recall are handled here rather than in agentd: memory outlives
	// the sandbox and is queried from the API side, and agentd only ever sees
	// its own desktop.
	//
	// Each agent remembers into its own namespace. One shared pool meant every
	// agent recalled every other agent's notes, so a scout's finding about its
	// own machine came back as guidance to an unrelated bot mid-task. Recall
	// still reads the shared "fleet" pool alongside, so deliberately fleet-wide
	// knowledge reaches everyone.
	//
	// These were advertised in the system prompt with nothing behind them, so a
	// model that followed the instruction got "unsupported action" back and
	// burned a step every time it tried.
	case protocol.ActRemember:
		content := firstNonEmpty(a.Text, a.Summary, a.Question)
		if content == "" {
			return "failed: remember needs the text to store", terminalNone
		}
		title := firstNonEmpty(a.Target, clip(content, 60))
		// A note the agent marked as being about the person who asked is
		// attributed to them, so it surfaces when they next turn up rather
		// than to whoever happens to be talking.
		aboutUser := ""
		if a.AboutUser {
			aboutUser = task.OwnerID
		}
		// Shared or private. Every memory used to go to the recording agent's own
		// namespace and nothing ever wrote to a shared one, so the fleet-wide
		// episodic memory the docs describe was permanently empty and one
		// agent's discovery was unreachable by every other one.
		//
		// A note about a person stays private regardless: what one bot learned
		// about a colleague is not the whole fleet's to know, and broadcasting it
		// is a different decision from sharing a technical finding.
		namespace := memory.BotNamespace(inst.ID)
		shared := strings.EqualFold(strings.TrimSpace(a.MemoryScope), "fleet") && aboutUser == ""
		if shared {
			namespace = memory.FleetNamespace
		}

		if err := memory.GlobalEngine.StoreMemory(ctx, protocol.MemoryRecord{
			Namespace:        namespace,
			AboutUserID:      aboutUser,
			Title:            title,
			Content:          content,
			Tags:             []string{"agent", inst.Name},
			SourceTaskID:     task.ID,
			SourceInstanceID: inst.ID,
		}); err != nil {
			return "failed to store memory: " + err.Error(), terminalNone
		}
		if shared {
			return "remembered for the whole fleet: " + clip(title, 120), terminalNone
		}
		return "remembered: " + clip(title, 120), terminalNone

	case protocol.ActRecall:
		query := firstNonEmpty(a.Query, a.Text, a.Target)
		if query == "" {
			return "failed: recall needs a query", terminalNone
		}
		// The bot's own namespace, the shared pool, and the auto-indexed task
		// trajectories. The last one was missing: every completed task's summary
		// was written to "tasks" and no search ever looked there.
		hits := memory.GlobalEngine.SearchScoped(ctx, memory.RecallScope(inst.ID), query, 5)
		if len(hits) == 0 {
			// An explicit miss, not an error: "nothing recorded about X" is
			// information the agent should act on rather than retry.
			return "recalled nothing for " + clip(query, 80) +
				" — no prior run has recorded this", terminalNone
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "recalled %d memor%s:", len(hits), plural(len(hits), "y", "ies"))
		for _, m := range hits {
			fmt.Fprintf(&sb, "\n- %s: %s", m.Title, clip(m.Content, 200))
		}
		return sb.String(), terminalNone

	// share_secret / share_session publish to the fleet vault, which — like
	// episodic memory — is process-wide, not per-sandbox, so agentd can't hold it.
	// Both were advertised in the prompt (with dedicated rules) but rejected by
	// the parser, so an obedient model got "unknown action" and burned a step.
	case protocol.ActPublishWork:
		name := firstNonEmpty(a.WorkName, a.Target)
		if name == "" {
			return "failed: publish_work needs a work_name", terminalNone
		}
		kind := firstNonEmpty(a.WorkKind, protocol.WorkFile)
		if !protocol.ValidWorkKind(kind) {
			return "failed: work_kind must be file, app or workspace", terminalNone
		}
		body := a.Text
		if kind != protocol.WorkWorkspace && strings.TrimSpace(body) == "" {
			return "failed: publish_work needs the content in \"text\"", terminalNone
		}
		mime := "text/plain"
		if kind == protocol.WorkApp {
			mime = "text/html"
		}

		// Resolve the workspace by name so agents can group work without
		// inventing ids and telling each other what they are.
		parentID := ""
		if ws := strings.TrimSpace(a.WorkWorkspace); ws != "" {
			if parent, err := r.workspaceNamed(ctx, ws, inst); err == nil {
				parentID = parent
			}
		}

		item := protocol.WorkItem{
			Name:          name,
			Kind:          kind,
			Description:   clip(a.Thought, 300),
			Content:       body,
			MIME:          mime,
			CreatedBy:     inst.ID,
			CreatedByName: inst.Name,
			OrgID:         protocol.SoleOrg(inst.OrgIDs),
			ParentID:      parentID,
		}
		if err := r.db.PutWorkItem(ctx, &item); err != nil {
			return "failed to publish: " + err.Error(), terminalNone
		}
		r.bus.Emit("work", inst.ID, "", item)

		// Say plainly when an agent is going round in circles.
		//
		// One published the same file ten times in a single run. Each publish
		// looked like progress to the model and was none, and the colleague
		// waiting to test it never got the chance because the run never ended.
		// The count is per task, so publishing several different things is
		// unaffected.
		if n := r.countPublish(task.ID, name); n >= 3 {
			return fmt.Sprintf(
				"published %s %q (version %d). You have now published this same "+
					"item %d times in this run. It is saved -- publishing it again "+
					"changes nothing and nobody is waiting for another copy. Move on "+
					"to something else, or finish with the done action.",
				kind, name, item.Version, n), terminalNone
		}
		return fmt.Sprintf("published %s %q to the shared catalog (version %d)",
			kind, name, item.Version), terminalNone

	case protocol.ActReadWork:
		name := firstNonEmpty(a.WorkName, a.Target)
		if name == "" {
			return "failed: read_work needs a work_name", terminalNone
		}
		all, err := r.db.ListWorkItems(ctx)
		if err != nil {
			return "failed to read the catalog: " + err.Error(), terminalNone
		}
		// Scoped to this bot's departments, the same way the HTTP layer scopes
		// a person's view. Reading the whole catalog and matching on name let
		// a bot in one department read another department's work by guessing
		// what it was called -- the catalog is shared within a department, not
		// across the deployment.
		all = readableWork(all, inst, r.authorOrgs(ctx))
		for _, w := range all {
			if !strings.EqualFold(w.Name, name) {
				continue
			}
			// Truncated rather than refused: a partial read of a large file is
			// more use to the model than an error, and it is told it happened.
			body := w.Content
			suffix := ""
			if len(body) > 12000 {
				body = body[:12000]
				suffix = "\n\n[truncated: the item is " +
					fmt.Sprint(len(w.Content)) + " bytes]"
			}
			// Put a runnable copy on the desktop, not just its source.
			//
			// A tester sent to try an app could read it and nothing more. One
			// spent forty steps guessing at URLs for a page that was never
			// served -- typing http://localhost:8080/rollr.html, searching the
			// web, and landing on a real company's site that happened to share
			// the name -- and never once saw the thing it was testing.
			// The path goes first, before the source.
			//
			// It was appended after the document, and a tester that had
			// already opened a search engine read four thousand bytes of HTML,
			// reached the sentence telling it where the file was, and carried
			// on searching the web regardless. What the agent is meant to do
			// next belongs at the top, where a small model will still be
			// looking.
			opened := ""
			if path, ok := r.materialize(ctx, inst.ID, w); ok {
				opened = fmt.Sprintf("It is already open in the browser on this "+
					"desktop, from %s. Look at the screen and use it. It is not "+
					"published on the web, so do not search for it, and you do "+
					"not need to type the address.\n\n", "file://"+path)
			}
			return fmt.Sprintf("%s%s %q by %s (version %d):\n%s%s",
				opened, w.Kind, w.Name, w.CreatedByName, w.Version, body, suffix), terminalNone
		}

		// Say what IS there. "Not found" sends a model guessing at names.
		var names []string
		for _, w := range all {
			names = append(names, w.Name)
		}
		if len(names) == 0 {
			return "the shared catalog is empty", terminalNone
		}
		return "no work item called " + name + ". The catalog holds: " +
			strings.Join(names, ", "), terminalNone

	// Handled here rather than in the sandbox. agentd's version synthesised a
	// modulated tone, returned it base64'd in a field nothing read, and the
	// sandbox has no audio device or path to the operator anyway. See speech.go.
	case protocol.ActSpeak:
		return r.speak(ctx, task, inst, a), terminalNone

	// MCP tool calls go through the orchestrator, not the sandbox. The
	// connections live here — one per server for the whole fleet rather than
	// one per desktop — and a stdio server is a child process of this binary.
	case protocol.ActCallMCP:
		return r.callMCP(ctx, inst, a), terminalNone

	case protocol.ActShareSecret:
		key := firstNonEmpty(a.SecretKey, a.Target)
		val := firstNonEmpty(a.SecretVal, a.Text)
		if key == "" || val == "" {
			return "failed: share_secret needs a key and a value", terminalNone
		}
		// Scoped to the bot's own department: a secret an agent discovers
		// belongs to the org that agent works for, not to everyone with an
		// account. Scope stays "fleet" so it remains readable by that org's
		// bots, which is what sharing is for.
		vault.GlobalBus.PutSecret(ctx, key, val, "fleet", a.Thought, inst.Name,
			protocol.SoleOrg(inst.OrgIDs))
		return "shared secret to the fleet vault: " + clip(key, 120), terminalNone

	case protocol.ActShareSession:
		domain := firstNonEmpty(a.SessionDomain, a.Target)
		cookies := firstNonEmpty(a.SessionCookies, a.Text)
		if domain == "" || cookies == "" {
			return "failed: share_session needs a domain and cookies", terminalNone
		}
		title := firstNonEmpty(a.Thought, clip(domain, 60))
		sess := vault.GlobalBus.SaveSession(ctx, domain, title, cookies, "", inst.ID,
			protocol.SoleOrg(inst.OrgIDs))
		return "shared session for " + clip(domain, 80) + " (" + sess.ID + ")", terminalNone

	// Peer messaging. Deliberately not gated behind a swarm: every instance can
	// reach every other one, or broadcast, with no team declared up front. The
	// bus is durable across the fleet, so a peer that is mid-task picks the
	// message up on its next turn rather than needing to be listening now.
	case protocol.ActMsgPeer, protocol.ActDelegateTask:
		to := strings.TrimSpace(a.Target)
		if to == "" {
			to = "broadcast"
		}
		kind := "message"
		content := firstNonEmpty(a.Text, a.Question, a.Summary)
		if a.Action == protocol.ActDelegateTask {
			kind = "delegation"
			content = firstNonEmpty(a.SubGoal, a.Text, a.Question)
		} else if strings.TrimSpace(a.Question) != "" {
			kind = "question"
		}
		if content == "" {
			return "failed: nothing to send", terminalNone
		}

		// Resolve a friendly name to an id so the model can address peers the
		// way the prompt lists them rather than having to copy a UUID.
		if to != "broadcast" {
			if id, ok := r.resolvePeer(ctx, inst.ID, to); ok {
				to = id
			}
		}
		vault.GlobalBus.SendMessage(ctx, inst.ID, inst.Name, to, kind, content, nil)
		if to == "broadcast" {
			return "broadcast to the fleet: " + clip(content, 160), terminalNone
		}
		return "sent to " + to + ": " + clip(content, 160), terminalNone

	case protocol.ActAskHuman:
		reply, err := r.escalate(ctx, task, inst, protocol.AlertNeedsHuman, "warn",
			"Agent needs you", a.Question, obs)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return "cancelled while waiting for the operator", terminalCancelled
			}
			return "no operator reply: " + err.Error(), terminalFail
		}
		return reply, terminalHumanReply

	// Every action that can execute operator-supplied or model-supplied code
	// sits behind the same gate. `python` runs against a live interpreter that
	// can import os and spawn processes, and `mount_tool`/`call_tool` define and
	// then invoke a Python function body — all three are code execution by
	// another name. Gating only `shell` made the instance-level toggle
	// decorative: a model could be refused a shell and then get one via
	// `python`. Enforced again in agentd, because one check is one bug away
	// from a sandbox with an unexpected shell.
	case protocol.ActShell, protocol.ActPython, protocol.ActMountTool, protocol.ActCallTool:
		if !inst.ShellAccess {
			return "refused: code execution (" + string(a.Action) +
				") is disabled for this instance", terminalNone
		}

	case protocol.ActSpawnAgent:
		subGoal := firstNonEmpty(a.SubGoal, a.Text, a.Question)
		if subGoal == "" {
			return "failed: spawn_agent requires a sub-goal", terminalNone
		}
		// Recursion has to be bounded, and not for tidiness: every level is a
		// fan-out of paid model calls, so an unbounded tree is a
		// cost-exhaustion attack that a single poisoned web page can trigger.
		// Async children also bypass the one-task-per-instance guard, so
		// without a cap they pile onto the same desktop and fight for focus.
		if refused := r.checkSpawnBudget(task); refused != "" {
			return refused, terminalNone
		}
		child := &protocol.Task{
			ID:           store.NewID(),
			InstanceID:   task.InstanceID,
			OwnerID:      task.OwnerID,
			Goal:         subGoal,
			SkillID:      a.SubSkillID,
			Params:       a.SubParams,
			ParentTaskID: task.ID,
			State:        protocol.TaskQueued,
			MaxSteps:     30,
			ProviderID:   task.ProviderID,
			CreatedAt:    time.Now().UTC(),
		}
		if err := r.db.CreateTask(ctx, child); err != nil {
			return "failed to spawn child agent: " + err.Error(), terminalNone
		}
		r.recordSpawn(task, child)
		r.bus.Emit("task.spawned", task.InstanceID, task.ID, map[string]any{
			"parent_id": task.ID, "child_id": child.ID, "goal": child.Goal,
		})

		if a.WaitChild {
			res, err := r.runChildWait(ctx, child)
			if err != nil {
				return "child agent failed: " + err.Error(), terminalNone
			}
			return "child agent completed: " + clip(res, 400), terminalNone
		}

		if err := r.Start(ctx, child); err != nil {
			return "failed to start async child agent: " + err.Error(), terminalNone
		}
		return fmt.Sprintf("spawned async child agent %s", child.ID), terminalNone
	}

	actCtx, cancel := context.WithTimeout(ctx, r.cfg.StepTimeout)
	defer cancel()

	// Typing a URL replaces what is in the bar; it never adds to it.
	//
	// type appends, so an agent that retried an address built one out of all
	// its attempts: the bar read
	// ".../work/rollr.htmlfile:///home/agent/work/rollr.htmlhttp://localhost..."
	// and of course never went anywhere, which read as the page being broken
	// rather than the typing being wrong. A URL is always a replacement, so
	// select what is there first and let the typing overwrite it.
	if a.Action == protocol.ActType && looksLikeURL(a.Text) {
		selectCtx, selectCancel := context.WithTimeout(ctx, 10*time.Second)
		_, _ = sc.Act(selectCtx, protocol.Action{Action: protocol.ActKey, Text: "ctrl+a"})
		selectCancel()
	}

	// The model answered in the pixel space of the image it was shown. agentd
	// speaks desktop pixels. Doing the conversion here — once, in one place —
	// is what lets the prompt tell the model to just read positions off the
	// picture, which is the thing vision models are actually good at.
	res, err := sc.Act(actCtx, mapToDesktop(a, obs, r.coordTransformFor(ctx, task.ProviderID, obs)))
	if err != nil {
		if ctx.Err() != nil {
			return "cancelled", terminalCancelled
		}
		return "error: " + clip(err.Error(), 200), terminalNone
	}
	switch {
	case !res.OK:
		return "failed: " + clip(firstNonEmpty(res.Detail, "action rejected"), 200), terminalNone
	case a.Action == protocol.ActAssert:
		if res.Matched {
			return "assertion held", terminalNone
		}
		return "assertion FAILED: " + clip(res.Detail, 200), terminalNone
	case a.Action == protocol.ActWaitFor:
		if res.Matched {
			return "text appeared", terminalNone
		}
		return "timed out waiting for " + clip(a.Text, 60), terminalNone
	case a.Action == protocol.ActShell:
		return fmt.Sprintf("exit %d: %s", res.ExitCode, clip(res.Stdout, 500)), terminalNone
	case a.Action == protocol.ActPython:
		return fmt.Sprintf("python output: %s", clip(res.Stdout, 800)), terminalNone
	case a.Action == protocol.ActMountTool:
		if res.OK {
			mountedTools[a.ToolName] = protocol.MountedTool{
				Name:        a.ToolName,
				Description: a.ToolDescription,
				Parameters:  a.ToolParameters,
				HandlerCode: a.ToolHandler,
			}
			return fmt.Sprintf("mounted tool %q: %s", a.ToolName, firstNonEmpty(res.Detail, "ok")), terminalNone
		}
		return fmt.Sprintf("failed to mount tool %q: %s", a.ToolName, clip(res.Detail, 200)), terminalNone
	case a.Action == protocol.ActUnmountTool:
		delete(mountedTools, a.ToolName)
		return fmt.Sprintf("unmounted tool %q", a.ToolName), terminalNone
	case a.Action == protocol.ActCallTool:
		if !res.OK {
			return fmt.Sprintf("tool %q error: %s", a.ToolName, clip(res.Stdout, 500)), terminalNone
		}
		return fmt.Sprintf("tool %q output: %s", a.ToolName, clip(res.Stdout, 800)), terminalNone
	case a.Action == protocol.ActDeepSearch:
		if !res.OK {
			return fmt.Sprintf("deep_search error: %s", clip(res.Stdout, 500)), terminalNone
		}
		return fmt.Sprintf("deep_search output:\n%s", clip(res.Stdout, 1200)), terminalNone
	// snapshot/rollback are workspace-only (a tar of /home/agent/work), not
	// code execution, so they are not behind the ShellAccess gate; they fall
	// through to sc.Act and agentd's SnapshotEngine does the work.
	case a.Action == protocol.ActSnapshot:
		return "snapshot: " + clip(firstNonEmpty(res.Detail, "created"), 200), terminalNone
	case a.Action == protocol.ActRollback:
		return "rollback: " + clip(firstNonEmpty(res.Detail, "restored"), 200), terminalNone
	default:
		return firstNonEmpty(res.Detail, "ok"), terminalNone
	}
}

func (r *Runner) runChildWait(ctx context.Context, child *protocol.Task) (string, error) {
	if err := r.Start(ctx, child); err != nil {
		return "", err
	}
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			t, err := r.db.Task(ctx, child.ID)
			if err != nil {
				return "", err
			}
			switch t.State {
			case protocol.TaskSucceeded:
				return firstNonEmpty(t.Result, "succeeded"), nil
			case protocol.TaskFailed, protocol.TaskCancelled:
				return "", fmt.Errorf("child task %s: %s", t.State, t.Error)
			}
		}
	}
}

// escalate files an alert, pushes it to the operator's devices, parks the task in
// awaiting_human and blocks until someone answers.
func (r *Runner) escalate(
	ctx context.Context,
	task *protocol.Task,
	inst *protocol.Instance,
	kind protocol.AlertKind,
	severity, title, body string,
	obs *protocol.Observation,
) (string, error) {
	alert := &protocol.Alert{
		Kind:         kind,
		Severity:     severity,
		InstanceID:   inst.ID,
		TaskID:       task.ID,
		Title:        title + " — " + inst.Name,
		Body:         body,
		ScreenshotID: r.storeObservation(ctx, task, obs),
		NeedsReply:   true,
	}
	if err := r.db.CreateAlert(ctx, alert); err != nil {
		return "", err
	}
	r.bus.Emit("alert", inst.ID, task.ID, alert)
	if r.notify != nil {
		if err := r.notify.Send(ctx, *alert); err != nil {
			r.log.Warn("push notification failed", "alert", alert.ID, "err", err)
		}
	}
	_ = r.db.UpdateTaskState(ctx, task.ID, protocol.TaskAwaitingHuman, task.Step, "", "")

	// How many times this task has already stopped. An agent told to decide
	// for itself often asks again immediately -- the instruction not to is
	// advice, and a 4B model takes it as such -- and each ask costs another
	// eight minutes. After the first, it is answered at once instead.
	asks, err := r.db.CountTaskAlerts(ctx, task.ID)
	if err != nil {
		asks = 1 // unknown: assume it has asked, and do not stall on it
	}
	// Work handed over by a colleague does not wait for a person at all. It
	// arrived with instructions, nobody is standing by to answer, and every
	// wait holds up each agent downstream.
	if task.Params[protocol.ParamHandoff] != "" {
		asks = 2
	}

	reply, err := r.waitForReply(ctx, alert.ID, asks)
	if err != nil {
		return "", err
	}
	_ = r.db.UpdateTaskState(ctx, task.ID, protocol.TaskRunning, task.Step, "", "")
	r.bus.Emit("task.state", inst.ID, task.ID, map[string]any{"state": protocol.TaskRunning})
	return reply, nil
}

// waitForReply polls rather than holding a listener: a reply may arrive from the
// mobile app, the admin panel, or a second orchestrator process, and polling a
// row is the one mechanism that sees all three.
func (r *Runner) waitForReply(ctx context.Context, alertID string, priorAsks int) (string, error) {
	const (
		poll = 3 * time.Second
		// How long an agent waits before going ahead on its own.
		//
		// It used to wait six hours and then fail the task outright, which is
		// the worst of both: the work is abandoned, and it is abandoned long
		// after anyone was still watching. Most of what agents stop to ask is
		// something they could reasonably decide -- which of two files to
		// edit, whether a dialog is the one they wanted -- and eight minutes
		// is long enough for someone at their desk to answer and short enough
		// that a run does not die because nobody was.
		selfServeAfter = 8 * time.Minute
	)
	deadline := time.Now().Add(selfServeAfter)
	// Asked before and told to get on with it: do not make the run wait
	// another eight minutes for the same silence.
	if priorAsks > 1 {
		deadline = time.Now()
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
		reply, resolved, err := r.db.AlertReply(ctx, alertID)
		if err != nil {
			return "", err
		}
		if resolved {
			if reply == "" {
				reply = "(operator acknowledged without instructions — continue if you can)"
			}
			return reply, nil
		}
		if time.Now().After(deadline) {
			// Resolve it, so the alert does not sit unanswered forever looking
			// like it is still blocking something, and record that nobody
			// answered -- an operator reading back needs to know the decision
			// was the agent's.
			if err := r.db.ResolveAlert(ctx, alertID,
				"No answer within 8 minutes; the agent continued on its own."); err != nil {
				r.log.Warn("could not resolve an unanswered alert", "alert", alertID, "err", err)
			}
			r.log.Info("no operator reply; continuing without one",
				"alert", alertID, "prior_asks", priorAsks)
			return selfServeReply(priorAsks), nil
		}
	}
}

// storeObservation persists the screenshot for audit replay. Failure to store is
// never fatal to a run.
func (r *Runner) storeObservation(ctx context.Context, task *protocol.Task, obs *protocol.Observation) string {
	if r.art == nil || obs == nil || obs.ScreenshotB64 == "" {
		return ""
	}
	key := fmt.Sprintf("tasks/%s/step-%04d.webp", task.ID, task.Step)
	if err := r.art.PutBase64(ctx, key, "image/webp", obs.ScreenshotB64); err != nil {
		r.log.Warn("could not archive screenshot", "task", task.ID, "err", err)
		return ""
	}
	return key
}

// ------------------------------------------------------------- terminations ---

func (r *Runner) succeed(ctx context.Context, task *protocol.Task, inst *protocol.Instance, summary string) {
	_ = r.db.UpdateTaskState(ctx, task.ID, protocol.TaskSucceeded, task.Step, "", summary)
	r.closeTaskAlerts(ctx, task.ID, "the task finished")
	r.bus.Emit("task.state", task.InstanceID, task.ID,
		map[string]any{"state": protocol.TaskSucceeded, "result": summary})
	r.fileAlert(ctx, task, protocol.AlertCompleted, "info", "Task complete — "+inst.Name, summary)

	if r.refiner != nil && (task.AutoRefine || task.SkillID != "") {
		go func() {
			refineCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			steps, err := r.db.ListSteps(refineCtx, task.ID)
			if err != nil || len(steps) == 0 {
				return
			}
			if task.SkillID != "" {
				if sk, err := r.db.Skill(refineCtx, task.SkillID); err == nil {
					_, _ = r.refiner.RefineSkill(refineCtx, task, sk, steps)
				}
			} else if task.AutoRefine {
				_, _ = r.refiner.SynthesizeSkill(refineCtx, task, steps)
			}
		}()
	}
}

func (r *Runner) fail(ctx context.Context, task *protocol.Task, msg string) {
	r.log.Warn("task failed", "task", task.ID, "err", msg)
	// Detached deliberately: a task most often fails *because* its context died,
	// and writing the terminal state through that same context drops the write.
	// The task then sits in whatever state it was last in — running, or
	// awaiting_human — with nothing able to move it, forever.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = r.db.UpdateTaskState(wctx, task.ID, protocol.TaskFailed, task.Step, msg, "")
	r.bus.Emit("task.state", task.InstanceID, task.ID,
		map[string]any{"state": protocol.TaskFailed, "error": msg})
	// Close whatever it was asking before filing the failure, so the failure
	// is the one thing left open rather than the last of several.
	r.closeTaskAlerts(wctx, task.ID, "the task ended before this was answered")
	r.fileAlert(wctx, task, protocol.AlertFailed, "warn", "Task failed", msg)
}

func (r *Runner) cancelled(ctx context.Context, task *protocol.Task) {
	// The parent context is already dead, so use a fresh one for the write.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = r.db.UpdateTaskState(wctx, task.ID, protocol.TaskCancelled, task.Step, "cancelled by operator", "")
	r.bus.Emit("task.state", task.InstanceID, task.ID, map[string]any{"state": protocol.TaskCancelled})
	r.closeTaskAlerts(wctx, task.ID, "the task was cancelled")
}

func (r *Runner) fileAlert(ctx context.Context, task *protocol.Task, kind protocol.AlertKind, sev, title, body string) {
	a := &protocol.Alert{
		Kind: kind, Severity: sev, InstanceID: task.InstanceID, TaskID: task.ID,
		Title: title, Body: clip(body, 500),
	}
	if err := r.db.CreateAlert(ctx, a); err != nil {
		return
	}
	r.bus.Emit("alert", task.InstanceID, task.ID, a)
	if r.notify != nil {
		_ = r.notify.Send(ctx, *a)
	}
}

// coordSpaceFor resolves the coordinate convention to use for a provider.
//
// An explicit AGENT_COORD_SPACE wins, so an operator can always pin the answer
// if a model behaves oddly. Otherwise the model is measured once and the result
// cached. Detection failure is not fatal: it falls back to pixel, which is what
// the loop assumed before any of this existed.
func (r *Runner) coordTransformFor(ctx context.Context, providerID string, obs *protocol.Observation) CoordTransform {
	w, h := obs.ImageWidth(), obs.ImageHeight()
	if t, pinned := transformFor(r.cfg.CoordSpace, w, h); pinned {
		return t
	}
	if v, ok := r.coordSpace.Load(providerID); ok {
		return v.(CoordTransform)
	}

	t, err := DetectCoordTransform(ctx, r.models, providerID)
	if err != nil {
		r.log.Warn("coordinate calibration failed, assuming the model answers in pixels",
			"provider", providerID, "err", err)
		t = identityTransform
	} else {
		r.log.Info("calibrated model coordinate space",
			"provider", providerID, "space", t.Describe(),
			"scale_x", t.ScaleX, "scale_y", t.ScaleY)
	}
	// Cached either way: a model that cannot be calibrated will not start
	// being calibratable on the next step, and retrying costs a round trip.
	r.coordSpace.Store(providerID, t)
	return t
}

// mapToDesktop rewrites an action's coordinates from the image space the model
// answered in into desktop pixels. Returns a copy: the original is what gets
// persisted to the audit trail, so a replay shows what the model actually said.
//
// ct converts the model's own coordinate space into frame pixels; see
// CoordTransform and config.CoordSpace.
func mapToDesktop(a protocol.Action, obs *protocol.Observation, ct CoordTransform) protocol.Action {
	if obs == nil {
		return a
	}
	out := a
	if a.Mark > 0 {
		// Marks are already in desktop space — agentd builds them from AT-SPI
		// extents and only shifts them into frame space for drawing. So this
		// returns early and deliberately does NOT call ToDesktop: doing so
		// would scale an already-correct coordinate a second time and every
		// set-of-marks click would land short of its target.
		for _, m := range obs.Marks {
			if m.ID == a.Mark {
				out.Coordinates = []int{m.CX, m.CY}
				if out.Target == "" && m.Label != "" {
					out.Target = m.Label
				}
				return out
			}
		}
	}
	if len(a.Coordinates) == 2 {
		x, y := ct.apply(a.Coordinates[0], a.Coordinates[1])
		x, y = obs.ToDesktop(x, y)
		out.Coordinates = []int{x, y}
	}
	if len(a.To) == 2 {
		x, y := ct.apply(a.To[0], a.To[1])
		x, y = obs.ToDesktop(x, y)
		out.To = []int{x, y}
	}
	return out
}

// resolvePeer maps whatever the model called a peer -- an id, or the name the
// prompt listed it under -- onto an instance id. Names are matched case
// insensitively because a model will happily retype "Research Bot" as
// "research bot". Self is excluded: an agent messaging itself is a loop, not
// collaboration.
func (r *Runner) resolvePeer(ctx context.Context, selfID, ref string) (string, bool) {
	insts, err := r.db.ListInstances(ctx)
	if err != nil {
		return "", false
	}
	want := strings.ToLower(strings.TrimSpace(ref))
	for _, in := range insts {
		if in.ID == selfID {
			continue
		}
		if in.ID == ref || strings.ToLower(in.Name) == want {
			return in.ID, true
		}
	}
	return "", false
}

// peerContext lists the other instances an agent can talk to, plus anything
// addressed to it since the last turn. Supplying this every turn is what makes
// fleet collaboration automatic: no swarm has to be created, and an agent does
// not have to be told who its colleagues are.
func (r *Runner) peerContext(ctx context.Context, selfID string) string {
	var sb strings.Builder

	if insts, err := r.db.ListInstances(ctx); err == nil {
		var peers []protocol.Instance
		for _, in := range insts {
			if in.ID != selfID && in.State == protocol.InstanceRunning {
				peers = append(peers, in)
			}
		}
		if len(peers) > 0 {
			sb.WriteString("\nFLEET (peers you can message or delegate to)\n")
			for i, in := range peers {
				if i >= 12 {
					fmt.Fprintf(&sb, "...and %d more\n", len(peers)-i)
					break
				}
				fmt.Fprintf(&sb, "- %s (%s)\n", in.Name, in.ID)
			}
		}
	}

	msgs := vault.GlobalBus.ListMessages(ctx, selfID, 10)
	if len(msgs) > 0 {
		sb.WriteString("\nMESSAGES ADDRESSED TO YOU\n")
		for _, m := range msgs {
			scope := "direct"
			if m.ToInstanceID == "broadcast" {
				scope = "broadcast"
			}
			fmt.Fprintf(&sb, "- from %s (%s, %s): %s\n",
				m.FromInstanceName, scope, m.Kind, clip(m.Content, 240))
		}
	}
	return sb.String()
}

func lastHistory(h []turnSummary) string {
	if len(h) == 0 {
		return "(none)"
	}
	return h[len(h)-1].Action
}

// plural picks a suffix without a format-string dance at the call site.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// workspaceNamed finds a workspace by name, creating it if no one has yet.
//
// Agents refer to a workspace by what it is called, because that is what they
// tell each other. Creating on demand means the first agent to publish into a
// workspace does not have to be the one that made it -- which otherwise turns
// into a round of "who is making the folder".
func (r *Runner) workspaceNamed(ctx context.Context, name string, inst *protocol.Instance) (string, error) {
	all, err := r.db.ListWorkItems(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range all {
		if w.Kind == protocol.WorkWorkspace && strings.EqualFold(w.Name, name) {
			return w.ID, nil
		}
	}
	ws := protocol.WorkItem{
		Name:          name,
		Kind:          protocol.WorkWorkspace,
		Description:   "created by " + inst.Name,
		CreatedBy:     inst.ID,
		CreatedByName: inst.Name,
		OrgID:         protocol.SoleOrg(inst.OrgIDs),
	}
	if err := r.db.PutWorkItem(ctx, &ws); err != nil {
		return "", err
	}
	return ws.ID, nil
}

// readableWork filters the catalog to what one bot may see.
//
// An item filed to a department is readable by a bot in that department. An
// item filed nowhere is readable by the bot that published it and by bots that
// share a department with it -- a bot in several departments files its work
// nowhere, because there is no single department a shared bot's output belongs
// to, and its colleagues still have to be able to build on it.
func readableWork(all []protocol.WorkItem, inst *protocol.Instance, authorOrgs map[string][]string) []protocol.WorkItem {
	mine := make(map[string]bool, len(inst.OrgIDs))
	for _, o := range inst.OrgIDs {
		mine[o] = true
	}
	shares := func(orgs []string) bool {
		for _, o := range orgs {
			if mine[o] {
				return true
			}
		}
		// Two bots that are both unfiled are not being kept apart by anything.
		return len(orgs) == 0 && len(inst.OrgIDs) == 0
	}

	out := make([]protocol.WorkItem, 0, len(all))
	for _, w := range all {
		switch {
		case w.CreatedBy == inst.ID:
			// Always your own.
		case w.OrgID != "":
			if !mine[w.OrgID] {
				continue
			}
		case w.CreatedBy != "":
			// Filed nowhere because the bot that made it is in several
			// departments. Fall back to that bot's departments, so colleagues
			// can build on it and outsiders still cannot.
			if !shares(authorOrgs[w.CreatedBy]) {
				continue
			}
		default:
			// Published by a person into no department: admin-only, and an
			// agent is not an admin.
			continue
		}
		out = append(out, w)
	}
	return out
}

// authorOrgs maps each bot to its departments, for the fallback above.
func (r *Runner) authorOrgs(ctx context.Context) map[string][]string {
	instances, err := r.db.ListInstances(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string][]string, len(instances))
	for _, in := range instances {
		out[in.ID] = in.OrgIDs
	}
	return out
}

// selfServeReply is what an agent is told when nobody answers.
//
// The second time is blunter on purpose. The first message asks it not to ask
// again, and a small model reads that as a suggestion: every task in a
// measured run stopped twice, and each stop cost another wait.
func selfServeReply(priorAsks int) string {
	if priorAsks > 1 {
		return "You have already asked this and been told to decide for yourself. " +
			"Nobody is going to answer. Do not use ask_human again in this task -- " +
			"it will not be answered either. Pick the most reasonable option now " +
			"and carry on, avoiding anything you cannot undo. If you truly cannot " +
			"proceed, finish with the fail action and say exactly what you needed."
	}
	return "Nobody answered within eight minutes, so proceed on your own " +
		"judgement. Choose the most reasonable option and carry on. Do not " +
		"ask again for this same thing -- if you genuinely cannot proceed " +
		"without a person, finish with the fail action and say precisely " +
		"what you needed. Avoid anything you cannot undo."
}

// countPublish records and returns how many times a task has published under
// one name.
//
// Per task rather than per agent: an agent working on several jobs over a day
// is not looping, and a run that publishes the same name three times is.
func (r *Runner) countPublish(taskID, name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.published == nil {
		r.published = map[string]map[string]int{}
	}
	byName, ok := r.published[taskID]
	if !ok {
		byName = map[string]int{}
		r.published[taskID] = byName
	}
	byName[strings.ToLower(name)]++
	return byName[strings.ToLower(name)]
}

// closeTaskAlerts resolves anything a finished task was still asking.
//
// An alert is a question an agent stopped to ask. When the task ends, nobody
// is waiting on the answer any more -- but the alert stayed open, so the
// operator was shown a queue of agents needing them where every single one was
// dead. Measured after six hours: sixty open alerts, all of them belonging to
// tasks that had already succeeded, failed or been cancelled. With
// notifications working, each of those would also have reached the phone.
func (r *Runner) closeTaskAlerts(ctx context.Context, taskID, why string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := r.db.ResolveTaskAlerts(wctx, taskID, why); err != nil {
		r.log.Warn("could not close a finished task's alerts", "task", taskID, "err", err)
	}
}

// movesScreen reports whether an action is meant to change what is on the
// desktop, and so whether an unchanged screen after it means anything.
//
// The stall check compares screenshots. Most of the action vocabulary never
// touches a screen at all -- publishing to the work catalogue, reading a
// colleague's file, remembering something, messaging a peer -- and judging
// those by whether the desktop changed marks correct work as stuck. Anything
// not listed here is treated as off-screen: failing to notice a stall costs a
// step budget, while crying stall on healthy work costs the operator's
// attention and pins the agent.
// How many times one task may stall, decide for itself, and carry on before
// it is treated as going nowhere.
const maxStallEscalations = 3

func movesScreen(k protocol.ActionKind) bool {
	switch k {
	case protocol.ActClick, protocol.ActDoubleClick, protocol.ActRightClick,
		protocol.ActType, protocol.ActKey, protocol.ActScroll, protocol.ActDrag,
		protocol.ActFocus:
		return true
	default:
		// Includes the empty kind, which is the first turn of a task: there was
		// no previous action, so an unchanged screen says nothing yet.
		return false
	}
}

// materialize writes a catalog item into the sandbox so it can actually be
// opened, and reports the path it landed at.
//
// Only for things a browser can render. The content is already readable by
// this agent -- department scoping happened before we got here -- so writing it
// to a file the agent then opens grants nothing new. It goes in base64 so that
// neither the document nor its name can reach the shell as syntax.
func (r *Runner) materialize(ctx context.Context, instanceID string, w protocol.WorkItem) (string, bool) {
	if !renderable(w) {
		return "", false
	}
	safe := safeFileName(w.Name)
	if safe == "" {
		return "", false
	}
	if r.placer == nil {
		return "", false
	}
	path := "/home/agent/work/" + safe + ".html"
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := r.placer.PlaceFile(wctx, instanceID, path, []byte(w.Content)); err != nil {
		r.log.Warn("could not put a copy of the work on the desktop",
			"work", w.Name, "err", err)
		return "", false
	}
	// Put it on screen too, rather than leaving the agent to type the path.
	//
	// Driving a browser by hand is where these runs went to die: the address
	// bar is fiddly, and if the browser has wedged nothing the agent does can
	// tell it so. Opening it here also replaces a browser that has stopped
	// answering, which no bot in this fleet could do for itself.
	if err := r.placer.OpenInBrowser(wctx, instanceID, "file://"+path); err != nil {
		r.log.Warn("could not show the work on the desktop",
			"work", w.Name, "err", err)
	}
	return path, true
}

// renderable reports whether a browser could show this item.
func renderable(w protocol.WorkItem) bool {
	if w.Kind == protocol.WorkApp {
		return true
	}
	lower := strings.ToLower(w.Content)
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html")
}

// safeFileName reduces a catalog name to something that cannot escape the
// directory it is written into.
func safeFileName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// SetFilePlacer supplies the channel used to put files inside a sandbox.
func (r *Runner) SetFilePlacer(p FilePlacer) { r.placer = p }

// looksLikeURL reports whether typed text is an address rather than prose.
func looksLikeURL(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	if strings.ContainsAny(t, " \t\n") {
		return false
	}
	for _, scheme := range []string{"http://", "https://", "file://", "about:"} {
		if strings.HasPrefix(t, scheme) {
			return true
		}
	}
	return false
}
