package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Marathon runs: a step budget is a checkpoint, not a kill switch.
//
// The step budget exists so a confused agent cannot loop forever, and for a
// long time it was also the only thing that ended a run that was simply not
// finished: an agent building something over an afternoon hit step 60 and was
// told it had failed, with the work half-done on a desktop nobody would look at
// again. That is the wrong failure. What the budget should buy is a moment to
// take stock — write down what has been done and what is left — and then carry
// on in a fresh window with that summary in hand. The desktop is the state; the
// step counter was only ever ours.
//
// Each window is its own task, so the step log, screenshots and alerts stay
// browsable per window, and the chain is linked through ParamContinuationOf.
// A progress alert is filed at every boundary: a run that goes on for days is
// visible — and cancellable — rather than silent.

// maxProgressChars bounds the carried summary. It is re-summarised every
// window, so it stays a summary rather than a growing transcript.
const maxProgressChars = 3000

// continueOrFail is what happens when the loop runs out of steps with no
// verdict from the agent.
func (r *Runner) continueOrFail(ctx context.Context, task *protocol.Task, inst *protocol.Instance, history []turnSummary) {
	window := taskWindow(task)
	if !r.shouldContinue(task, window) {
		r.fail(ctx, task, fmt.Sprintf("step budget exhausted (%d steps, window %d)", task.MaxSteps, window))
		return
	}

	summary := r.progressSummary(ctx, task, inst, history)
	next := nextWindowTask(task, summary, window+1)

	// Detached from the loop's context for the same reason fail() is: the
	// writes that close this window must land even if the context that ran
	// it is on its way out.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	if err := r.db.CreateTask(wctx, next); err != nil {
		// No successor could be recorded, so this window IS the end. Say why
		// instead of leaving a "continued" task that continues into nothing.
		r.fail(ctx, task, "step budget exhausted and the next window could not be created: "+err.Error())
		return
	}

	_ = r.db.UpdateTaskState(wctx, task.ID, protocol.TaskContinued, task.Step, "", summary)
	r.bus.Emit("task.state", task.InstanceID, task.ID, map[string]any{
		"state": protocol.TaskContinued, "result": summary, "next_task_id": next.ID,
	})
	r.closeTaskAlerts(wctx, task.ID, "the agent moved to a fresh step window")
	r.fileAlert(wctx, task, protocol.AlertProgress, "info",
		fmt.Sprintf("Still working — window %d · %s", window+1, inst.Name), summary)
	r.log.Info("marathon: window closed, continuing", "task", task.ID, "next", next.ID, "window", window+1)

	if err := r.Start(context.WithoutCancel(ctx), next); err != nil {
		_ = r.db.UpdateTaskState(wctx, next.ID, protocol.TaskFailed, 0,
			"could not start the next window: "+err.Error(), "")
	}
}

// shouldContinue is the policy: marathon on, not opted out, under the cap.
func (r *Runner) shouldContinue(task *protocol.Task, window int) bool {
	if r.cfg == nil || !r.cfg.Marathon {
		return false
	}
	if strings.EqualFold(task.Params[protocol.ParamOnce], "true") {
		return false
	}
	if r.cfg.MarathonMaxWindows > 0 && window >= r.cfg.MarathonMaxWindows {
		return false
	}
	return true
}

// taskWindow is this task's position in its chain; a task with no chain is
// window 1.
func taskWindow(task *protocol.Task) int {
	if n, err := strconv.Atoi(task.Params[protocol.ParamWindow]); err == nil && n > 0 {
		return n
	}
	return 1
}

// nextWindowTask is the successor: same goal, skill, owner and parent, a
// fresh id and step counter, and the summary of everything so far.
func nextWindowTask(task *protocol.Task, progress string, window int) *protocol.Task {
	params := make(map[string]string, len(task.Params)+3)
	for k, v := range task.Params {
		params[k] = v
	}
	params[protocol.ParamProgress] = progress
	params[protocol.ParamContinuationOf] = task.ID
	params[protocol.ParamWindow] = strconv.Itoa(window)
	return &protocol.Task{
		ID:           store.NewID(),
		InstanceID:   task.InstanceID,
		OwnerID:      task.OwnerID,
		Goal:         task.Goal,
		SkillID:      task.SkillID,
		Params:       params,
		ParentTaskID: task.ParentTaskID,
		AutoRefine:   task.AutoRefine,
		State:        protocol.TaskQueued,
		MaxSteps:     task.MaxSteps,
		ProviderID:   task.ProviderID,
		CreatedAt:    time.Now().UTC(),
	}
}

// progressSummary asks the summarising role for a handover note; if no model
// can be reached, the raw recent history is the note. A window must never
// fail to close because a summary could not be written.
func (r *Runner) progressSummary(ctx context.Context, task *protocol.Task, inst *protocol.Instance, history []turnSummary) string {
	prior := strings.TrimSpace(task.Params[protocol.ParamProgress])
	recent := historyLines(history, 24)

	fallback := recent
	if prior != "" {
		fallback = prior + "\n\nLatest window's actions:\n" + recent
	}

	if r.models == nil {
		return clipProgress(fallback)
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.cfg.MarathonSummaryTimeout)
	defer cancel()
	resp, err := r.models.CompleteRole(sctx,
		connectors.PreferredChain(task.ProviderID, inst.ProviderIDs),
		protocol.RoleSummarize, connectors.Request{
			System: "You write the handover note an autonomous desktop agent leaves for " +
				"itself between work sessions on the same goal. Be concrete and short: " +
				"what has been DONE (with anything the next session must not redo), " +
				"what is IN PROGRESS and its exact state, what is LEFT, and any " +
				"gotchas discovered (paths, credentials it was told, things that " +
				"failed and why). Plain prose or a short list. No preamble.",
			Messages: []connectors.Message{{
				Role: connectors.RoleUser,
				Text: "GOAL\n" + task.Goal + "\n\nEARLIER PROGRESS NOTE\n" + orNone(prior) +
					"\n\nTHIS SESSION'S ACTIONS (newest last)\n" + recent,
			}},
			DisableThinking: true,
			MaxTokens:       600,
			Temperature:     0.2,
		})
	if err != nil || strings.TrimSpace(resp.Text) == "" {
		if err != nil {
			r.log.Warn("marathon: progress summary failed; carrying raw history", "task", task.ID, "err", err)
		}
		return clipProgress(fallback)
	}
	return clipProgress(strings.TrimSpace(resp.Text))
}

func historyLines(h []turnSummary, n int) string {
	if len(h) == 0 {
		return "(no actions recorded)"
	}
	if len(h) > n {
		h = h[len(h)-n:]
	}
	var sb strings.Builder
	for _, t := range h {
		fmt.Fprintf(&sb, "- step %d: %s → %s\n", t.Step, t.Action, clip(t.Outcome, 160))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// clipProgress keeps the TAIL: the newest state is the part the next window
// needs, and a note that has grown past the cap loses its oldest lines.
func clipProgress(s string) string {
	if len(s) <= maxProgressChars {
		return s
	}
	return "…" + s[len(s)-maxProgressChars:]
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none — this is the first window)"
	}
	return s
}

// continuationParamCount and isContinuationParam keep the chain's bookkeeping
// out of the PARAMETERS block the model reads as task inputs.
func continuationParamCount(params map[string]string) int {
	n := 0
	for k := range params {
		if isContinuationParam(k) {
			n++
		}
	}
	return n
}

func isContinuationParam(k string) bool {
	switch k {
	case protocol.ParamProgress, protocol.ParamContinuationOf, protocol.ParamWindow, protocol.ParamOnce:
		return true
	}
	return false
}
