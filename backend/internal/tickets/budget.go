package tickets

import (
	"context"
	"fmt"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Budgets: a monthly spend ceiling per agent and an optional one per ticket.
// At the warning percentage the operator is told; at the ceiling the agent is
// held -- its runs stopped, no new work dispatched -- and told again. Raising
// the ceiling, or the month turning, releases it. Auto mode is allowed;
// hidden spend is not.

// AfterTurn is called after every model turn that cost something. It keeps
// an agent from running past its ceiling by more than one turn.
func (e *Engine) AfterTurn(ctx context.Context, instanceID, taskID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if inst, err := e.db.Instance(ctx, instanceID); err == nil && inst.BudgetMonthUSD > 0 {
		e.budgetForLocked(ctx, inst)
	}
	if taskID == "" {
		return
	}
	t, err := e.db.TicketByTask(ctx, taskID)
	if err != nil || t.BudgetUSD <= 0 || t.TaskID != taskID {
		return
	}
	spent, err := e.db.TicketSpend(ctx, t.ID)
	if err != nil || spent < t.BudgetUSD {
		return
	}
	reason := fmt.Sprintf("the ticket has spent its budget ($%.2f of $%.2f)", spent, t.BudgetUSD)
	e.cancelling[taskID] = reason
	if !e.run.Cancel(taskID) {
		delete(e.cancelling, taskID)
	}
}

// applyBudgetsLocked checks every agent with a ceiling, and releases agents
// held at a ceiling that has been raised or a month that has turned.
func (e *Engine) applyBudgetsLocked(ctx context.Context) {
	insts, err := e.db.ListInstances(ctx)
	if err != nil {
		return
	}
	for i := range insts {
		in := &insts[i]
		if in.BudgetMonthUSD > 0 || in.Hold == protocol.HoldBudget {
			e.budgetForLocked(ctx, in)
		}
	}
}

func (e *Engine) budgetForLocked(ctx context.Context, in *protocol.Instance) {
	monthStart := store.MonthStart(e.now())
	month := monthStart.Format("2006-01")
	spent, err := e.db.InstanceSpend(ctx, in.ID, monthStart)
	if err != nil {
		return
	}
	over := in.BudgetMonthUSD > 0 && spent >= in.BudgetMonthUSD

	if !over && in.Hold == protocol.HoldBudget {
		e.releaseLocked(ctx, in, spent)
		return
	}
	if over {
		if in.Hold != protocol.HoldBudget {
			e.holdLocked(ctx, in, spent)
		}
		if first, _ := e.db.ClaimBudgetNotice(ctx, in.ID, month, "stop"); first {
			e.budgetAlertLocked(ctx, in, "critical",
				fmt.Sprintf("%s is held: monthly budget reached", in.Name),
				fmt.Sprintf("%s has spent $%.2f of its $%.2f ceiling this month. Its runs are stopped and its tickets wait. Raise the ceiling to let it carry on.", in.Name, spent, in.BudgetMonthUSD))
		}
		return
	}
	warn := in.BudgetMonthUSD * float64(in.BudgetWarnPct) / 100
	if in.BudgetMonthUSD > 0 && spent >= warn {
		if first, _ := e.db.ClaimBudgetNotice(ctx, in.ID, month, "warn"); first {
			e.budgetAlertLocked(ctx, in, "warn",
				fmt.Sprintf("%s has used %d%% of its budget", in.Name, int(100*spent/in.BudgetMonthUSD)),
				fmt.Sprintf("%s has spent $%.2f of its $%.2f monthly ceiling. At the ceiling it will be held.", in.Name, spent, in.BudgetMonthUSD))
		}
	}
}

func (e *Engine) holdLocked(ctx context.Context, in *protocol.Instance, spent float64) {
	_ = e.db.SetInstanceHold(ctx, in.ID, protocol.HoldBudget)
	in.Hold = protocol.HoldBudget
	reason := fmt.Sprintf("budget: %s reached its monthly ceiling ($%.2f of $%.2f)", in.Name, spent, in.BudgetMonthUSD)
	tasks, err := e.db.ListTasks(ctx, in.ID, 20)
	if err != nil {
		return
	}
	for _, k := range tasks {
		switch k.State {
		case protocol.TaskRunning, protocol.TaskQueued, protocol.TaskAwaitingHuman:
			e.cancelling[k.ID] = reason
			if !e.run.Cancel(k.ID) {
				delete(e.cancelling, k.ID)
				_ = e.db.UpdateTaskState(ctx, k.ID, protocol.TaskCancelled, k.Step, reason, "")
				if t, err := e.db.TicketByTask(ctx, k.ID); err == nil && t.TaskID == k.ID {
					if ok, _ := e.db.ReleaseTicket(ctx, t.ID, k.ID); ok {
						e.blockLocked(ctx, t, reason, false)
					}
				}
			}
		}
	}
	e.log.Info("agent held at its budget", "agent", in.Name, "spent", spent, "ceiling", in.BudgetMonthUSD)
}

func (e *Engine) releaseLocked(ctx context.Context, in *protocol.Instance, spent float64) {
	_ = e.db.SetInstanceHold(ctx, in.ID, protocol.HoldNone)
	in.Hold = protocol.HoldNone
	blocked, err := e.db.ListTickets(ctx, protocol.TicketFilter{AssigneeID: in.ID, Status: []protocol.TicketStatus{protocol.TicketBlocked}})
	if err == nil {
		for i := range blocked {
			t := &blocked[i]
			if strings.HasPrefix(t.BlockedReason, "budget") {
				t.Status = protocol.TicketTodo
				t.BlockedReason = ""
				t.StallFingerprint = ""
				_ = e.db.UpdateTicket(ctx, t)
				e.systemComment(ctx, t, in.Name+" is no longer held at its budget; the ticket goes back in the queue.")
				e.emit(t)
			}
		}
	}
	e.log.Info("agent released from its budget hold", "agent", in.Name, "spent", spent, "ceiling", in.BudgetMonthUSD)
}

func (e *Engine) budgetAlertLocked(ctx context.Context, in *protocol.Instance, sev, title, body string) {
	a := &protocol.Alert{Kind: protocol.AlertBudget, Severity: sev, InstanceID: in.ID, Title: title, Body: body}
	if err := e.db.CreateAlert(ctx, a); err == nil && e.hooks.Alert != nil {
		e.hooks.Alert(ctx, a)
	}
}

// BudgetChanged re-arms the notices for an agent whose ceiling was just
// edited and re-applies it at once, so raising a ceiling releases a held
// agent without waiting for the next pass.
func (e *Engine) BudgetChanged(ctx context.Context, instanceID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	month := store.MonthStart(e.now()).Format("2006-01")
	_ = e.db.ClearBudgetNotices(ctx, instanceID, month)
	if in, err := e.db.Instance(ctx, instanceID); err == nil {
		e.budgetForLocked(ctx, in)
	}
	e.Kick()
}
