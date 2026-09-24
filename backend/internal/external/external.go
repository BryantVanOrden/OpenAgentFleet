// Package external runs agents that are not desktops.
//
// Claude Code, Codex and Hermes are CLIs on a PC: the run becomes a device
// job that `fleetctl host` picks up, runs in the agent's folder, and reports
// back with its answer, its token usage and a session id to resume. OpenClaw
// is a gateway the orchestrator dials. A webhook is anything that accepts a
// POST and calls back. All of them surface as ordinary tasks -- the same
// states, steps, costs and events as a desktop's run -- so tickets, the org
// chart, budgets and both consoles treat them exactly alike.
package external

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Store is the persistence the adapters need.
type Store interface {
	Instance(ctx context.Context, id string) (*protocol.Instance, error)
	Task(ctx context.Context, id string) (*protocol.Task, error)
	UpdateTaskState(ctx context.Context, id string, st protocol.TaskState, step int, errMsg, result string) error
	AppendStep(ctx context.Context, r *protocol.StepRecord) error
	CreateDeviceJob(ctx context.Context, j *protocol.DeviceJob) error
	DeviceJob(ctx context.Context, id string) (*protocol.DeviceJob, error)
	OafDevice(ctx context.Context, id string) (*protocol.Device, error)
	TicketTasks(ctx context.Context, ticketID string) ([]protocol.Task, error)
	SetTaskParam(ctx context.Context, taskID, key, value string) error
	Ticket(ctx context.Context, id string) (*protocol.Ticket, error)
	PutWorkItem(ctx context.Context, w *protocol.WorkItem) error
}

// Secrets opens and stores vault entries.
type Secrets interface {
	Open(ctx context.Context, ref string) (string, error)
	Put(ctx context.Context, ref, value, note string) error
}

// Verdicts is the part of the ticket engine a finishing run reports to.
type Verdicts interface {
	RequiresVerdict(ctx context.Context, taskID string) bool
	SetVerdict(taskID, verdict string)
}

// Config tunes the adapters.
type Config struct {
	// PublicURL is how an external service reaches this orchestrator, for
	// webhook callbacks.
	PublicURL string
	// Secret signs run tokens.
	Secret []byte
	// DefaultTimeout bounds one run when the agent sets none.
	DefaultTimeout time.Duration
	// DeviceOnline is how recently a PC must have polled to count as there.
	DeviceOnline time.Duration
}

// Dispatcher is the External implementation the runner hands runs to.
type Dispatcher struct {
	cfg      Config
	db       Store
	secrets  Secrets
	verdicts Verdicts
	emit     func(kind, instanceID, taskID string, payload any)
	record   func(ctx context.Context, rec protocol.TokenTelemetryRecord)
	log      *slog.Logger

	mu   sync.Mutex
	runs map[string]*run // by task id
	now  func() time.Time
}

type run struct {
	task     *protocol.Task
	inst     *protocol.Instance
	cancel   context.CancelFunc
	stopped  bool // the operator or the engine asked it to stop
	jobID    string
	step     int
	callback chan outcome // webhook completions
}

// outcome is how a run ended, whatever ran it.
type outcome struct {
	ok        bool
	answer    string
	err       string
	model     string
	inTokens  int
	outTokens int
	cached    int
	costUSD   float64
	sessionID string
	verdict   string
	// files are what the agent created or changed in its folder, shared to
	// the catalog so colleagues on other machines can read them.
	files []ProducedFile
	// unshared are changed paths too big or not text enough to share.
	unshared []string
}

// New builds a dispatcher. emit publishes events; record books a turn's cost.
func New(cfg Config, db Store, secrets Secrets, verdicts Verdicts,
	emit func(kind, instanceID, taskID string, payload any),
	record func(ctx context.Context, rec protocol.TokenTelemetryRecord), log *slog.Logger) *Dispatcher {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.DeviceOnline <= 0 {
		cfg.DeviceOnline = 90 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{cfg: cfg, db: db, secrets: secrets, verdicts: verdicts, emit: emit, record: record, log: log,
		runs: map[string]*run{}, now: func() time.Time { return time.Now().UTC() }}
}

// SetVerdicts connects the ticket engine after construction; the two are
// built in either order.
func (d *Dispatcher) SetVerdicts(v Verdicts) { d.verdicts = v }

// Ready reports whether an external agent can take a run now, and why not.
func (d *Dispatcher) Ready(ctx context.Context, inst *protocol.Instance) (bool, string) {
	kind := inst.AgentKindOf()
	c := inst.Connection
	if kind.OnDevice() {
		if c.DeviceID == "" {
			return false, "it has no PC set; edit it and pick a device running fleetctl host"
		}
		dev, err := d.db.OafDevice(ctx, c.DeviceID)
		if err != nil {
			return false, "its PC is no longer registered"
		}
		if d.now().Sub(dev.LastSeen) > d.cfg.DeviceOnline {
			return false, fmt.Sprintf("its PC (%s) is not connected; run fleetctl host there", dev.Name)
		}
		if len(dev.Runtimes) > 0 && !has(dev.Runtimes, string(kind)) {
			return false, fmt.Sprintf("%s does not have %s installed", dev.Name, kind.Label())
		}
		return true, ""
	}
	if strings.TrimSpace(c.URL) == "" {
		return false, "it has no address set"
	}
	return true, ""
}

// Start begins a run and returns at once; the run reports through the task.
func (d *Dispatcher) Start(ctx context.Context, task *protocol.Task, inst *protocol.Instance) error {
	d.mu.Lock()
	if _, busy := d.runs[task.ID]; busy {
		d.mu.Unlock()
		return fmt.Errorf("task %s is already running", task.ID)
	}
	timeout := d.cfg.DefaultTimeout
	if inst.Connection.TimeoutSec > 0 {
		timeout = time.Duration(inst.Connection.TimeoutSec) * time.Second
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	r := &run{task: task, inst: inst, cancel: cancel, callback: make(chan outcome, 1)}
	d.runs[task.ID] = r
	d.mu.Unlock()

	_ = d.db.UpdateTaskState(rctx, task.ID, protocol.TaskRunning, 0, "", "")
	d.emitState(task, protocol.TaskRunning, nil)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				d.finish(context.WithoutCancel(rctx), r, outcome{err: fmt.Sprintf("agent cli exited: adapter panicked: %v", rec)})
			}
			d.mu.Lock()
			delete(d.runs, task.ID)
			d.mu.Unlock()
			cancel()
		}()
		var out outcome
		switch inst.AgentKindOf() {
		case protocol.KindClaudeCode, protocol.KindCodex, protocol.KindHermes:
			out = d.runOnDevice(rctx, r)
		case protocol.KindOpenClaw:
			out = d.runOpenClaw(rctx, r)
		case protocol.KindWebhook:
			out = d.runWebhook(rctx, r)
		default:
			out = outcome{err: "unknown agent kind " + string(inst.AgentKindOf())}
		}
		if errors.Is(rctx.Err(), context.DeadlineExceeded) && !out.ok && out.err == "" {
			out.err = fmt.Sprintf("the run timed out after %s", timeout)
		}
		d.finish(context.WithoutCancel(rctx), r, out)
	}()
	return nil
}

// Cancel stops a run. Local CLIs are killed by their host; a gateway run is
// abandoned; a webhook is told nothing more.
func (d *Dispatcher) Cancel(taskID string) bool {
	d.mu.Lock()
	r, ok := d.runs[taskID]
	if ok {
		r.stopped = true
	}
	d.mu.Unlock()
	if ok {
		r.cancel()
	}
	return ok
}

func (d *Dispatcher) IsRunning(taskID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.runs[taskID]
	return ok
}

// finish records how a run ended, exactly as the desktop runner does: the
// task row, the event, the cost, and the verdict for a review.
func (d *Dispatcher) finish(ctx context.Context, r *run, out outcome) {
	d.mu.Lock()
	stopped := r.stopped
	d.mu.Unlock()
	task, inst := r.task, r.inst

	if out.inTokens > 0 || out.outTokens > 0 || out.costUSD > 0 {
		if d.record != nil {
			d.record(ctx, protocol.TokenTelemetryRecord{
				TaskID: task.ID, InstanceID: inst.ID, ProviderID: string(inst.AgentKindOf()),
				ModelName:    firstNonEmpty(out.model, inst.Connection.Model, string(inst.AgentKindOf())),
				PromptTokens: out.inTokens, CompletionTokens: out.outTokens, CachedTokens: out.cached,
				CostUSD: out.costUSD,
			})
		}
	}
	if out.sessionID != "" {
		_ = d.db.SetTaskParam(ctx, task.ID, "session_id", out.sessionID)
		_ = d.db.SetTaskParam(ctx, task.ID, "session_cwd", inst.Connection.Cwd)
	}

	var shared []string
	if out.ok && !stopped {
		shared = d.share(ctx, r, out.files)
	}

	switch {
	case stopped:
		_ = d.db.UpdateTaskState(ctx, task.ID, protocol.TaskCancelled, r.step, "stopped", "")
		d.emitState(task, protocol.TaskCancelled, nil)
	case out.ok:
		answer := strings.TrimSpace(out.answer)
		if answer == "" {
			answer = "(the agent finished without a final message)"
		}
		if len(shared) > 0 {
			answer += "\n\nShared with the fleet (read_work): " + strings.Join(shared, ", ") + "."
		}
		if len(out.unshared) > 0 {
			answer += "\nAlso changed, not shared (binary or too large): " + strings.Join(out.unshared, ", ") + "."
		}
		if d.verdicts != nil && d.verdicts.RequiresVerdict(ctx, task.ID) {
			v := firstNonEmpty(out.verdict, leadingVerdict(answer))
			if v != "" {
				d.verdicts.SetVerdict(task.ID, v)
			}
		}
		_ = d.db.UpdateTaskState(ctx, task.ID, protocol.TaskSucceeded, r.step, "", clip(answer, 20000))
		d.emitState(task, protocol.TaskSucceeded, map[string]any{"result": clip(answer, 20000)})
	default:
		msg := firstNonEmpty(out.err, "the agent failed without saying why")
		_ = d.db.UpdateTaskState(ctx, task.ID, protocol.TaskFailed, r.step, clip(msg, 2000), "")
		d.emitState(task, protocol.TaskFailed, map[string]any{"error": clip(msg, 2000)})
	}
	d.log.Info("external run finished", "agent", inst.Name, "kind", inst.AgentKindOf(), "task", task.ID,
		"ok", out.ok, "stopped", stopped, "cost", out.costUSD)
}

// share publishes what an agent on a PC made, keyed by its path in the
// agent's folder, so the name in its report is the name a colleague reads.
// A desktop colleague cannot reach the operator's PC; without this the
// file an agent was asked to write was invisible to whoever had to check it.
func (d *Dispatcher) share(ctx context.Context, r *run, files []ProducedFile) []string {
	var names []string
	for _, f := range files {
		name := strings.TrimSpace(strings.ReplaceAll(f.Path, "\\", "/"))
		if name == "" || strings.HasPrefix(name, "/") || slices.Contains(strings.Split(name, "/"), "..") {
			continue
		}
		mime := "text/plain"
		if strings.HasSuffix(strings.ToLower(name), ".html") {
			mime = "text/html"
		}
		item := protocol.WorkItem{
			Name: name, Kind: protocol.WorkFile, Content: f.Content, MIME: mime,
			Description:   clip("From "+r.inst.Name+"'s folder on its PC", 300),
			CreatedBy:     r.inst.ID,
			CreatedByName: r.inst.Name,
			OrgID:         protocol.SoleOrg(r.inst.OrgIDs),
		}
		if err := d.db.PutWorkItem(ctx, &item); err != nil {
			d.log.Warn("could not share an external agent's file", "agent", r.inst.Name, "path", name, "err", err)
			continue
		}
		if d.emit != nil {
			d.emit("work", r.inst.ID, r.task.ID, item)
		}
		names = append(names, name)
	}
	return names
}

func (d *Dispatcher) emitState(task *protocol.Task, st protocol.TaskState, extra map[string]any) {
	if d.emit == nil {
		return
	}
	p := map[string]any{"state": st}
	for k, v := range extra {
		p[k] = v
	}
	d.emit("task.state", task.InstanceID, task.ID, p)
}

// progress records one line of what an external agent is doing as a step,
// so the task's page reads like a desktop's.
func (d *Dispatcher) progress(ctx context.Context, r *run, kind, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	d.mu.Lock()
	r.step++
	n := r.step
	d.mu.Unlock()
	a := protocol.Action{Action: protocol.ActionKind(firstNonEmpty(kind, "progress")), Thought: clip(text, 600)}
	_ = d.db.AppendStep(ctx, &protocol.StepRecord{TaskID: r.task.ID, Step: n, Action: a, Outcome: clip(text, 2000)})
	_ = d.db.UpdateTaskState(ctx, r.task.ID, protocol.TaskRunning, n, "", "")
	if d.emit != nil {
		d.emit("task.step", r.inst.ID, r.task.ID, map[string]any{"step": n, "action": a, "outcome": clip(text, 600)})
	}
}

// Prompt is what an external agent is given: the ticket's brief, its
// standing instructions, and how to talk back to the fleet.
func (d *Dispatcher) Prompt(r *run, apiHint string) string {
	var b strings.Builder
	if s := strings.TrimSpace(r.inst.SystemPrompt); s != "" {
		b.WriteString("Your standing instructions:\n" + s + "\n\n")
	}
	b.WriteString(strings.TrimSpace(r.task.Goal))
	b.WriteString("\n\n---\nYou are " + r.inst.Name)
	if r.inst.Title != "" {
		b.WriteString(" (" + r.inst.Title + ")")
	}
	b.WriteString(", an agent in an OpenAgentFleet fleet. Work in your folder and finish in this run. " +
		"Your final message is your report: say what you produced, where it is, and anything left undone.\n")
	if r.inst.AgentKindOf().OnDevice() {
		b.WriteString("Text files you create or change in your folder are shared with the fleet when you finish, under their path in the folder, so colleagues on other machines can read them. Refer to them by that path.\n")
	}
	if apiHint != "" {
		b.WriteString(apiHint)
	}
	return b.String()
}

// apiHint tells an agent that can run commands how to reach the fleet.
func apiHint(ticketRef string) string {
	return `
To talk to the fleet (optional), use its API with the credentials in your environment:
  base: $AGENTFLEET_API_URL    auth: -H "Authorization: Bearer $AGENTFLEET_RUN_TOKEN"
  - Hand part of ` + ticketRef + ` to a colleague:  POST $AGENTFLEET_API_URL/api/runs/$AGENTFLEET_TASK_ID/tickets  {"target":"<their name>","title":"...","text":"complete instructions"}
  - Leave a note on the ticket:              POST $AGENTFLEET_API_URL/api/runs/$AGENTFLEET_TASK_ID/comments {"body":"..."}
  - Read your ticket and its colleagues:     GET  $AGENTFLEET_API_URL/api/runs/$AGENTFLEET_TASK_ID
For a review or verify ticket, start your final message with PASS or FAIL.
`
}

func leadingVerdict(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(u, "PASS"):
		return protocol.VerdictPass
	case strings.HasPrefix(u, "FAIL"):
		return protocol.VerdictFail
	}
	return ""
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
