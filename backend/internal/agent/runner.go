package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/artifacts"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/bus"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
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

	mu      sync.Mutex
	running map[string]context.CancelFunc
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
		refiner: NewRefiner(db, models, b, log),
		log:     log,
		running: map[string]context.CancelFunc{},
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
			r.mu.Unlock()
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
		r.log.Info("resuming task", "task", t.ID, "step", t.Step)
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
		sameCount   int
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
		if obs.Hash != "" && obs.Hash == lastHash {
			sameCount++
		} else {
			sameCount = 0
		}
		lastHash = obs.Hash

		if sameCount >= r.cfg.StallThreshold {
			reply, err := r.escalate(ctx, task, inst, protocol.AlertStalled, "critical",
				"Agent appears stuck",
				fmt.Sprintf("The screen has not changed across %d actions on %q.\nLast action: %s",
					sameCount, inst.Name, lastHistory(history)),
				obs)
			if err != nil {
				r.fail(ctx, task, "stalled and could not reach the operator: "+err.Error())
				return
			}
			humanReply = reply
			sameCount = 0
			continue
		}

		obsKey := r.storeObservation(ctx, task, obs)

		resp, err := r.models.Complete(ctx, task.ProviderID, connectors.Request{
			System:   buildSystem(inst, mountedTools),
			JSONOnly: true,
			Messages: []connectors.Message{{
				Role:      connectors.RoleUser,
				Text:      buildTurn(task, skill, obs, history, humanReply),
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

	case protocol.ActShell:
		if !inst.ShellAccess {
			return "refused: shell is disabled for this instance", terminalNone
		}

	case protocol.ActSpawnAgent:
		subGoal := firstNonEmpty(a.SubGoal, a.Text, a.Question)
		if subGoal == "" {
			return "failed: spawn_agent requires a sub-goal", terminalNone
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

	// The model answered in the pixel space of the image it was shown. agentd
	// speaks desktop pixels. Doing the conversion here — once, in one place —
	// is what lets the prompt tell the model to just read positions off the
	// picture, which is the thing vision models are actually good at.
	res, err := sc.Act(actCtx, mapToDesktop(a, obs))
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

	reply, err := r.waitForReply(ctx, alert.ID)
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
func (r *Runner) waitForReply(ctx context.Context, alertID string) (string, error) {
	const (
		poll   = 3 * time.Second
		giveUp = 6 * time.Hour
	)
	deadline := time.Now().Add(giveUp)
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
			return "", errors.New("no operator reply within 6 hours")
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
	_ = r.db.UpdateTaskState(ctx, task.ID, protocol.TaskFailed, task.Step, msg, "")
	r.bus.Emit("task.state", task.InstanceID, task.ID,
		map[string]any{"state": protocol.TaskFailed, "error": msg})
	r.fileAlert(ctx, task, protocol.AlertFailed, "warn", "Task failed", msg)
}

func (r *Runner) cancelled(ctx context.Context, task *protocol.Task) {
	// The parent context is already dead, so use a fresh one for the write.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = r.db.UpdateTaskState(wctx, task.ID, protocol.TaskCancelled, task.Step, "cancelled by operator", "")
	r.bus.Emit("task.state", task.InstanceID, task.ID, map[string]any{"state": protocol.TaskCancelled})
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

// mapToDesktop rewrites an action's coordinates from the image space the model
// answered in into desktop pixels. Returns a copy: the original is what gets
// persisted to the audit trail, so a replay shows what the model actually said.
func mapToDesktop(a protocol.Action, obs *protocol.Observation) protocol.Action {
	if obs == nil {
		return a
	}
	out := a
	if a.Mark > 0 {
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
		x, y := obs.ToDesktop(a.Coordinates[0], a.Coordinates[1])
		out.Coordinates = []int{x, y}
	}
	if len(a.To) == 2 {
		x, y := obs.ToDesktop(a.To[0], a.To[1])
		out.To = []int{x, y}
	}
	return out
}

func lastHistory(h []turnSummary) string {
	if len(h) == 0 {
		return "(none)"
	}
	return h[len(h)-1].Action
}
