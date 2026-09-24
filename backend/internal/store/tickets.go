package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Tickets: work as durable rows. See migrations/0038_org_and_tickets.sql.

const ticketCols = `id,number,title,description,kind,status,priority,
    COALESCE(parent_id,''),COALESCE(target_id,''),COALESCE(assignee_id,''),COALESCE(assignee_user_id,''),
    COALESCE(reviewer_id,''),COALESCE(verifier_id,''),verified_fingerprint,stall_fingerprint,
    COALESCE(created_by_id,''),COALESCE(created_by_user_id,''),owner_id,thread,origin,stage,
    COALESCE(task_id,''),attempts,rounds,wakes,verdict,result,blocked_reason,budget_usd,
    created_at,updated_at,started_at,done_at`

const ticketSelect = `SELECT ` + ticketCols + ` FROM tickets`

func scanTickets(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]protocol.Ticket, error) {
	out := []protocol.Ticket{}
	for rows.Next() {
		var t protocol.Ticket
		var kind, status string
		if err := rows.Scan(&t.ID, &t.Number, &t.Title, &t.Description, &kind, &status, &t.Priority,
			&t.ParentID, &t.TargetID, &t.AssigneeID, &t.AssigneeUserID,
			&t.ReviewerID, &t.VerifierID, &t.VerifiedFingerprint, &t.StallFingerprint,
			&t.CreatedByID, &t.CreatedByUserID, &t.OwnerID, &t.Thread, &t.Origin, &t.Stage,
			&t.TaskID, &t.Attempts, &t.Rounds, &t.Wakes, &t.Verdict, &t.Result, &t.BlockedReason, &t.BudgetUSD,
			&t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.DoneAt); err != nil {
			return nil, err
		}
		t.Kind, t.Status = protocol.TicketKind(kind), protocol.TicketStatus(status)
		t.BlockedBy = []string{}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTicket inserts a ticket and its blocker edges. The number is assigned
// by the database and written back.
func (s *Store) CreateTicket(ctx context.Context, t *protocol.Ticket) error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("%w: a ticket needs a title", ErrInvalid)
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.Kind == "" {
		t.Kind = protocol.TicketWork
	}
	if !t.Kind.Valid() {
		return fmt.Errorf("%w: unknown ticket kind %q", ErrInvalid, t.Kind)
	}
	if t.Status == "" {
		t.Status = protocol.TicketTodo
	}
	if !t.Status.Valid() {
		return fmt.Errorf("%w: unknown ticket status %q", ErrInvalid, t.Status)
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx,
		`INSERT INTO tickets(id,title,description,kind,status,priority,parent_id,target_id,assignee_id,
             assignee_user_id,reviewer_id,verifier_id,created_by_id,created_by_user_id,owner_id,thread,origin,
             stage,budget_usd,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
         RETURNING number`,
		t.ID, t.Title, t.Description, string(t.Kind), string(t.Status), t.Priority,
		nullIfEmpty(t.ParentID), nullIfEmpty(t.TargetID), nullIfEmpty(t.AssigneeID),
		nullIfEmpty(t.AssigneeUserID), nullIfEmpty(t.ReviewerID), nullIfEmpty(t.VerifierID),
		nullIfEmpty(t.CreatedByID), nullIfEmpty(t.CreatedByUserID), t.OwnerID, t.Thread, t.Origin,
		t.Stage, t.BudgetUSD, t.CreatedAt, t.UpdatedAt).Scan(&t.Number)
	if err != nil {
		return norm(err)
	}
	for _, b := range t.BlockedBy {
		if b == "" || b == t.ID {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO ticket_blockers(ticket_id,blocker_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			t.ID, b); err != nil {
			return norm(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return norm(err)
	}
	if t.BlockedBy == nil {
		t.BlockedBy = []string{}
	}
	return nil
}

// Ticket loads one ticket with its blockers and cost.
func (s *Store) Ticket(ctx context.Context, id string) (*protocol.Ticket, error) {
	rows, err := s.pool.Query(ctx, ticketSelect+` WHERE id=$1`, id)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	if err := s.attachTicketExtras(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// TicketByNumber resolves T-42.
func (s *Store) TicketByNumber(ctx context.Context, n int64) (*protocol.Ticket, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM tickets WHERE number=$1`, n).Scan(&id); err != nil {
		return nil, norm(err)
	}
	return s.Ticket(ctx, id)
}

// TicketByTask finds the ticket a run holds.
func (s *Store) TicketByTask(ctx context.Context, taskID string) (*protocol.Ticket, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM tickets WHERE task_id=$1`, taskID).Scan(&id); err != nil {
		// A run that no longer holds its ticket (it was released, or the run
		// continued in a new window) is still recorded against it.
		if err2 := s.pool.QueryRow(ctx, `SELECT COALESCE(ticket_id,'') FROM tasks WHERE id=$1`, taskID).Scan(&id); err2 != nil || id == "" {
			return nil, norm(err)
		}
	}
	return s.Ticket(ctx, id)
}

// ListTickets returns tickets matching the filter, newest first.
func (s *Store) ListTickets(ctx context.Context, f protocol.TicketFilter) ([]protocol.Ticket, error) {
	q := ticketSelect + ` WHERE TRUE`
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		q += fmt.Sprintf(" AND "+cond, len(args))
	}
	if len(f.Status) > 0 {
		st := make([]string, 0, len(f.Status))
		for _, s := range f.Status {
			st = append(st, string(s))
		}
		add("status = ANY($%d)", st)
	}
	if f.AssigneeID != "" {
		add("assignee_id = $%d", f.AssigneeID)
	}
	if f.ParentID != "" {
		add("parent_id = $%d", f.ParentID)
	}
	if f.RootsOnly {
		q += " AND parent_id IS NULL"
	}
	limit := f.Limit
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if err := s.attachTicketExtras(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// OpenTickets is every ticket that is not finished, oldest first: the set the
// scheduler and the liveness sweep walk.
func (s *Store) OpenTickets(ctx context.Context) ([]protocol.Ticket, error) {
	rows, err := s.pool.Query(ctx, ticketSelect+` WHERE status NOT IN ('done','cancelled') ORDER BY priority DESC, created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if err := s.attachTicketExtras(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// Children lists a ticket's direct children.
func (s *Store) Children(ctx context.Context, parentID string) ([]protocol.Ticket, error) {
	return s.ListTickets(ctx, protocol.TicketFilter{ParentID: parentID, Limit: 2000})
}

// Dependents lists the tickets waiting on this one.
func (s *Store) Dependents(ctx context.Context, blockerID string) ([]protocol.Ticket, error) {
	rows, err := s.pool.Query(ctx, ticketSelect+` WHERE id IN (SELECT ticket_id FROM ticket_blockers WHERE blocker_id=$1)`, blockerID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if err := s.attachTicketExtras(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// attachTicketExtras fills BlockedBy and CostUSD for a page of tickets in two
// queries rather than two per ticket.
func (s *Store) attachTicketExtras(ctx context.Context, list []protocol.Ticket) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]string, len(list))
	idx := map[string]int{}
	for i := range list {
		ids[i] = list[i].ID
		idx[list[i].ID] = i
	}
	rows, err := s.pool.Query(ctx, `SELECT ticket_id, blocker_id FROM ticket_blockers WHERE ticket_id = ANY($1) ORDER BY blocker_id`, ids)
	if err != nil {
		return norm(err)
	}
	for rows.Next() {
		var t, b string
		if err := rows.Scan(&t, &b); err != nil {
			rows.Close()
			return err
		}
		list[idx[t]].BlockedBy = append(list[idx[t]].BlockedBy, b)
	}
	rows.Close()
	rows, err = s.pool.Query(ctx,
		`SELECT k.ticket_id, COALESCE(SUM(tt.cost_usd),0)
           FROM tasks k JOIN token_telemetry tt ON tt.task_id = k.id
          WHERE k.ticket_id = ANY($1) GROUP BY k.ticket_id`, ids)
	if err != nil {
		return norm(err)
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		var c float64
		if err := rows.Scan(&t, &c); err != nil {
			return err
		}
		list[idx[t]].CostUSD = c
	}
	return rows.Err()
}

// UpdateTicket writes every mutable field back. The checkout lock is not one
// of them: task_id moves only through CheckoutTicket and ReleaseTicket.
func (s *Store) UpdateTicket(ctx context.Context, t *protocol.Ticket) error {
	if !t.Status.Valid() || !t.Kind.Valid() {
		return fmt.Errorf("%w: bad status or kind", ErrInvalid)
	}
	t.UpdatedAt = time.Now().UTC()
	if t.Status.Terminal() && t.DoneAt == nil {
		now := t.UpdatedAt
		t.DoneAt = &now
	}
	if !t.Status.Terminal() {
		t.DoneAt = nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE tickets SET title=$2,description=$3,kind=$4,status=$5,priority=$6,parent_id=$7,target_id=$8,
             assignee_id=$9,assignee_user_id=$10,reviewer_id=$11,verifier_id=$12,verified_fingerprint=$13,
             stall_fingerprint=$14,thread=$15,origin=$16,stage=$17,attempts=$18,rounds=$19,wakes=$20,
             verdict=$21,result=$22,blocked_reason=$23,budget_usd=$24,updated_at=$25,started_at=$26,done_at=$27
         WHERE id=$1`,
		t.ID, t.Title, t.Description, string(t.Kind), string(t.Status), t.Priority,
		nullIfEmpty(t.ParentID), nullIfEmpty(t.TargetID), nullIfEmpty(t.AssigneeID),
		nullIfEmpty(t.AssigneeUserID), nullIfEmpty(t.ReviewerID), nullIfEmpty(t.VerifierID),
		t.VerifiedFingerprint, t.StallFingerprint, t.Thread, t.Origin, t.Stage, t.Attempts, t.Rounds,
		t.Wakes, t.Verdict, t.Result, t.BlockedReason, t.BudgetUSD, t.UpdatedAt, t.StartedAt, t.DoneAt)
	return norm(err)
}

// CheckoutTicket claims a ready ticket for a run, atomically. It fails with
// ErrConflict when another run holds the ticket or it is no longer ready --
// which is what stops two dispatchers starting the same work twice.
func (s *Store) CheckoutTicket(ctx context.Context, ticketID, taskID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tickets SET task_id=$2, status='in_progress', updated_at=now(),
             started_at=COALESCE(started_at, now()), blocked_reason=''
          WHERE id=$1 AND task_id IS NULL AND status='todo'`,
		ticketID, taskID)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// ReleaseTicket clears the checkout if, and only if, taskID still holds it: a
// run that finishes after its ticket was handed to a retry must not clear the
// retry's lock.
func (s *Store) ReleaseTicket(ctx context.Context, ticketID, taskID string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tickets SET task_id=NULL, updated_at=now() WHERE id=$1 AND task_id=$2`, ticketID, taskID)
	if err != nil {
		return false, norm(err)
	}
	return tag.RowsAffected() > 0, nil
}

// MoveTicketLock hands the checkout from one run to the run that continues
// it (a marathon window, a retry) without the ticket ever looking free.
func (s *Store) MoveTicketLock(ctx context.Context, ticketID, fromTask, toTask string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tickets SET task_id=$3, updated_at=now() WHERE id=$1 AND task_id=$2`, ticketID, fromTask, toTask)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	_, err = s.pool.Exec(ctx, `UPDATE tasks SET ticket_id=$2 WHERE id=$1`, toTask, ticketID)
	return norm(err)
}

// SetBlockers replaces a ticket's blockers. A ticket cannot wait on itself or
// on anything that already waits on it.
func (s *Store) SetBlockers(ctx context.Context, ticketID string, blockers []string) error {
	clean := []string{}
	seen := map[string]bool{}
	for _, b := range blockers {
		b = strings.TrimSpace(b)
		if b == "" || seen[b] {
			continue
		}
		if b == ticketID {
			return fmt.Errorf("%w: a ticket cannot wait on itself", ErrInvalid)
		}
		seen[b] = true
		clean = append(clean, b)
	}
	for _, b := range clean {
		cyc, err := s.waitsOn(ctx, b, ticketID)
		if err != nil {
			return err
		}
		if cyc {
			return fmt.Errorf("%w: that would make the tickets wait on each other", ErrInvalid)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM ticket_blockers WHERE ticket_id=$1`, ticketID); err != nil {
		return norm(err)
	}
	for _, b := range clean {
		if _, err := tx.Exec(ctx, `INSERT INTO ticket_blockers(ticket_id,blocker_id) VALUES ($1,$2)`, ticketID, b); err != nil {
			return norm(err)
		}
	}
	_, _ = tx.Exec(ctx, `UPDATE tickets SET updated_at=now() WHERE id=$1`, ticketID)
	return norm(tx.Commit(ctx))
}

// AddBlocker adds one edge, refusing cycles.
func (s *Store) AddBlocker(ctx context.Context, ticketID, blockerID string) error {
	if ticketID == blockerID {
		return fmt.Errorf("%w: a ticket cannot wait on itself", ErrInvalid)
	}
	cyc, err := s.waitsOn(ctx, blockerID, ticketID)
	if err != nil {
		return err
	}
	if cyc {
		return fmt.Errorf("%w: that would make the tickets wait on each other", ErrInvalid)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO ticket_blockers(ticket_id,blocker_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, ticketID, blockerID)
	return norm(err)
}

// RemoveBlocker drops one edge.
func (s *Store) RemoveBlocker(ctx context.Context, ticketID, blockerID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ticket_blockers WHERE ticket_id=$1 AND blocker_id=$2`, ticketID, blockerID)
	return norm(err)
}

// waitsOn reports whether from waits, directly or through a chain, on to.
func (s *Store) waitsOn(ctx context.Context, from, to string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE chain(id) AS (
             SELECT blocker_id FROM ticket_blockers WHERE ticket_id=$1
             UNION SELECT b.blocker_id FROM ticket_blockers b JOIN chain c ON b.ticket_id = c.id)
         SELECT EXISTS(SELECT 1 FROM chain WHERE id=$2)`, from, to).Scan(&found)
	return found, norm(err)
}

// Ancestry returns the ticket's chain of parents, the root first.
func (s *Store) Ancestry(ctx context.Context, id string) ([]protocol.Ticket, error) {
	chain := []protocol.Ticket{}
	seen := map[string]bool{}
	cur := id
	for cur != "" && !seen[cur] && len(chain) < 32 {
		seen[cur] = true
		t, err := s.Ticket(ctx, cur)
		if err != nil {
			return nil, err
		}
		chain = append([]protocol.Ticket{*t}, chain...)
		cur = t.ParentID
	}
	return chain, nil
}

// Subtree returns the ticket and every descendant, excluding verify tickets
// and anything under them so a verifier cannot watch its own reviews.
func (s *Store) Subtree(ctx context.Context, rootID string) ([]protocol.Ticket, error) {
	rows, err := s.pool.Query(ctx,
		`WITH RECURSIVE tree(id) AS (
             SELECT id FROM tickets WHERE id=$1
             UNION SELECT t.id FROM tickets t JOIN tree ON t.parent_id = tree.id WHERE t.kind <> 'verify')
         `+ticketSelect+` WHERE id IN (SELECT id FROM tree) ORDER BY created_at`, rootID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if err := s.attachTicketExtras(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteTicket removes a ticket, its edges and its comments. Children are
// kept and lifted to the ticket's parent.
func (s *Store) DeleteTicket(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var parent *string
	if err := tx.QueryRow(ctx, `SELECT parent_id FROM tickets WHERE id=$1`, id).Scan(&parent); err != nil {
		return norm(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE tickets SET parent_id=$2 WHERE parent_id=$1`, id, parent); err != nil {
		return norm(err)
	}
	for _, q := range []string{
		`DELETE FROM ticket_blockers WHERE ticket_id=$1 OR blocker_id=$1`,
		`DELETE FROM ticket_comments WHERE ticket_id=$1`,
		`DELETE FROM tickets WHERE id=$1`,
	} {
		if _, err := tx.Exec(ctx, q, id); err != nil {
			return norm(err)
		}
	}
	return norm(tx.Commit(ctx))
}

// ------------------------------------------------------------- comments ---

func (s *Store) AddTicketComment(ctx context.Context, c *protocol.TicketComment) error {
	if strings.TrimSpace(c.Body) == "" {
		return fmt.Errorf("%w: an empty comment", ErrInvalid)
	}
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.Kind == "" {
		c.Kind = "comment"
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ticket_comments(id,ticket_id,author_id,author_user_id,author_name,kind,body,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		c.ID, c.TicketID, nullIfEmpty(c.AuthorID), nullIfEmpty(c.AuthorUserID), c.AuthorName, c.Kind, c.Body, c.CreatedAt)
	if err == nil {
		_, _ = s.pool.Exec(ctx, `UPDATE tickets SET updated_at=now() WHERE id=$1`, c.TicketID)
	}
	return norm(err)
}

func (s *Store) TicketComments(ctx context.Context, ticketID string, limit int) ([]protocol.TicketComment, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx,
		`SELECT * FROM (SELECT id,ticket_id,COALESCE(author_id,''),COALESCE(author_user_id,''),author_name,kind,body,created_at
           FROM ticket_comments WHERE ticket_id=$1 ORDER BY created_at DESC LIMIT $2) c ORDER BY created_at`,
		ticketID, limit)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.TicketComment{}
	for rows.Next() {
		var c protocol.TicketComment
		if err := rows.Scan(&c.ID, &c.TicketID, &c.AuthorID, &c.AuthorUserID, &c.AuthorName, &c.Kind, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TicketTasks lists the runs started for a ticket, newest first.
func (s *Store) TicketTasks(ctx context.Context, ticketID string) ([]protocol.Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE ticket_id=$1 ORDER BY created_at DESC LIMIT 100`, ticketID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// -------------------------------------------------------------- budgets ---

// MonthStart is the first instant of t's month in UTC: budgets reset there.
func MonthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// InstanceSpend is what an agent has cost since a moment, in dollars.
func (s *Store) InstanceSpend(ctx context.Context, instanceID string, since time.Time) (float64, error) {
	var c float64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(cost_usd),0) FROM token_telemetry WHERE instance_id=$1 AND created_at >= $2`,
		instanceID, since).Scan(&c)
	return c, norm(err)
}

// SpendByInstance is every agent's spend since a moment, in one query.
func (s *Store) SpendByInstance(ctx context.Context, since time.Time) (map[string]float64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT instance_id, COALESCE(SUM(cost_usd),0) FROM token_telemetry WHERE created_at >= $1 GROUP BY instance_id`, since)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var id string
		var c float64
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// TicketSpend is what a ticket's runs have cost.
func (s *Store) TicketSpend(ctx context.Context, ticketID string) (float64, error) {
	var c float64
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(tt.cost_usd),0) FROM token_telemetry tt JOIN tasks k ON k.id = tt.task_id WHERE k.ticket_id=$1`,
		ticketID).Scan(&c)
	return c, norm(err)
}

// ClaimBudgetNotice records that a budget notice was sent and reports whether
// this call was the first: a warning goes out once per agent, month and level.
func (s *Store) ClaimBudgetNotice(ctx context.Context, instanceID, month, level string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO budget_notices(instance_id,month,level,created_at) VALUES ($1,$2,$3,now()) ON CONFLICT DO NOTHING`,
		instanceID, month, level)
	if err != nil {
		return false, norm(err)
	}
	return tag.RowsAffected() > 0, nil
}

// ClearBudgetNotices forgets a month's notices for an agent, so raising a
// ceiling re-arms the warning at the new level.
func (s *Store) ClearBudgetNotices(ctx context.Context, instanceID, month string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM budget_notices WHERE instance_id=$1 AND month=$2`, instanceID, month)
	return norm(err)
}

// SetTaskParam sets one parameter on a task: an external run records the CLI
// session it can be resumed from.
func (s *Store) SetTaskParam(ctx context.Context, taskID, key, value string) error {
	t, err := s.Task(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Params == nil {
		t.Params = map[string]string{}
	}
	t.Params[key] = value
	raw, _ := json.Marshal(t.Params)
	_, err = s.pool.Exec(ctx, `UPDATE tasks SET params=$2 WHERE id=$1`, taskID, raw)
	return norm(err)
}
