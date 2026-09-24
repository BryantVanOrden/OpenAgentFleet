package store

import (
	"errors"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func TestTicketLifecycleInTheDatabase(t *testing.T) {
	s, ctx := testStore(t)

	root := &protocol.Ticket{Title: "Ship Fleet Tasks", Kind: protocol.TicketWork, Status: protocol.TicketTodo, Origin: "operator request"}
	if err := s.CreateTicket(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteTicket(ctx, root.ID) })
	if root.Number <= 0 {
		t.Fatalf("the database should number tickets, got %d", root.Number)
	}
	build := &protocol.Ticket{Title: "Build it", ParentID: root.ID, AssigneeID: "inst-builder", Stage: "build"}
	if err := s.CreateTicket(ctx, build); err != nil {
		t.Fatalf("create build: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteTicket(ctx, build.ID) })
	test := &protocol.Ticket{Title: "Test it", ParentID: root.ID, AssigneeID: "inst-checker", BlockedBy: []string{build.ID}}
	if err := s.CreateTicket(ctx, test); err != nil {
		t.Fatalf("create test: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteTicket(ctx, test.ID) })

	got, err := s.Ticket(ctx, test.ID)
	if err != nil || len(got.BlockedBy) != 1 || got.BlockedBy[0] != build.ID {
		t.Fatalf("blocker did not round-trip: %+v %v", got, err)
	}
	if byNum, err := s.TicketByNumber(ctx, test.Number); err != nil || byNum.ID != test.ID {
		t.Fatalf("lookup by number: %v %v", byNum, err)
	}

	// A cycle is refused, in both the direct and the indirect form.
	if err := s.AddBlocker(ctx, build.ID, test.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a two-ticket cycle should be refused, got %v", err)
	}
	if err := s.AddBlocker(ctx, build.ID, build.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a self-block should be refused, got %v", err)
	}

	// Checkout is atomic: the second claim loses.
	if err := s.CheckoutTicket(ctx, build.ID, "task-a"); err != nil {
		t.Fatalf("first checkout: %v", err)
	}
	if err := s.CheckoutTicket(ctx, build.ID, "task-b"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second checkout should conflict, got %v", err)
	}
	held, _ := s.Ticket(ctx, build.ID)
	if held.Status != protocol.TicketInProgress || held.TaskID != "task-a" || held.StartedAt == nil {
		t.Fatalf("checkout should move to in_progress under task-a: %+v", held)
	}
	// Only the holder releases; a stale run cannot clear its successor's lock.
	if ok, _ := s.ReleaseTicket(ctx, build.ID, "task-b"); ok {
		t.Fatal("a run that does not hold the ticket must not release it")
	}
	if err := s.MoveTicketLock(ctx, build.ID, "task-a", "task-a2"); err != nil {
		t.Fatalf("move lock: %v", err)
	}
	if ok, _ := s.ReleaseTicket(ctx, build.ID, "task-a2"); !ok {
		t.Fatal("the holder should release")
	}

	// Terminal statuses stamp done_at; reopening clears it.
	held, _ = s.Ticket(ctx, build.ID)
	held.Status = protocol.TicketDone
	held.Result = "built"
	if err := s.UpdateTicket(ctx, held); err != nil {
		t.Fatalf("update: %v", err)
	}
	done, _ := s.Ticket(ctx, build.ID)
	if done.DoneAt == nil || done.Result != "built" {
		t.Fatalf("done should stamp done_at: %+v", done)
	}
	deps, err := s.Dependents(ctx, build.ID)
	if err != nil || len(deps) != 1 || deps[0].ID != test.ID {
		t.Fatalf("dependents: %v %v", deps, err)
	}
	kids, _ := s.Children(ctx, root.ID)
	if len(kids) != 2 {
		t.Fatalf("children: %d", len(kids))
	}
	chain, err := s.Ancestry(ctx, test.ID)
	if err != nil || len(chain) != 2 || chain[0].ID != root.ID {
		t.Fatalf("ancestry should run root first: %v %v", chain, err)
	}
	tree, _ := s.Subtree(ctx, root.ID)
	if len(tree) != 3 {
		t.Fatalf("subtree: %d", len(tree))
	}

	// Comments.
	if err := s.AddTicketComment(ctx, &protocol.TicketComment{TicketID: test.ID, AuthorName: "Checker", Body: "3 findings"}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	cs, _ := s.TicketComments(ctx, test.ID, 10)
	if len(cs) != 1 || cs[0].Body != "3 findings" || cs[0].Kind != "comment" {
		t.Fatalf("comments: %+v", cs)
	}

	// Deleting a parent lifts its children rather than orphaning them.
	mid := &protocol.Ticket{Title: "Middle", ParentID: root.ID}
	_ = s.CreateTicket(ctx, mid)
	leaf := &protocol.Ticket{Title: "Leaf", ParentID: mid.ID}
	_ = s.CreateTicket(ctx, leaf)
	t.Cleanup(func() { _ = s.DeleteTicket(ctx, leaf.ID) })
	if err := s.DeleteTicket(ctx, mid.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	lifted, _ := s.Ticket(ctx, leaf.ID)
	if lifted.ParentID != root.ID {
		t.Fatalf("the child should move up to the deleted ticket's parent, got %q", lifted.ParentID)
	}
}

func TestOrgFieldsAndBudgetNotices(t *testing.T) {
	s, ctx := testStore(t)
	now := time.Now().UTC()
	mk := func(id, name string) *protocol.Instance {
		in := &protocol.Instance{ID: id, Name: name, OwnerID: "u", Tier: "micro", State: protocol.InstanceRunning,
			CreatedAt: now, UpdatedAt: now}
		if err := s.CreateInstance(ctx, in); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		t.Cleanup(func() { _ = s.DeleteInstance(ctx, id) })
		return in
	}
	lead := mk("org-test-lead", "Lead")
	dev := mk("org-test-dev", "Dev")
	dev.Kind = protocol.KindClaudeCode
	dev.Title = "Backend engineer"
	dev.Capabilities = "Go services and SQL"
	dev.Connection = protocol.AgentConnection{DeviceID: "dev-pc", Cwd: "C:/work", Autonomy: "full"}
	dev.BudgetMonthUSD = 25
	if err := s.UpdateInstance(ctx, dev); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := s.SetReportsTo(ctx, dev.ID, lead.ID); err != nil {
		t.Fatalf("reports_to: %v", err)
	}
	if err := s.SetReportsTo(ctx, lead.ID, dev.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a reporting loop should be refused, got %v", err)
	}
	got, err := s.Instance(ctx, dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != protocol.KindClaudeCode || got.ReportsTo != lead.ID || got.Title != "Backend engineer" ||
		got.Connection.Cwd != "C:/work" || got.BudgetMonthUSD != 25 || got.BudgetWarnPct != 80 || got.Trust != protocol.TrustStandard {
		t.Fatalf("org fields did not round-trip: %+v", got)
	}
	old, _ := s.Instance(ctx, lead.ID)
	if old.Kind != protocol.KindDesktop {
		t.Fatalf("an agent created without a kind is a desktop, got %q", old.Kind)
	}

	month := MonthStart(now).Format("2006-01")
	first, err := s.ClaimBudgetNotice(ctx, dev.ID, month, "warn")
	if err != nil || !first {
		t.Fatalf("first notice: %v %v", first, err)
	}
	again, _ := s.ClaimBudgetNotice(ctx, dev.ID, month, "warn")
	if again {
		t.Fatal("a notice goes out once per month and level")
	}
	_ = s.ClearBudgetNotices(ctx, dev.ID, month)
	if re, _ := s.ClaimBudgetNotice(ctx, dev.ID, month, "warn"); !re {
		t.Fatal("clearing should re-arm the notice")
	}
	_ = s.ClearBudgetNotices(ctx, dev.ID, month)
}
