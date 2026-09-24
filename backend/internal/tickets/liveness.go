package tickets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The liveness promise: no unfinished ticket is left where nobody is
// responsible for the next move and nothing will wake it. When the engine
// cannot say what moves a ticket forward, it says so -- once per stopped
// state, to whoever can act -- instead of guessing or going quiet.

func (e *Engine) livenessLocked(ctx context.Context) {
	open, err := e.db.OpenTickets(ctx)
	if err != nil {
		return
	}
	openReviews := map[string]bool{}
	openUnblocks := map[string]bool{}
	for _, t := range open {
		switch t.Kind {
		case protocol.TicketReview:
			openReviews[t.TargetID] = true
		case protocol.TicketUnblock:
			openUnblocks[t.TargetID] = true
		}
	}
	for i := range open {
		t := &open[i]
		switch t.Status {
		case protocol.TicketTodo:
			e.checkTodoLocked(ctx, t)
		case protocol.TicketBacklog:
			// Only the engine's own parking spot: a tester that answered a
			// request before any builder waits in a review with nothing to
			// review. A ticket a person put in the backlog is there on
			// purpose, and raising an alert and a push about it every time
			// was noise.
			if t.Kind == protocol.TicketReview && t.TargetID == "" && len(t.BlockedBy) == 0 &&
				e.now().Sub(t.CreatedAt) > e.cfg.BacklogPatience {
				e.surfaceLocked(ctx, t, "backlog-orphan",
					fmt.Sprintf("%s is waiting for work nobody is making", t.Ref()),
					fmt.Sprintf("%s (%s) is a review waiting for the work it reviews, and nobody has taken that work. Give someone the part that makes it, or cancel the review.", t.Ref(), t.Title))
			}
		case protocol.TicketInReview:
			if !openReviews[t.ID] && t.ReviewerID != "" {
				// The review went missing -- deleted, cancelled. Open it again
				// rather than leaving the work in review forever.
				author, _ := e.db.Instance(ctx, t.AssigneeID)
				e.openReviewLocked(ctx, t, author)
			} else if !openReviews[t.ID] {
				t.Status = protocol.TicketDone
				_ = e.db.UpdateTicket(ctx, t)
				e.onDoneLocked(ctx, t)
			}
		case protocol.TicketBlocked:
			if !openUnblocks[t.ID] && t.StallFingerprint == "" && t.BlockedReason != "" {
				e.escalateLocked(ctx, t)
			}
		}
	}
}

// ownerlessGrace is how long an unassigned ticket may sit before it is
// reported as having nobody to start it.
const ownerlessGrace = 2 * time.Minute

func (e *Engine) checkTodoLocked(ctx context.Context, t *protocol.Ticket) {
	if t.AssigneeID == "" && t.AssigneeUserID == "" {
		kids, _ := e.db.Children(ctx, t.ID)
		for _, k := range kids {
			if !k.Status.Terminal() {
				return // a request whose parts are in hand
			}
		}
		if len(kids) > 0 {
			e.rollupLocked(ctx, t.ID)
			return
		}
		// A request's root exists a moment before its first part is filed
		// under it, and a ticket made on the board is often assigned a
		// moment after it is made. Neither is ownerless yet.
		if e.now().Sub(t.UpdatedAt) < ownerlessGrace {
			return
		}
		e.surfaceLocked(ctx, t, "unowned",
			fmt.Sprintf("Nobody owns %s", t.Ref()),
			fmt.Sprintf("%s (%s) has no assignee, so nothing will start it. Assign it to an agent or a person.", t.Ref(), t.Title))
		return
	}
	if t.AssigneeID == "" {
		return // a person's ticket: they close it
	}
	inst, err := e.db.Instance(ctx, t.AssigneeID)
	if err != nil {
		e.surfaceLocked(ctx, t, "assignee-gone",
			fmt.Sprintf("%s is assigned to an agent that no longer exists", t.Ref()),
			fmt.Sprintf("%s (%s) cannot start: its assignee was deleted. Reassign it.", t.Ref(), t.Title))
		return
	}
	if inst.Hold == protocol.HoldBudget {
		e.surfaceLocked(ctx, t, "held-budget",
			fmt.Sprintf("%s waits for %s, who is at its budget", t.Ref(), inst.Name),
			fmt.Sprintf("%s (%s) is ready, but %s reached its monthly spend ceiling and is held. Raise the ceiling or reassign the ticket.", t.Ref(), t.Title, inst.Name))
		return
	}
	if ok, why := e.run.Ready(ctx, inst); !ok {
		e.surfaceLocked(ctx, t, "not-ready:"+why,
			fmt.Sprintf("%s waits for %s: %s", t.Ref(), inst.Name, why),
			fmt.Sprintf("%s (%s) is ready to run, but %s cannot take it: %s.", t.Ref(), t.Title, inst.Name, why))
		return
	}
	for _, id := range t.BlockedBy {
		b, err := e.db.Ticket(ctx, id)
		if err != nil {
			e.surfaceLocked(ctx, t, "blocker-gone:"+id,
				fmt.Sprintf("%s waits on a ticket that no longer exists", t.Ref()),
				fmt.Sprintf("%s (%s) is blocked by a deleted ticket. Remove the blocker.", t.Ref(), t.Title))
			return
		}
		if b.Status == protocol.TicketCancelled {
			// Cancelled is terminal for the blocker, but it does not satisfy
			// the dependency: the thing waited for never happened.
			e.surfaceLocked(ctx, t, "blocker-cancelled:"+id,
				fmt.Sprintf("%s waits on %s, which was cancelled", t.Ref(), b.Ref()),
				fmt.Sprintf("%s (%s) can never start: %s was cancelled. Remove the blocker or reopen it.", t.Ref(), t.Title, b.Ref()))
			return
		}
	}
}

// surfaceLocked tells the operator (and the thread) once that a ticket has
// no next move, keyed by what is wrong so a new problem is reported and a
// repeat is not.
func (e *Engine) surfaceLocked(ctx context.Context, t *protocol.Ticket, key, title, body string) {
	fp := fingerprint(key)
	if t.StallFingerprint == fp {
		return
	}
	t.StallFingerprint = fp
	_ = e.db.UpdateTicket(ctx, t)
	a := &protocol.Alert{Kind: protocol.AlertStalled, Severity: "warn", InstanceID: t.AssigneeID, Title: title, Body: body}
	if err := e.db.CreateAlert(ctx, a); err == nil && e.hooks.Alert != nil {
		e.hooks.Alert(ctx, a)
	}
	e.systemComment(ctx, t, body)
	if e.hooks.Say != nil && t.Thread != "" {
		e.hooks.Say(ctx, t.Thread, "", "Oaf", "broadcast", body)
	}
	e.log.Info("ticket needs attention", "ticket", t.Ref(), "why", key)
}

// escalateLocked sends a blocked ticket up the org chart: the assignee's
// manager gets an unblock ticket. With no manager -- or a manager that cannot
// take work -- the operator is told.
func (e *Engine) escalateLocked(ctx context.Context, t *protocol.Ticket) {
	fp := fingerprint("blocked:" + t.BlockedReason)
	if t.StallFingerprint == fp {
		return
	}
	var manager *protocol.Instance
	if t.AssigneeID != "" {
		if inst, err := e.db.Instance(ctx, t.AssigneeID); err == nil && inst.ReportsTo != "" {
			if m, err := e.db.Instance(ctx, inst.ReportsTo); err == nil && m.Hold == protocol.HoldNone && m.ID != inst.ID {
				manager = m
			}
		}
	}
	if manager == nil || t.Kind == protocol.TicketUnblock {
		e.surfaceLocked(ctx, t, "blocked:"+t.BlockedReason,
			fmt.Sprintf("%s is blocked", t.Ref()),
			fmt.Sprintf("%s (%s, %s) is blocked: %s. It needs you.", t.Ref(), t.Title, e.nameOf(ctx, t.AssigneeID), t.BlockedReason))
		return
	}
	t.StallFingerprint = fp
	_ = e.db.UpdateTicket(ctx, t)
	u := &protocol.Ticket{
		Title:       fmt.Sprintf("Unblock %s: %s", t.Ref(), t.Title),
		Description: t.BlockedReason,
		Kind:        protocol.TicketUnblock,
		Status:      protocol.TicketTodo,
		ParentID:    t.ParentID,
		TargetID:    t.ID,
		AssigneeID:  manager.ID,
		OwnerID:     t.OwnerID,
		Thread:      t.Thread,
		Origin:      t.Origin,
	}
	if err := e.db.CreateTicket(ctx, u); err != nil {
		e.log.Warn("tickets: could not escalate", "ticket", t.Ref(), "err", err)
		return
	}
	e.systemComment(ctx, t, fmt.Sprintf("Escalated to %s as %s.", manager.Name, u.Ref()))
	e.emit(u)
	if e.hooks.Say != nil && t.Thread != "" {
		e.hooks.Say(ctx, t.Thread, "", "Oaf", manager.ID,
			fmt.Sprintf("%s is blocked (%s). Over to %s, who %s reports to: %s.", t.Ref(), clip(t.BlockedReason, 140), manager.Name, e.nameOf(ctx, t.AssigneeID), u.Ref()))
	}
}

// -------------------------------------------------------------- verifier ---

// verifiersLocked wakes a ticket's verifier when the ticket's whole subtree
// has come to rest in a state it has not already checked. Agents stop for the
// wrong reasons -- "done" without proof, a blocker that is not real -- and
// none of those wakes anyone on its own.
func (e *Engine) verifiersLocked(ctx context.Context) {
	candidates, err := e.db.ListTickets(ctx, protocol.TicketFilter{Limit: 400})
	if err != nil {
		return
	}
	openVerify := map[string]int{}
	allVerify := map[string]int{}
	for _, t := range candidates {
		if t.Kind == protocol.TicketVerify {
			allVerify[t.TargetID]++
			if !t.Status.Terminal() {
				openVerify[t.TargetID]++
			}
		}
	}
	for i := range candidates {
		root := &candidates[i]
		if root.VerifierID == "" || root.Status == protocol.TicketCancelled || openVerify[root.ID] > 0 {
			continue
		}
		tree, err := e.db.Subtree(ctx, root.ID)
		if err != nil || !e.atRest(ctx, tree) {
			continue
		}
		fp := subtreeFingerprint(tree, root.VerifierID)
		if fp == root.VerifiedFingerprint {
			continue
		}
		if allVerify[root.ID] >= 5 {
			e.surfaceLocked(ctx, root, "verifier-exhausted",
				fmt.Sprintf("%s has been verified five times and keeps stopping", root.Ref()),
				fmt.Sprintf("The work under %s has been reopened by its verifier five times. Look at it yourself.", root.Ref()))
			root.VerifiedFingerprint = fp
			_ = e.db.UpdateTicket(ctx, root)
			continue
		}
		v := &protocol.Ticket{
			Title:      fmt.Sprintf("Verify %s: %s", root.Ref(), root.Title),
			Kind:       protocol.TicketVerify,
			Status:     protocol.TicketTodo,
			ParentID:   root.ID,
			TargetID:   root.ID,
			AssigneeID: root.VerifierID,
			OwnerID:    root.OwnerID,
			Thread:     root.Thread,
			Origin:     root.Origin,
		}
		if err := e.db.CreateTicket(ctx, v); err != nil {
			continue
		}
		root.VerifiedFingerprint = fp
		_ = e.db.UpdateTicket(ctx, root)
		e.systemComment(ctx, root, fmt.Sprintf("The work stopped; %s asks %s to check it.", v.Ref(), e.nameOf(ctx, root.VerifierID)))
		e.emit(v)
	}
}

// atRest reports whether nothing in a subtree will move by itself.
func (e *Engine) atRest(ctx context.Context, tree []protocol.Ticket) bool {
	for i := range tree {
		t := &tree[i]
		switch t.Status {
		case protocol.TicketInProgress:
			return false
		case protocol.TicketTodo:
			if t.AssigneeID == "" {
				continue
			}
			if ok, _ := e.blockersDone(ctx, t); ok {
				if inst, err := e.db.Instance(ctx, t.AssigneeID); err == nil && inst.Hold == protocol.HoldNone {
					return false // it will run
				}
			} else {
				// Waiting on a blocker: at rest only if that blocker is.
				for _, id := range t.BlockedBy {
					if b, err := e.db.Ticket(ctx, id); err == nil && !b.Status.Terminal() && b.Status != protocol.TicketBlocked {
						return false
					}
				}
			}
		case protocol.TicketBacklog:
			if e.now().Sub(t.CreatedAt) < e.cfg.BacklogPatience {
				return false
			}
		}
	}
	return true
}

func subtreeFingerprint(tree []protocol.Ticket, verifier string) string {
	parts := make([]string, 0, len(tree)+1)
	for _, t := range tree {
		parts = append(parts, fmt.Sprintf("%s|%s|%s|%d|%s", t.ID, t.Status, t.Verdict, len(t.Result), strings.Join(t.BlockedBy, ",")))
	}
	sort.Strings(parts)
	parts = append(parts, "verifier="+verifier)
	return fingerprint(strings.Join(parts, "\n"))
}

func fingerprint(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:12])
}
