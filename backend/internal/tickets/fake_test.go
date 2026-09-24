package tickets

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// fakeDB is an in-memory DB with the same semantics the SQL has where the
// engine depends on them: atomic checkout, compare-and-clear release,
// blockers, ancestry, spend.
type fakeDB struct {
	mu        sync.Mutex
	next      int64
	tickets   map[string]*protocol.Ticket
	blockers  map[string][]string
	comments  map[string][]protocol.TicketComment
	instances map[string]*protocol.Instance
	tasks     map[string]*protocol.Task
	spend     map[string]float64 // by instance
	tspend    map[string]float64 // by ticket
	notices   map[string]bool
	alerts    []protocol.Alert
	messages  []protocol.PeerMessage
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		tickets: map[string]*protocol.Ticket{}, blockers: map[string][]string{},
		comments: map[string][]protocol.TicketComment{}, instances: map[string]*protocol.Instance{},
		tasks: map[string]*protocol.Task{}, spend: map[string]float64{}, tspend: map[string]float64{},
		notices: map[string]bool{},
	}
}

func (f *fakeDB) copyTicket(t *protocol.Ticket) *protocol.Ticket {
	c := *t
	c.BlockedBy = append([]string{}, f.blockers[t.ID]...)
	sort.Strings(c.BlockedBy)
	c.CostUSD = f.tspend[t.ID]
	return &c
}

func (f *fakeDB) CreateTicket(_ context.Context, t *protocol.Ticket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.ID == "" {
		t.ID = store.NewID()
	}
	if t.Kind == "" {
		t.Kind = protocol.TicketWork
	}
	if t.Status == "" {
		t.Status = protocol.TicketTodo
	}
	f.next++
	t.Number = f.next
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now
	c := *t
	f.tickets[t.ID] = &c
	for _, b := range t.BlockedBy {
		f.blockers[t.ID] = append(f.blockers[t.ID], b)
	}
	return nil
}

func (f *fakeDB) Ticket(_ context.Context, id string) (*protocol.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return f.copyTicket(t), nil
}

func (f *fakeDB) TicketByNumber(_ context.Context, n int64) (*protocol.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tickets {
		if t.Number == n {
			return f.copyTicket(t), nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeDB) TicketByTask(_ context.Context, taskID string) (*protocol.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tickets {
		if t.TaskID == taskID {
			return f.copyTicket(t), nil
		}
	}
	if k, ok := f.tasks[taskID]; ok && k.TicketID != "" {
		if t, ok := f.tickets[k.TicketID]; ok {
			return f.copyTicket(t), nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeDB) sorted() []*protocol.Ticket {
	var list []*protocol.Ticket
	for _, t := range f.tickets {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Number < list[j].Number })
	return list
}

func (f *fakeDB) ListTickets(_ context.Context, fl protocol.TicketFilter) ([]protocol.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []protocol.Ticket{}
	for _, t := range f.sorted() {
		if len(fl.Status) > 0 {
			ok := false
			for _, s := range fl.Status {
				if s == t.Status {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		if fl.AssigneeID != "" && t.AssigneeID != fl.AssigneeID {
			continue
		}
		if fl.ParentID != "" && t.ParentID != fl.ParentID {
			continue
		}
		if fl.RootsOnly && t.ParentID != "" {
			continue
		}
		out = append(out, *f.copyTicket(t))
	}
	return out, nil
}

func (f *fakeDB) OpenTickets(ctx context.Context) ([]protocol.Ticket, error) {
	all, _ := f.ListTickets(ctx, protocol.TicketFilter{})
	out := []protocol.Ticket{}
	for _, t := range all {
		if !t.Status.Terminal() {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeDB) Children(ctx context.Context, parentID string) ([]protocol.Ticket, error) {
	return f.ListTickets(ctx, protocol.TicketFilter{ParentID: parentID})
}

func (f *fakeDB) Dependents(_ context.Context, blockerID string) ([]protocol.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []protocol.Ticket{}
	for _, t := range f.sorted() {
		for _, b := range f.blockers[t.ID] {
			if b == blockerID {
				out = append(out, *f.copyTicket(t))
			}
		}
	}
	return out, nil
}

func (f *fakeDB) UpdateTicket(_ context.Context, t *protocol.Ticket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.tickets[t.ID]
	if !ok {
		return store.ErrNotFound
	}
	lock := cur.TaskID
	c := *t
	c.TaskID = lock // the lock moves only through checkout/release
	c.UpdatedAt = time.Now().UTC()
	if c.Status.Terminal() && c.DoneAt == nil {
		now := c.UpdatedAt
		c.DoneAt = &now
	}
	if !c.Status.Terminal() {
		c.DoneAt = nil
	}
	f.tickets[t.ID] = &c
	return nil
}

func (f *fakeDB) CheckoutTicket(_ context.Context, ticketID, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[ticketID]
	if !ok || t.TaskID != "" || t.Status != protocol.TicketTodo {
		return store.ErrConflict
	}
	t.TaskID, t.Status, t.BlockedReason = taskID, protocol.TicketInProgress, ""
	return nil
}

func (f *fakeDB) ReleaseTicket(_ context.Context, ticketID, taskID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[ticketID]
	if !ok || t.TaskID != taskID {
		return false, nil
	}
	t.TaskID = ""
	return true, nil
}

func (f *fakeDB) MoveTicketLock(_ context.Context, ticketID, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[ticketID]
	if !ok || t.TaskID != from {
		return store.ErrConflict
	}
	t.TaskID = to
	if k, ok := f.tasks[to]; ok {
		k.TicketID = ticketID
	}
	return nil
}

func (f *fakeDB) SetBlockers(_ context.Context, id string, bs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockers[id] = append([]string{}, bs...)
	return nil
}

func (f *fakeDB) AddBlocker(_ context.Context, id, b string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, x := range f.blockers[id] {
		if x == b {
			return nil
		}
	}
	f.blockers[id] = append(f.blockers[id], b)
	return nil
}

func (f *fakeDB) Ancestry(ctx context.Context, id string) ([]protocol.Ticket, error) {
	var chain []protocol.Ticket
	cur := id
	for cur != "" {
		t, err := f.Ticket(ctx, cur)
		if err != nil {
			return nil, err
		}
		chain = append([]protocol.Ticket{*t}, chain...)
		cur = t.ParentID
	}
	return chain, nil
}

func (f *fakeDB) Subtree(ctx context.Context, root string) ([]protocol.Ticket, error) {
	t, err := f.Ticket(ctx, root)
	if err != nil {
		return nil, err
	}
	out := []protocol.Ticket{*t}
	kids, _ := f.Children(ctx, root)
	for _, k := range kids {
		if k.Kind == protocol.TicketVerify {
			continue
		}
		sub, _ := f.Subtree(ctx, k.ID)
		out = append(out, sub...)
	}
	return out, nil
}

func (f *fakeDB) AddTicketComment(_ context.Context, c *protocol.TicketComment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == "" {
		c.ID = store.NewID()
	}
	if c.Kind == "" {
		c.Kind = "comment"
	}
	c.CreatedAt = time.Now().UTC()
	f.comments[c.TicketID] = append(f.comments[c.TicketID], *c)
	return nil
}

func (f *fakeDB) TicketComments(_ context.Context, id string, limit int) ([]protocol.TicketComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.TicketComment{}, f.comments[id]...), nil
}

func (f *fakeDB) TicketSpend(_ context.Context, id string) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tspend[id], nil
}

func (f *fakeDB) InstanceSpend(_ context.Context, id string, _ time.Time) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spend[id], nil
}

func (f *fakeDB) ClaimBudgetNotice(_ context.Context, id, month, level string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := id + month + level
	if f.notices[k] {
		return false, nil
	}
	f.notices[k] = true
	return true, nil
}

func (f *fakeDB) ClearBudgetNotices(_ context.Context, id, month string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.notices {
		if len(k) >= len(id)+len(month) && k[:len(id)+len(month)] == id+month {
			delete(f.notices, k)
		}
	}
	return nil
}

func (f *fakeDB) SetInstanceHold(_ context.Context, id, hold string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.instances[id]; ok {
		in.Hold = hold
	}
	return nil
}

func (f *fakeDB) Instance(_ context.Context, id string) (*protocol.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	in, ok := f.instances[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := *in
	return &c, nil
}

func (f *fakeDB) ListInstances(_ context.Context) ([]protocol.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.Instance
	for _, in := range f.instances {
		out = append(out, *in)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeDB) CreateTask(_ context.Context, t *protocol.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *t
	f.tasks[t.ID] = &c
	return nil
}

func (f *fakeDB) Task(_ context.Context, id string) (*protocol.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.tasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	c := *k
	return &c, nil
}

func (f *fakeDB) ListTasks(_ context.Context, instanceID string, _ int) ([]protocol.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.Task
	for _, k := range f.tasks {
		if k.InstanceID == instanceID {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (f *fakeDB) UpdateTaskState(_ context.Context, id string, st protocol.TaskState, step int, errMsg, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k, ok := f.tasks[id]; ok {
		k.State, k.Step, k.Error, k.Result = st, step, errMsg, result
	}
	return nil
}

func (f *fakeDB) ListPeerMessages(_ context.Context, instanceID string, _ int) ([]protocol.PeerMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.PeerMessage{}, f.messages...), nil
}

func (f *fakeDB) CreateAlert(_ context.Context, a *protocol.Alert) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a.ID = store.NewID()
	f.alerts = append(f.alerts, *a)
	return nil
}

// fakeRunner records starts and lets tests finish runs.
type fakeRunner struct {
	mu       sync.Mutex
	db       *fakeDB
	running  map[string]bool
	started  []string
	notReady map[string]string
}

func newFakeRunner(db *fakeDB) *fakeRunner {
	return &fakeRunner{db: db, running: map[string]bool{}, notReady: map[string]string{}}
}

func (r *fakeRunner) Start(_ context.Context, t *protocol.Task) error {
	r.mu.Lock()
	r.running[t.ID] = true
	r.started = append(r.started, t.ID)
	r.mu.Unlock()
	return r.db.UpdateTaskState(context.Background(), t.ID, protocol.TaskRunning, 0, "", "")
}

func (r *fakeRunner) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running[id] {
		return false
	}
	delete(r.running, id)
	_ = r.db.UpdateTaskState(context.Background(), id, protocol.TaskCancelled, 0, "cancelled", "")
	return true
}

func (r *fakeRunner) IsRunning(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[id]
}

func (r *fakeRunner) Ready(_ context.Context, in *protocol.Instance) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if why, ok := r.notReady[in.ID]; ok {
		return false, why
	}
	return true, ""
}

func (r *fakeRunner) end(id string) {
	r.mu.Lock()
	delete(r.running, id)
	r.mu.Unlock()
}
