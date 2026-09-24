package tickets

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Operations people and agents perform on tickets. Each takes the engine's
// lock, validates, writes, and kicks a pass so the effect is immediate.

// ErrRefused is an operation the caller is not allowed to perform, wrapped
// with the reason.
var ErrRefused = errors.New("refused")

// Actor is who is acting: a person, an agent, or the system.
type Actor struct {
	UserID     string
	InstanceID string
	Name       string
}

// Create stores a new ticket after checking what it refers to exists.
func (e *Engine) Create(ctx context.Context, t *protocol.Ticket, by Actor) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.validateRefsLocked(ctx, t); err != nil {
		return err
	}
	if t.Status == "" {
		t.Status = protocol.TicketTodo
	}
	t.CreatedByID, t.CreatedByUserID = by.InstanceID, by.UserID
	if err := e.db.CreateTicket(ctx, t); err != nil {
		return err
	}
	e.systemComment(ctx, t, fmt.Sprintf("Created by %s.", firstNonEmpty(by.Name, "the operator")))
	e.emit(t)
	e.Kick()
	return nil
}

func (e *Engine) validateRefsLocked(ctx context.Context, t *protocol.Ticket) error {
	for _, id := range []string{t.AssigneeID, t.ReviewerID, t.VerifierID} {
		if id == "" {
			continue
		}
		if _, err := e.db.Instance(ctx, id); err != nil {
			return fmt.Errorf("%w: no agent %s", store.ErrInvalid, id)
		}
	}
	if t.ParentID != "" {
		if _, err := e.db.Ticket(ctx, t.ParentID); err != nil {
			return fmt.Errorf("%w: no parent ticket %s", store.ErrInvalid, t.ParentID)
		}
	}
	for _, b := range t.BlockedBy {
		if _, err := e.db.Ticket(ctx, b); err != nil {
			return fmt.Errorf("%w: no blocker ticket %s", store.ErrInvalid, b)
		}
	}
	if t.Kind != "" && !t.Kind.Valid() {
		return fmt.Errorf("%w: unknown kind %q", store.ErrInvalid, t.Kind)
	}
	return nil
}

// Patch is a partial update. Nil fields are left alone.
type Patch struct {
	Title       *string
	Description *string
	Status      *protocol.TicketStatus
	Priority    *int
	AssigneeID  *string
	ReviewerID  *string
	VerifierID  *string
	ParentID    *string
	BudgetUSD   *float64
	BlockedBy   *[]string
}

// Update applies a patch. A status change to cancelled stops a live run;
// moving a blocked or finished ticket back to todo clears what stopped it.
func (e *Engine) Update(ctx context.Context, id string, p Patch, by Actor) (*protocol.Ticket, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.Ticket(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Title != nil {
		t.Title = strings.TrimSpace(*p.Title)
	}
	if p.Description != nil {
		t.Description = *p.Description
	}
	if p.Priority != nil {
		t.Priority = *p.Priority
	}
	if p.AssigneeID != nil {
		t.AssigneeID = *p.AssigneeID
		t.StallFingerprint = ""
	}
	if p.ReviewerID != nil {
		t.ReviewerID = *p.ReviewerID
	}
	if p.VerifierID != nil {
		t.VerifierID = *p.VerifierID
		t.VerifiedFingerprint = ""
	}
	if p.ParentID != nil {
		if *p.ParentID == t.ID {
			return nil, fmt.Errorf("%w: a ticket cannot be its own parent", store.ErrInvalid)
		}
		t.ParentID = *p.ParentID
	}
	if p.BudgetUSD != nil {
		t.BudgetUSD = *p.BudgetUSD
	}
	if err := e.validateRefsLocked(ctx, &protocol.Ticket{AssigneeID: t.AssigneeID, ReviewerID: t.ReviewerID, VerifierID: t.VerifierID, ParentID: t.ParentID}); err != nil {
		return nil, err
	}
	if p.BlockedBy != nil {
		if err := e.db.SetBlockers(ctx, t.ID, *p.BlockedBy); err != nil {
			return nil, err
		}
		t.BlockedBy = *p.BlockedBy
	}
	if p.Status != nil && *p.Status != t.Status {
		next := *p.Status
		if !next.Valid() {
			return nil, fmt.Errorf("%w: unknown status %q", store.ErrInvalid, next)
		}
		if next == protocol.TicketInProgress {
			return nil, fmt.Errorf("%w: a ticket goes in progress when a run takes it; set it to todo", store.ErrInvalid)
		}
		if t.TaskID != "" {
			// Release first, then stop the run: a run that no longer holds
			// its ticket finishes nothing, so this decision stands.
			held := t.TaskID
			if _, err := e.db.ReleaseTicket(ctx, t.ID, held); err == nil {
				t.TaskID = ""
			}
			e.run.Cancel(held)
		}
		t.Status = next
		switch next {
		case protocol.TicketTodo, protocol.TicketBacklog:
			t.BlockedReason, t.StallFingerprint = "", ""
			t.Attempts = 0
		case protocol.TicketBlocked:
			if t.BlockedReason == "" {
				t.BlockedReason = "blocked by " + firstNonEmpty(by.Name, "the operator")
			}
		}
		e.systemComment(ctx, t, fmt.Sprintf("%s moved it to %s.", firstNonEmpty(by.Name, "The operator"), next))
		if next == protocol.TicketDone {
			defer e.onDoneLocked(ctx, t)
		}
	}
	if err := e.db.UpdateTicket(ctx, t); err != nil {
		return nil, err
	}
	e.emit(t)
	if t.Status == protocol.TicketCancelled {
		e.cancelUnderLocked(ctx, t)
	}
	e.Kick()
	return t, nil
}

// cancelUnderLocked cancels what a cancelled ticket was for: its open parts,
// and the reviews and verifications of any of it. Cancelling a stuck verify
// and then its request woke the verifier again in between; it reopened a part
// of the cancelled request, which ran again, while a new request went by.
func (e *Engine) cancelUnderLocked(ctx context.Context, t *protocol.Ticket) {
	tree, err := e.db.Subtree(ctx, t.ID)
	if err != nil {
		return
	}
	in := map[string]bool{t.ID: true}
	var stop []*protocol.Ticket
	for i := range tree {
		in[tree[i].ID] = true
		if tree[i].ID != t.ID && !tree[i].Status.Terminal() {
			stop = append(stop, &tree[i])
		}
	}
	if open, err := e.db.OpenTickets(ctx); err == nil {
		for i := range open {
			c := &open[i]
			if in[c.TargetID] && !in[c.ID] && (c.Kind == protocol.TicketVerify || c.Kind == protocol.TicketReview) {
				stop = append(stop, c)
			}
		}
	}
	for _, c := range stop {
		if c.TaskID != "" {
			held := c.TaskID
			if _, err := e.db.ReleaseTicket(ctx, c.ID, held); err == nil {
				c.TaskID = ""
			}
			e.run.Cancel(held)
		}
		c.Status = protocol.TicketCancelled
		if err := e.db.UpdateTicket(ctx, c); err != nil {
			continue
		}
		e.systemComment(ctx, c, t.Ref()+" was cancelled, so this was too.")
		e.emit(c)
	}
}

// Comment adds to a ticket's thread.
func (e *Engine) Comment(ctx context.Context, id, body string, by Actor) (*protocol.TicketComment, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.Ticket(ctx, id)
	if err != nil {
		return nil, err
	}
	c := &protocol.TicketComment{TicketID: t.ID, AuthorID: by.InstanceID, AuthorUserID: by.UserID, AuthorName: firstNonEmpty(by.Name, "operator"), Kind: "comment", Body: body}
	if err := e.db.AddTicketComment(ctx, c); err != nil {
		return nil, err
	}
	if e.hooks.Emit != nil {
		e.hooks.Emit("ticket.comment", t.AssigneeID, t.TaskID, c)
	}
	return c, nil
}

// Reopen sends finished or stopped work back to its assignee with a reason:
// what a verifier, a manager or the operator found missing.
func (e *Engine) Reopen(ctx context.Context, id, reason string, by Actor) (*protocol.Ticket, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.Ticket(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := e.reopenLocked(ctx, t, reason, by); err != nil {
		return nil, err
	}
	e.Kick()
	return t, nil
}

// reopenLocked sends a ticket back and says who it went back to. A request
// nobody is assigned is reopened through its finished parts, which is what
// its claims were: a verifier that reopened the request itself reopened it
// "for nobody", and nothing ever picked it up.
func (e *Engine) reopenLocked(ctx context.Context, t *protocol.Ticket, reason string, by Actor) (string, error) {
	if t.Status == protocol.TicketInProgress {
		return "", fmt.Errorf("%w: %s is being worked on right now", ErrRefused, t.Ref())
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("%w: say what is missing", store.ErrInvalid)
	}
	if t.AssigneeID == "" && t.AssigneeUserID == "" {
		kids, _ := e.db.Children(ctx, t.ID)
		var sent []string
		for i := range kids {
			k := &kids[i]
			if k.Kind != protocol.TicketWork || k.Status != protocol.TicketDone {
				continue
			}
			if who, err := e.reopenLocked(ctx, k, reason, by); err == nil {
				sent = append(sent, k.Ref()+" ("+who+")")
			}
		}
		if len(sent) > 0 {
			return strings.Join(sent, ", "), nil
		}
	}
	t.Status = protocol.TicketTodo
	t.Verdict = protocol.VerdictFail
	t.BlockedReason, t.StallFingerprint = "", ""
	t.Attempts = 0
	if err := e.db.UpdateTicket(ctx, t); err != nil {
		return "", err
	}
	_ = e.db.AddTicketComment(ctx, &protocol.TicketComment{
		TicketID: t.ID, AuthorID: by.InstanceID, AuthorUserID: by.UserID, AuthorName: firstNonEmpty(by.Name, "operator"),
		Kind: "verdict", Body: "Reopened by " + firstNonEmpty(by.Name, "the operator") + ": " + reason,
	})
	e.emit(t)
	// Whatever above it was already finished is not finished any more: a
	// request or a manager's ticket that waited on this work waits again.
	cur := t.ParentID
	for cur != "" {
		p, err := e.db.Ticket(ctx, cur)
		if err != nil {
			break
		}
		waits := p.AssigneeID == ""
		for _, b := range p.BlockedBy {
			if b == t.ID {
				waits = true
			}
		}
		if !waits || p.Status != protocol.TicketDone {
			break
		}
		p.Status = protocol.TicketTodo
		_ = e.db.UpdateTicket(ctx, p)
		e.systemComment(ctx, p, t.Ref()+" was reopened, so this waits again.")
		e.emit(p)
		cur = p.ParentID
	}
	return e.nameOf(ctx, t.AssigneeID), nil
}

// Delete removes a ticket, stopping its run.
func (e *Engine) Delete(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.Ticket(ctx, id)
	if err != nil {
		return err
	}
	if t.TaskID != "" {
		held := t.TaskID
		_, _ = e.db.ReleaseTicket(ctx, t.ID, held)
		e.run.Cancel(held)
	}
	if d, ok := e.db.(interface {
		DeleteTicket(ctx context.Context, id string) error
	}); ok {
		return d.DeleteTicket(ctx, id)
	}
	return fmt.Errorf("delete is not supported by this store")
}

// ---------------------------------------------------------- agent actions ---

// RequiresVerdict reports whether a run must finish with a verdict: it holds
// a review or verify ticket.
func (e *Engine) RequiresVerdict(ctx context.Context, taskID string) bool {
	t, err := e.db.TicketByTask(ctx, taskID)
	if err != nil {
		return false
	}
	return t.Kind == protocol.TicketReview || t.Kind == protocol.TicketVerify
}

// CreateFromAgent is the create_ticket action: an agent hands part of its work
// to a colleague. The new ticket is a child of the ticket the agent is on,
// and by default that ticket waits for it.
func (e *Engine) CreateFromAgent(ctx context.Context, taskID string, inst *protocol.Instance, target, title, text string, wait bool) (string, error) {
	if inst.Trust == protocol.TrustLow {
		return "", fmt.Errorf("%w: a low-trust agent cannot hand work to others; put what you found in your summary instead", ErrRefused)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = clip(firstLine(text), 90)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%w: create_ticket needs text with complete instructions", store.ErrInvalid)
	}
	all, err := e.db.ListInstances(ctx)
	if err != nil {
		return "", err
	}
	var assignee *protocol.Instance
	want := strings.ToLower(strings.TrimSpace(target))
	for i := range all {
		if strings.ToLower(all[i].Name) == want || all[i].ID == target {
			assignee = &all[i]
			break
		}
	}
	if assignee == nil {
		var names []string
		for _, in := range all {
			if in.ID != inst.ID {
				names = append(names, in.Name)
			}
		}
		return "", fmt.Errorf("%w: no agent called %q; the fleet has %s", store.ErrInvalid, target, strings.Join(names, ", "))
	}
	if assignee.ID == inst.ID {
		return "", fmt.Errorf("%w: that is you; do the work instead of ticketing it to yourself", store.ErrInvalid)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	var parent *protocol.Ticket
	if taskID != "" {
		if p, err := e.db.TicketByTask(ctx, taskID); err == nil {
			parent = p
		}
	}
	t := &protocol.Ticket{
		Title: title, Description: text, Kind: protocol.TicketWork, Status: protocol.TicketTodo,
		AssigneeID: assignee.ID, CreatedByID: inst.ID,
	}
	if parent != nil {
		t.ParentID, t.OwnerID, t.Thread, t.Origin = parent.ID, parent.OwnerID, parent.Thread, parent.Origin
	}
	if err := e.db.CreateTicket(ctx, t); err != nil {
		return "", err
	}
	e.systemComment(ctx, t, fmt.Sprintf("Created by %s.", inst.Name))
	e.emit(t)
	if parent != nil && wait {
		if err := e.db.AddBlocker(ctx, parent.ID, t.ID); err == nil {
			e.systemComment(ctx, parent, fmt.Sprintf("%s handed %s to %s; this ticket waits for it.", inst.Name, t.Ref(), assignee.Name))
		}
	}
	if e.hooks.Say != nil && t.Thread != "" {
		e.hooks.Say(ctx, t.Thread, inst.ID, inst.Name, assignee.ID, fmt.Sprintf("%s — over to you, %s: %s", t.Ref(), assignee.Name, t.Title))
	}
	e.Kick()
	msg := fmt.Sprintf("created %s for %s: %s", t.Ref(), assignee.Name, t.Title)
	if parent != nil && wait {
		msg += fmt.Sprintf(". %s now waits for it and comes back to you when %s finishes; you can finish this run with done now", parent.Ref(), assignee.Name)
	}
	return msg, nil
}

// ReopenFromAgent is the reopen_ticket action. A verifier may reopen work in
// the subtree it watches; a manager, work of anyone who reports to it; a
// reviewer, the work it reviews.
func (e *Engine) ReopenFromAgent(ctx context.Context, taskID string, inst *protocol.Instance, ref, reason string) (string, error) {
	if inst.Trust == protocol.TrustLow {
		return "", fmt.Errorf("%w: a low-trust agent cannot reopen work", ErrRefused)
	}
	n, ok := protocol.ParseTicketRef(ref)
	if !ok {
		return "", fmt.Errorf("%w: %q is not a ticket number (T-12)", store.ErrInvalid, ref)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	t, err := e.db.TicketByNumber(ctx, n)
	if err != nil {
		return "", fmt.Errorf("%w: no ticket %s", store.ErrInvalid, ref)
	}
	if !e.mayReopenLocked(ctx, taskID, inst, t) {
		return "", fmt.Errorf("%w: %s is not yours to reopen — you can reopen work you verify, review, or that someone reporting to you owns", ErrRefused, t.Ref())
	}
	who, err := e.reopenLocked(ctx, t, reason, Actor{InstanceID: inst.ID, Name: inst.Name})
	if err != nil {
		return "", err
	}
	e.Kick()
	return fmt.Sprintf("reopened %s for %s: %s", t.Ref(), who, clip(reason, 120)), nil
}

func (e *Engine) mayReopenLocked(ctx context.Context, taskID string, inst *protocol.Instance, t *protocol.Ticket) bool {
	if own, err := e.db.TicketByTask(ctx, taskID); err == nil {
		switch own.Kind {
		case protocol.TicketVerify, protocol.TicketReview:
			// Anything in the subtree it is checking.
			tree, _ := e.db.Subtree(ctx, own.TargetID)
			for _, k := range tree {
				if k.ID == t.ID {
					return true
				}
			}
		}
	}
	// A manager, over work its reports own, however far down.
	cur := t.AssigneeID
	seen := map[string]bool{}
	for cur != "" && !seen[cur] {
		seen[cur] = true
		in, err := e.db.Instance(ctx, cur)
		if err != nil {
			return false
		}
		if in.ReportsTo == inst.ID {
			return true
		}
		cur = in.ReportsTo
	}
	return false
}
