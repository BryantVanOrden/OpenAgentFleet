// Package tickets runs work as durable tickets.
//
// A ticket has one assignee, a parent it exists for, and the tickets it waits
// on. The engine starts a run for every ticket that is ready, finishes the
// ticket from how the run ended, and wakes whoever was waiting on it. Nothing
// here is inferred from what an agent said: a hand-off is a blocker finishing,
// a review is a ticket with a verdict, and a restart loses nothing because
// every decision is re-derived from the rows on the next pass.
//
// The engine also keeps the promise that no unfinished ticket is ownerless
// (liveness.go), checks stopped work with a verifier (liveness.go), and holds
// an agent that reaches its spend ceiling (budget.go).
package tickets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// DB is the persistence the engine needs. *store.Store satisfies it; tests use
// an in-memory fake.
type DB interface {
	CreateTicket(ctx context.Context, t *protocol.Ticket) error
	Ticket(ctx context.Context, id string) (*protocol.Ticket, error)
	TicketByNumber(ctx context.Context, n int64) (*protocol.Ticket, error)
	TicketByTask(ctx context.Context, taskID string) (*protocol.Ticket, error)
	ListTickets(ctx context.Context, f protocol.TicketFilter) ([]protocol.Ticket, error)
	OpenTickets(ctx context.Context) ([]protocol.Ticket, error)
	Children(ctx context.Context, parentID string) ([]protocol.Ticket, error)
	Dependents(ctx context.Context, blockerID string) ([]protocol.Ticket, error)
	UpdateTicket(ctx context.Context, t *protocol.Ticket) error
	CheckoutTicket(ctx context.Context, ticketID, taskID string) error
	ReleaseTicket(ctx context.Context, ticketID, taskID string) (bool, error)
	MoveTicketLock(ctx context.Context, ticketID, fromTask, toTask string) error
	SetBlockers(ctx context.Context, ticketID string, blockers []string) error
	AddBlocker(ctx context.Context, ticketID, blockerID string) error
	Ancestry(ctx context.Context, id string) ([]protocol.Ticket, error)
	Subtree(ctx context.Context, rootID string) ([]protocol.Ticket, error)
	AddTicketComment(ctx context.Context, c *protocol.TicketComment) error
	TicketComments(ctx context.Context, ticketID string, limit int) ([]protocol.TicketComment, error)
	TicketSpend(ctx context.Context, ticketID string) (float64, error)
	InstanceSpend(ctx context.Context, instanceID string, since time.Time) (float64, error)
	ClaimBudgetNotice(ctx context.Context, instanceID, month, level string) (bool, error)
	ClearBudgetNotices(ctx context.Context, instanceID, month string) error
	SetInstanceHold(ctx context.Context, id, hold string) error

	Instance(ctx context.Context, id string) (*protocol.Instance, error)
	ListInstances(ctx context.Context) ([]protocol.Instance, error)
	CreateTask(ctx context.Context, t *protocol.Task) error
	Task(ctx context.Context, id string) (*protocol.Task, error)
	ListTasks(ctx context.Context, instanceID string, limit int) ([]protocol.Task, error)
	UpdateTaskState(ctx context.Context, id string, st protocol.TaskState, step int, errMsg, result string) error
	ListPeerMessages(ctx context.Context, instanceID string, limit int) ([]protocol.PeerMessage, error)
	CreateAlert(ctx context.Context, a *protocol.Alert) error
	ResolveTicketAlerts(ctx context.Context, ticketID, reply string) ([]string, error)
}

var _ DB = (*store.Store)(nil)

// Starter runs tasks. The agent runner implements it for desktops and hands
// external kinds to their adapters.
type Starter interface {
	Start(ctx context.Context, task *protocol.Task) error
	Cancel(taskID string) bool
	IsRunning(taskID string) bool
	// Ready reports whether an agent can take a run now, and why not.
	Ready(ctx context.Context, inst *protocol.Instance) (bool, string)
}

// Hooks are how the engine talks to the rest of the system.
type Hooks struct {
	// Alert has been stored already; this delivers it (event bus, phone).
	Alert func(ctx context.Context, a *protocol.Alert)
	// Say posts a line in a fleet comms thread, as fromName.
	Say func(ctx context.Context, thread, fromID, fromName, toID, text string)
	// Emit publishes a live event for the consoles.
	Emit func(kind, instanceID, taskID string, payload any)
}

// Config tunes the engine. Zero values take the defaults.
type Config struct {
	MaxSteps int
	// MaxRetries is how often one ticket's run is restarted after a failure
	// that says nothing about the work.
	MaxRetries int
	// MaxRounds bounds review/fix cycles on one ticket.
	MaxRounds int
	// MaxWakes bounds how often a ticket that waits on tickets it created is
	// woken again, so a manager that keeps delegating cannot loop forever.
	MaxWakes int
	// BacklogPatience is how long a parked ticket may wait for its
	// predecessors to be arranged before the operator hears about it.
	BacklogPatience time.Duration
	// Tick is how often the engine re-derives everything from the rows.
	Tick time.Duration
}

func (c *Config) defaults() {
	if c.MaxSteps <= 0 {
		c.MaxSteps = 60
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 2
	}
	if c.MaxRounds <= 0 {
		c.MaxRounds = 3
	}
	if c.MaxWakes <= 0 {
		c.MaxWakes = 6
	}
	if c.BacklogPatience <= 0 {
		c.BacklogPatience = 8 * time.Minute
	}
	if c.Tick <= 0 {
		c.Tick = 20 * time.Second
	}
}

// Engine is the ticket scheduler.
type Engine struct {
	cfg   Config
	db    DB
	run   Starter
	hooks Hooks
	log   *slog.Logger

	// mu serialises every decision. Tickets move a few times a minute; one
	// lock is simpler to reason about than a lock per row, and it is what
	// makes "finish this ticket, wake its dependents" one step.
	mu   sync.Mutex
	kick chan struct{}

	// cancelling records runs the engine itself stopped, and why, so the
	// cancellation that comes back is read as that reason rather than as the
	// operator pressing stop.
	cancelling map[string]string
	// verdicts are the verdicts review and verify runs finished with, keyed
	// by task, handed over by the runner before it reports success.
	verdicts map[string]string

	now func() time.Time
}

// New builds an engine.
func New(cfg Config, db DB, run Starter, hooks Hooks, log *slog.Logger) *Engine {
	cfg.defaults()
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		cfg: cfg, db: db, run: run, hooks: hooks, log: log,
		kick:       make(chan struct{}, 1),
		cancelling: map[string]string{},
		verdicts:   map[string]string{},
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// Kick asks for a pass soon. It never blocks.
func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Run drives the engine until ctx ends: a pass on every kick, every task
// event and every tick. Events come from the event bus.
func (e *Engine) Run(ctx context.Context, events <-chan protocol.Event) {
	t := time.NewTicker(e.cfg.Tick)
	defer t.Stop()
	e.Reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.kick:
			e.Reconcile(ctx)
		case <-t.C:
			e.Reconcile(ctx)
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			e.handleEvent(ctx, ev)
		}
	}
}

func (e *Engine) handleEvent(ctx context.Context, ev protocol.Event) {
	switch ev.Type {
	case "task.state":
		payload, _ := ev.Payload.(map[string]any)
		st := stateOf(payload["state"])
		if st == "" {
			if task, ok := ev.Payload.(*protocol.Task); ok {
				st = task.State
			}
		}
		switch st {
		case protocol.TaskSucceeded, protocol.TaskFailed, protocol.TaskCancelled:
			e.TaskEnded(ctx, ev.TaskID)
		case protocol.TaskContinued:
			next, _ := payload["next_task_id"].(string)
			e.TaskContinued(ctx, ev.TaskID, next)
		}
	case "work":
		if ev.InstanceID != "" {
			if item, ok := ev.Payload.(protocol.WorkItem); ok {
				e.notePublished(ctx, ev.InstanceID, item)
			}
		}
	}
}

func stateOf(v any) protocol.TaskState {
	switch s := v.(type) {
	case protocol.TaskState:
		return s
	case string:
		return protocol.TaskState(s)
	}
	return ""
}

// ------------------------------------------------------------ reconcile ---

// Reconcile is one pass: start everything that is ready, finish anything
// whose run ended while nobody was listening, keep the liveness promise, run
// verifiers, and apply budgets.
func (e *Engine) Reconcile(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.applyBudgetsLocked(ctx)

	open, err := e.db.OpenTickets(ctx)
	if err != nil {
		e.log.Warn("tickets: could not list open tickets", "err", err)
		return
	}
	byID := map[string]*protocol.Ticket{}
	for i := range open {
		byID[open[i].ID] = &open[i]
	}
	for i := range open {
		t := &open[i]
		switch t.Status {
		case protocol.TicketInProgress:
			e.checkHolderLocked(ctx, t)
		case protocol.TicketTodo:
			e.tryDispatchLocked(ctx, t)
		}
	}
	e.livenessLocked(ctx)
	e.verifiersLocked(ctx)

	// The sweep and the verifiers create tickets (reviews, verifications,
	// unblocks); start those in this pass rather than the next one.
	if again, err := e.db.OpenTickets(ctx); err == nil {
		for i := range again {
			if again[i].Status == protocol.TicketTodo {
				e.tryDispatchLocked(ctx, &again[i])
			}
		}
	}
}

// checkHolderLocked finishes a ticket whose run ended without the engine
// hearing about it -- the orchestrator restarted, an event was dropped -- and
// frees one whose run vanished.
func (e *Engine) checkHolderLocked(ctx context.Context, t *protocol.Ticket) {
	if t.TaskID == "" {
		// In progress with no holder: a lost lock. Back to todo so it runs.
		t.Status = protocol.TicketTodo
		_ = e.db.UpdateTicket(ctx, t)
		e.systemComment(ctx, t, "The run holding this ticket was lost; it goes back in the queue.")
		return
	}
	if e.run.IsRunning(t.TaskID) {
		return
	}
	task, err := e.db.Task(ctx, t.TaskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_, _ = e.db.ReleaseTicket(ctx, t.ID, t.TaskID)
			t.TaskID = ""
			t.Status = protocol.TicketTodo
			t.Attempts++
			_ = e.db.UpdateTicket(ctx, t)
		}
		return
	}
	switch task.State {
	case protocol.TaskSucceeded, protocol.TaskFailed, protocol.TaskCancelled:
		e.finishLocked(ctx, t, task)
	case protocol.TaskAwaitingHuman:
		// Parked on a question. That is a live path: a person is being
		// asked. The liveness sweep watches how long it has been.
	case protocol.TaskQueued, protocol.TaskRunning:
		// Recorded as live but no runner holds it: resumed on boot or about
		// to start. Give it a moment before calling it lost.
		if task.StartedAt != nil && e.now().Sub(*task.StartedAt) > 10*time.Minute && task.State == protocol.TaskQueued {
			_ = e.db.UpdateTaskState(ctx, task.ID, protocol.TaskFailed, task.Step, "run was lost before it started", "")
			task.State, task.Error = protocol.TaskFailed, "run was lost before it started"
			e.finishLocked(ctx, t, task)
		}
	}
}

// --------------------------------------------------------------- dispatch ---

// ready reports whether every blocker of t is done.
func (e *Engine) blockersDone(ctx context.Context, t *protocol.Ticket) (bool, []*protocol.Ticket) {
	var blockers []*protocol.Ticket
	done := true
	for _, id := range t.BlockedBy {
		b, err := e.db.Ticket(ctx, id)
		if err != nil {
			done = false
			continue
		}
		blockers = append(blockers, b)
		if b.Status != protocol.TicketDone {
			done = false
		}
	}
	return done, blockers
}

func (e *Engine) tryDispatchLocked(ctx context.Context, t *protocol.Ticket) {
	if t.AssigneeID == "" || t.TaskID != "" {
		return
	}
	if ok, _ := e.blockersDone(ctx, t); !ok {
		return
	}
	inst, err := e.db.Instance(ctx, t.AssigneeID)
	if err != nil {
		return // the liveness sweep reports a missing assignee
	}
	if inst.Hold != protocol.HoldNone {
		return
	}
	if ok, _ := e.run.Ready(ctx, inst); !ok {
		return
	}
	if e.busyLocked(ctx, inst) {
		return
	}
	if t.BudgetUSD > 0 {
		if spent, err := e.db.TicketSpend(ctx, t.ID); err == nil && spent >= t.BudgetUSD {
			e.blockLocked(ctx, t, fmt.Sprintf("the ticket has spent its budget ($%.2f of $%.2f)", spent, t.BudgetUSD), false)
			return
		}
	}

	goal, err := e.BuildGoal(ctx, t, inst)
	if err != nil {
		e.log.Warn("tickets: could not build a goal", "ticket", t.Ref(), "err", err)
		return
	}
	task := &protocol.Task{
		ID:         store.NewID(),
		InstanceID: inst.ID,
		OwnerID:    firstNonEmpty(t.OwnerID, inst.OwnerID),
		Goal:       goal,
		TicketID:   t.ID,
		State:      protocol.TaskQueued,
		MaxSteps:   e.cfg.MaxSteps,
		CreatedAt:  e.now(),
		// A colleague -- or the engine -- handed this over with concrete
		// instructions. There is no person waiting to answer questions, so
		// the runner must not park it on ask_human.
		Params: map[string]string{protocol.ParamHandoff: "1", "ticket": t.Ref()},
	}
	if err := e.db.CheckoutTicket(ctx, t.ID, task.ID); err != nil {
		return // someone else took it; the next pass sees why
	}
	if err := e.db.CreateTask(ctx, task); err != nil {
		_, _ = e.db.ReleaseTicket(ctx, t.ID, task.ID)
		e.revertToTodoLocked(ctx, t)
		e.log.Warn("tickets: could not create the run", "ticket", t.Ref(), "err", err)
		return
	}
	if err := e.run.Start(context.WithoutCancel(ctx), task); err != nil {
		_ = e.db.UpdateTaskState(ctx, task.ID, protocol.TaskFailed, 0, "could not start: "+err.Error(), "")
		_, _ = e.db.ReleaseTicket(ctx, t.ID, task.ID)
		e.revertToTodoLocked(ctx, t)
		e.log.Warn("tickets: could not start the run", "ticket", t.Ref(), "err", err)
		return
	}
	t.TaskID, t.Status = task.ID, protocol.TicketInProgress
	e.emit(t)
	e.log.Info("ticket started", "ticket", t.Ref(), "agent", inst.Name, "task", task.ID)
	e.announceStartLocked(ctx, t, inst)
}

func (e *Engine) revertToTodoLocked(ctx context.Context, t *protocol.Ticket) {
	fresh, err := e.db.Ticket(ctx, t.ID)
	if err != nil {
		return
	}
	fresh.Status = protocol.TicketTodo
	_ = e.db.UpdateTicket(ctx, fresh)
}

// busyLocked reports whether the agent already has a run. One driver at a
// time, the rule the task endpoint enforces too. A run parked on a question
// nobody is answering is not ticket work and does not hold the desktop
// against it: it is superseded, as the relay always did.
func (e *Engine) busyLocked(ctx context.Context, inst *protocol.Instance) bool {
	tasks, err := e.db.ListTasks(ctx, inst.ID, 20)
	if err != nil {
		return true
	}
	busy := false
	for _, k := range tasks {
		switch k.State {
		case protocol.TaskRunning, protocol.TaskQueued:
			busy = true
		case protocol.TaskAwaitingHuman:
			if k.TicketID != "" {
				busy = true
				continue
			}
			if e.run.Cancel(k.ID) {
				e.cancelling[k.ID] = "superseded by ticket work"
			} else {
				_ = e.db.UpdateTaskState(ctx, k.ID, protocol.TaskCancelled, k.Step, "superseded by ticket work", "")
			}
		}
	}
	return busy
}

// announceStartLocked tells the thread the work moved, when it moved because
// something it waited on finished. A start nobody can see looks like an agent
// doing something at random.
func (e *Engine) announceStartLocked(ctx context.Context, t *protocol.Ticket, inst *protocol.Instance) {
	if e.hooks.Say == nil || t.Thread == "" || len(t.BlockedBy) == 0 {
		return
	}
	var names []string
	var from *protocol.Instance
	for _, id := range t.BlockedBy {
		b, err := e.db.Ticket(ctx, id)
		if err != nil {
			continue
		}
		names = append(names, b.Ref())
		if from == nil && b.AssigneeID != "" {
			from, _ = e.db.Instance(ctx, b.AssigneeID)
		}
	}
	line := fmt.Sprintf("%s is done. Over to you, %s: %s — %s.", strings.Join(names, " and "), inst.Name, t.Ref(), t.Title)
	if from != nil {
		e.hooks.Say(ctx, t.Thread, from.ID, from.Name, inst.ID, line)
	} else {
		e.hooks.Say(ctx, t.Thread, "", "Oaf", inst.ID, line)
	}
}

// -------------------------------------------------------------- finishing ---

// TaskContinued moves a ticket's checkout to the marathon window that carries
// its run on.
func (e *Engine) TaskContinued(ctx context.Context, taskID, next string) {
	if next == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.TicketByTask(ctx, taskID)
	if err != nil || t.TaskID != taskID {
		return
	}
	if err := e.db.MoveTicketLock(ctx, t.ID, taskID, next); err != nil {
		e.log.Warn("tickets: could not follow a continued run", "ticket", t.Ref(), "err", err)
	}
}

// TaskEnded finishes the ticket a run held.
func (e *Engine) TaskEnded(ctx context.Context, taskID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.TicketByTask(ctx, taskID)
	if err != nil || t.TaskID != taskID {
		return
	}
	task, err := e.db.Task(ctx, taskID)
	if err != nil {
		return
	}
	e.finishLocked(ctx, t, task)
	e.Kick()
}

// SetVerdict records the verdict a review or verify run is about to finish
// with. The runner calls it before reporting success.
func (e *Engine) SetVerdict(taskID, verdict string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.verdicts[taskID] = verdict
}

func (e *Engine) finishLocked(ctx context.Context, t *protocol.Ticket, task *protocol.Task) {
	if ok, err := e.db.ReleaseTicket(ctx, t.ID, task.ID); err != nil || !ok {
		return
	}
	t.TaskID = ""
	verdict := e.verdicts[task.ID]
	delete(e.verdicts, task.ID)
	reason, engineStopped := e.cancelling[task.ID]
	delete(e.cancelling, task.ID)

	switch task.State {
	case protocol.TaskSucceeded:
		e.succeededLocked(ctx, t, task, verdict)
	case protocol.TaskFailed:
		e.failedLocked(ctx, t, task)
	case protocol.TaskCancelled:
		if engineStopped {
			e.blockLocked(ctx, t, reason, false)
		} else {
			e.blockLocked(ctx, t, "the run was stopped by the operator", false)
		}
	}
}

func (e *Engine) succeededLocked(ctx context.Context, t *protocol.Ticket, task *protocol.Task, verdict string) {
	result := strings.TrimSpace(task.Result)
	t.Result = clip(result, 6000)
	inst, _ := e.db.Instance(ctx, task.InstanceID)
	author := "agent"
	if inst != nil {
		author = inst.Name
	}
	if result != "" {
		_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{TicketID: t.ID, AuthorID: task.InstanceID, AuthorName: author, Kind: "result", Body: result})
	}

	switch t.Kind {
	case protocol.TicketReview, protocol.TicketVerify:
		if verdict == "" {
			verdict = verdictFromText(result)
		}
		t.Verdict = verdict
		if result == "" {
			// A verdict with no words: say what it means, so the ticket's
			// page is not blank where the check should be.
			t.Result = verdictSentence(t, verdict)
			_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{TicketID: t.ID, AuthorID: task.InstanceID, AuthorName: author, Kind: "result", Body: t.Result})
		}
		t.Status = protocol.TicketDone
		_ = e.db.UpdateTicket(ctx, t)
		e.emit(t)
		e.applyVerdictLocked(ctx, t)
		e.onDoneLocked(ctx, t)
		return
	case protocol.TicketUnblock:
		t.Status = protocol.TicketDone
		_ = e.db.UpdateTicket(ctx, t)
		e.emit(t)
		e.unblockedLocked(ctx, t)
		e.onDoneLocked(ctx, t)
		return
	}

	// It created tickets of its own and waits on them: not done, waiting.
	if ok, _ := e.blockersDone(ctx, t); !ok {
		t.Wakes++
		if t.Wakes > e.cfg.MaxWakes {
			e.blockLocked(ctx, t, fmt.Sprintf("woken %d times for tickets it keeps creating", t.Wakes-1), true)
			return
		}
		t.Status = protocol.TicketTodo
		_ = e.db.UpdateTicket(ctx, t)
		e.systemComment(ctx, t, "Waiting on "+e.refList(ctx, t.BlockedBy)+"; this ticket comes back to "+author+" when they are done.")
		e.emit(t)
		return
	}

	if t.ReviewerID != "" && t.ReviewerID != task.InstanceID {
		t.Status = protocol.TicketInReview
		t.Rounds++
		_ = e.db.UpdateTicket(ctx, t)
		e.emit(t)
		e.openReviewLocked(ctx, t, inst)
		return
	}
	t.Status = protocol.TicketDone
	_ = e.db.UpdateTicket(ctx, t)
	e.emit(t)
	e.onDoneLocked(ctx, t)
}

// openReviewLocked creates the review ticket for a finished piece of work.
// The reviewer's verdict is the deliverable: pass finishes the work, fail
// sends it back with the findings.
func (e *Engine) openReviewLocked(ctx context.Context, t *protocol.Ticket, author *protocol.Instance) {
	by := "its author"
	if author != nil {
		by = author.Name
	}
	r := &protocol.Ticket{
		Title:       fmt.Sprintf("Review %s: %s", t.Ref(), t.Title),
		Description: fmt.Sprintf("%s finished %s. Review it the way a real user would and give a verdict.", by, t.Ref()),
		Kind:        protocol.TicketReview,
		Status:      protocol.TicketTodo,
		ParentID:    t.ParentID,
		TargetID:    t.ID,
		AssigneeID:  t.ReviewerID,
		OwnerID:     t.OwnerID,
		Thread:      t.Thread,
		Origin:      t.Origin,
		Stage:       "review",
		CreatedByID: t.AssigneeID,
	}
	if err := e.db.CreateTicket(ctx, r); err != nil {
		e.log.Warn("tickets: could not open a review", "ticket", t.Ref(), "err", err)
		return
	}
	e.systemComment(ctx, t, fmt.Sprintf("Round %d: %s opened for review.", t.Rounds, r.Ref()))
	e.emit(r)
}

// applyVerdictLocked acts on a finished review or verify ticket.
func (e *Engine) applyVerdictLocked(ctx context.Context, r *protocol.Ticket) {
	if r.TargetID == "" {
		return
	}
	target, err := e.db.Ticket(ctx, r.TargetID)
	if err != nil {
		return
	}
	switch r.Kind {
	case protocol.TicketVerify:
		msg := fmt.Sprintf("%s verified the stopped work: %s.", r.Ref(), verdictWord(r.Verdict))
		e.systemComment(ctx, target, msg)
		return
	case protocol.TicketReview:
	default:
		return
	}
	if target.Status != protocol.TicketInReview {
		return
	}
	if r.Verdict == protocol.VerdictPass {
		target.Status = protocol.TicketDone
		target.Verdict = protocol.VerdictPass
		_ = e.db.UpdateTicket(ctx, target)
		e.systemComment(ctx, target, r.Ref()+" passed it.")
		e.emit(target)
		e.onDoneLocked(ctx, target)
		return
	}
	// Failed: the findings go back to the author as the next round's brief.
	if target.Rounds >= e.cfg.MaxRounds {
		e.blockLocked(ctx, target, fmt.Sprintf("still failing review after %d rounds; last findings in %s", target.Rounds, r.Ref()), true)
		return
	}
	target.Status = protocol.TicketTodo
	target.Verdict = protocol.VerdictFail
	_ = e.db.UpdateTicket(ctx, target)
	_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{
		TicketID: target.ID, AuthorID: r.AssigneeID, AuthorName: e.nameOf(ctx, r.AssigneeID),
		Kind: "verdict", Body: "Review " + r.Ref() + " found problems to fix:\n" + r.Result,
	})
	e.emit(target)
	if e.hooks.Say != nil && target.Thread != "" && target.AssigneeID != "" {
		e.hooks.Say(ctx, target.Thread, r.AssigneeID, e.nameOf(ctx, r.AssigneeID), target.AssigneeID,
			fmt.Sprintf("%s: %s needs another round — findings are in %s.", target.Ref(), target.Title, r.Ref()))
	}
}

// unblockedLocked puts the work a manager was asked to unblock back in the
// queue, with the manager's note as its brief.
func (e *Engine) unblockedLocked(ctx context.Context, u *protocol.Ticket) {
	if u.TargetID == "" {
		return
	}
	target, err := e.db.Ticket(ctx, u.TargetID)
	if err != nil || target.Status != protocol.TicketBlocked {
		return
	}
	target.Status = protocol.TicketTodo
	target.BlockedReason = ""
	target.Attempts = 0
	target.StallFingerprint = ""
	_ = e.db.UpdateTicket(ctx, target)
	_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{
		TicketID: target.ID, AuthorID: u.AssigneeID, AuthorName: e.nameOf(ctx, u.AssigneeID),
		Kind: "comment", Body: "Unblocked by " + u.Ref() + ": " + u.Result,
	})
	e.emit(target)
}

func (e *Engine) failedLocked(ctx context.Context, t *protocol.Ticket, task *protocol.Task) {
	errText := strings.TrimSpace(task.Error)
	if SystemFailure(errText) && t.Attempts < e.cfg.MaxRetries {
		t.Attempts++
		t.Status = protocol.TicketTodo
		_ = e.db.UpdateTicket(ctx, t)
		note := fmt.Sprintf("Attempt %d stopped at step %d, not because of anything in the work: %s. It starts again from where it got to.",
			t.Attempts, task.Step, clip(errText, 200))
		if strings.Contains(errText, "without making progress") {
			note = fmt.Sprintf("Attempt %d stalled at step %d repeating one action that was not working (%s). The next attempt is told not to repeat it.",
				t.Attempts, task.Step, clip(errText, 160))
		}
		_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{TicketID: t.ID, AuthorName: "Oaf", Kind: "retry", Body: note})
		e.emit(t)
		if e.hooks.Say != nil && t.Thread != "" {
			inst, _ := e.db.Instance(ctx, task.InstanceID)
			if inst != nil {
				e.hooks.Say(ctx, t.Thread, inst.ID, inst.Name, "broadcast",
					fmt.Sprintf("My run on %s stopped at step %d (%s). Picking it back up where I left off.", t.Ref(), task.Step, clip(errText, 120)))
			}
		}
		return
	}
	e.blockLocked(ctx, t, "the run failed: "+clip(firstNonEmpty(errText, "no reason given"), 300), true)
}

// blockLocked stops a ticket for a reason someone has to act on, and makes
// sure someone hears: the assignee's manager gets an unblock ticket, and with
// no manager the operator gets an alert.
func (e *Engine) blockLocked(ctx context.Context, t *protocol.Ticket, reason string, escalate bool) {
	t.Status = protocol.TicketBlocked
	t.BlockedReason = reason
	_ = e.db.UpdateTicket(ctx, t)
	e.systemComment(ctx, t, "Blocked: "+reason)
	e.emit(t)
	if escalate {
		e.escalateLocked(ctx, t)
	}
}

// onDoneLocked wakes whatever was waiting on a finished ticket and closes a
// parent whose children are all finished.
func (e *Engine) onDoneLocked(ctx context.Context, t *protocol.Ticket) {
	deps, err := e.db.Dependents(ctx, t.ID)
	if err == nil {
		for i := range deps {
			d := &deps[i]
			if d.Status == protocol.TicketBacklog {
				if ok, _ := e.blockersDone(ctx, d); ok {
					d.Status = protocol.TicketTodo
					_ = e.db.UpdateTicket(ctx, d)
				}
			}
		}
	}
	e.rollupLocked(ctx, t.ParentID)
}

// rollupLocked finishes a parent that belongs to nobody -- the operator's
// request itself -- once every child is finished, and reports in its thread.
// A parent with an assignee is woken through its blocker edges instead.
func (e *Engine) rollupLocked(ctx context.Context, parentID string) {
	if parentID == "" {
		return
	}
	p, err := e.db.Ticket(ctx, parentID)
	if err != nil || p.Status.Terminal() || p.AssigneeID != "" || p.AssigneeUserID != "" {
		return
	}
	kids, err := e.db.Children(ctx, p.ID)
	if err != nil || len(kids) == 0 {
		return
	}
	var lines []string
	for _, k := range kids {
		if !k.Status.Terminal() {
			return
		}
		if k.Kind != protocol.TicketWork {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s %s (%s): %s", k.Ref(), k.Title, e.nameOf(ctx, k.AssigneeID), clip(summaryLine(k.Result), 160)))
	}
	p.Status = protocol.TicketDone
	p.Result = strings.Join(lines, "\n")
	_ = e.db.UpdateTicket(ctx, p)
	e.systemComment(ctx, p, "Every part is finished.")
	e.emit(p)
	if e.hooks.Say != nil && p.Thread != "" {
		e.hooks.Say(ctx, p.Thread, "", "Oaf", "broadcast", fmt.Sprintf("%s is done — %s\n%s", p.Ref(), p.Title, p.Result))
	}
	e.rollupLocked(ctx, p.ParentID)
}

// notePublished records what a run put in the catalog on the ticket it is
// working, so the hand-off to whoever waits on it can say so.
func (e *Engine) notePublished(ctx context.Context, instanceID string, item protocol.WorkItem) {
	e.mu.Lock()
	defer e.mu.Unlock()
	open, err := e.db.ListTickets(ctx, protocol.TicketFilter{AssigneeID: instanceID, Status: []protocol.TicketStatus{protocol.TicketInProgress}, Limit: 5})
	if err != nil || len(open) == 0 {
		return
	}
	t := &open[0]
	_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{
		TicketID: t.ID, AuthorID: instanceID, AuthorName: e.nameOf(ctx, instanceID), Kind: "published",
		Body: fmt.Sprintf("Published %s %q (version %d, %d bytes)", item.Kind, item.Name, item.Version, len(item.Content)),
	})
}

// ------------------------------------------------------------ helpers ---

func (e *Engine) systemComment(ctx context.Context, t *protocol.Ticket, body string) {
	_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{TicketID: t.ID, AuthorName: "Oaf", Kind: "system", Body: body})
}

func (e *Engine) emit(t *protocol.Ticket) {
	if e.hooks.Emit != nil {
		e.hooks.Emit("ticket", t.AssigneeID, t.TaskID, t)
	}
	// Whatever was reported about a ticket is over once it runs or ends.
	switch t.Status {
	case protocol.TicketInProgress:
		e.settleAlerts(t, t.Ref()+" is running now.")
	case protocol.TicketDone:
		e.settleAlerts(t, t.Ref()+" is done.")
	case protocol.TicketCancelled:
		e.settleAlerts(t, t.Ref()+" was cancelled.")
	}
}

// settleAlerts closes a ticket's open alerts and tells the consoles.
func (e *Engine) settleAlerts(t *protocol.Ticket, why string) {
	ids, err := e.db.ResolveTicketAlerts(context.Background(), t.ID, why)
	if err != nil || e.hooks.Emit == nil {
		return
	}
	for _, id := range ids {
		e.hooks.Emit("alert.resolved", t.AssigneeID, "", map[string]string{"alert_id": id, "by": "Oaf"})
	}
}

func (e *Engine) nameOf(ctx context.Context, instanceID string) string {
	if instanceID == "" {
		return "nobody"
	}
	if in, err := e.db.Instance(ctx, instanceID); err == nil {
		return in.Name
	}
	return "an agent that no longer exists"
}

func (e *Engine) refList(ctx context.Context, ids []string) string {
	var refs []string
	for _, id := range ids {
		if b, err := e.db.Ticket(ctx, id); err == nil && b.Status != protocol.TicketDone {
			refs = append(refs, b.Ref())
		}
	}
	if len(refs) == 0 {
		return "nothing"
	}
	return strings.Join(refs, ", ")
}

// verdictFromText reads a verdict out of a summary when the run did not set
// one: a leading PASS or FAIL, as the review brief asks for.
func verdictFromText(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(u, "PASS"):
		return protocol.VerdictPass
	case strings.HasPrefix(u, "FAIL"):
		return protocol.VerdictFail
	}
	// No verdict is not a pass: a reviewer that could not say the work is
	// fine has not said it.
	return protocol.VerdictFail
}

func verdictWord(v string) string {
	if v == protocol.VerdictPass {
		return "the stop is legitimate"
	}
	return "work was reopened"
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndexAny(cut, " \n"); i > n*3/4 {
		cut = cut[:i]
	}
	return cut + "…"
}

// summaryLine is the first line of a report that says something. Reports
// open with a heading more often than not, and a request's closing report
// that read "(Claude): ## ✓ DONE — T-16" for every part said nothing.
func summaryLine(s string) string {
	for _, ln := range strings.Split(strings.TrimSpace(s), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "---") {
			continue
		}
		bare := strings.ToLower(strings.Trim(t, "*_>-•✓✔✅ :.!\t"))
		if bare == "" || isDoneWord(bare) {
			continue
		}
		return strings.TrimLeft(t, "> ")
	}
	return firstLine(s)
}

func isDoneWord(s string) bool {
	for _, w := range []string{"done", "complete", "completed", "finished", "task complete", "task completed", "summary", "report", "result", "results"} {
		if s == w || strings.HasPrefix(s, w+" —") || strings.HasPrefix(s, w+" -") {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func verdictSentence(t *protocol.Ticket, verdict string) string {
	switch {
	case t.Kind == protocol.TicketVerify && verdict == protocol.VerdictPass:
		return "PASS — checked the work and found nothing to reopen."
	case t.Kind == protocol.TicketVerify:
		return "FAIL — some of the work was not finished; see the tickets it reopened."
	case verdict == protocol.VerdictPass:
		return "PASS — reviewed with no findings."
	case verdict == protocol.VerdictFail:
		return "FAIL — the reviewer gave no findings."
	}
	return "Finished without a verdict."
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
