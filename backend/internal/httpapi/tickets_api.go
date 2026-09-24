package httpapi

import (
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/tickets"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The ticket API. Tickets are company-wide work, visible to everyone who can
// sign in; who may change them is the operator role, as for tasks.

// ticketView is a ticket as the clients see it: with its short reference and
// the names of the agents it refers to, so a board renders without a lookup
// per card.
type ticketView struct {
	protocol.Ticket
	Ref          string `json:"ref"`
	AssigneeName string `json:"assignee_name,omitempty"`
	ReviewerName string `json:"reviewer_name,omitempty"`
	VerifierName string `json:"verifier_name,omitempty"`
}

func (s *Server) viewTickets(r *http.Request, list []protocol.Ticket) []ticketView {
	names := map[string]string{}
	if insts, err := s.db.ListInstances(r.Context()); err == nil {
		for _, in := range insts {
			names[in.ID] = in.Name
		}
	}
	out := make([]ticketView, 0, len(list))
	for _, t := range list {
		out = append(out, ticketView{Ticket: t, Ref: t.Ref(),
			AssigneeName: names[t.AssigneeID], ReviewerName: names[t.ReviewerID], VerifierName: names[t.VerifierID]})
	}
	return out
}

func (s *Server) viewTicket(r *http.Request, t *protocol.Ticket) ticketView {
	return s.viewTickets(r, []protocol.Ticket{*t})[0]
}

// resolveTicket accepts a ticket's id or its reference (T-12).
func (s *Server) resolveTicket(r *http.Request, ref string) (*protocol.Ticket, error) {
	ref = strings.TrimSpace(ref)
	// A uuid is 36 characters with hyphens; a reference is T-12, #12 or 12.
	if len(ref) < 20 {
		if n, ok := protocol.ParseTicketRef(ref); ok {
			return s.db.TicketByNumber(r.Context(), n)
		}
	}
	return s.db.Ticket(r.Context(), ref)
}

func actorFrom(r *http.Request) tickets.Actor {
	u := userFrom(r.Context())
	if u == nil {
		return tickets.Actor{Name: "operator"}
	}
	name := u.Email
	if i := strings.IndexByte(name, '@'); i > 0 {
		name = name[:i]
	}
	return tickets.Actor{UserID: u.Subject, Name: name}
}

func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := protocol.TicketFilter{
		AssigneeID: q.Get("assignee"),
		ParentID:   q.Get("parent"),
		RootsOnly:  q.Get("roots") == "1",
		Limit:      queryInt(r, "limit", 500),
	}
	for _, st := range strings.Split(q.Get("status"), ",") {
		if st = strings.TrimSpace(st); st != "" {
			f.Status = append(f.Status, protocol.TicketStatus(st))
		}
	}
	list, err := s.db.ListTickets(r.Context(), f)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.viewTickets(r, list))
}

type createTicketRequest struct {
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Kind        protocol.TicketKind   `json:"kind"`
	Status      protocol.TicketStatus `json:"status"`
	Priority    int                   `json:"priority"`
	AssigneeID  string                `json:"assignee_id"`
	ParentID    string                `json:"parent_id"`
	BlockedBy   []string              `json:"blocked_by"`
	ReviewerID  string                `json:"reviewer_id"`
	VerifierID  string                `json:"verifier_id"`
	BudgetUSD   float64               `json:"budget_usd"`
	Thread      string                `json:"thread"`
}

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	var req createTicketRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		fail(w, http.StatusBadRequest, "a ticket needs a title")
		return
	}
	blockers, err := s.resolveTicketIDs(r, req.BlockedBy)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	parentID := req.ParentID
	if parentID != "" {
		p, err := s.resolveTicket(r, parentID)
		if err != nil {
			fail(w, http.StatusBadRequest, "no parent ticket "+parentID)
			return
		}
		parentID = p.ID
	}
	t := &protocol.Ticket{
		Title: strings.TrimSpace(req.Title), Description: req.Description, Kind: req.Kind, Status: req.Status,
		Priority: req.Priority, AssigneeID: req.AssigneeID, ParentID: parentID, BlockedBy: blockers,
		ReviewerID: req.ReviewerID, VerifierID: req.VerifierID, BudgetUSD: req.BudgetUSD,
		Thread: req.Thread, Origin: strings.TrimSpace(req.Title),
		OwnerID: userFrom(r.Context()).Subject,
	}
	if parentID != "" {
		if p, err := s.db.Ticket(r.Context(), parentID); err == nil {
			t.Origin, t.Thread = p.Origin, firstNonEmptyStr(t.Thread, p.Thread)
		}
	}
	if err := s.tickets.Create(r.Context(), t, actorFrom(r)); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.viewTicket(r, t))
}

func (s *Server) resolveTicketIDs(r *http.Request, refs []string) ([]string, error) {
	var out []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		t, err := s.resolveTicket(r, ref)
		if err != nil {
			return nil, &httpError{"no ticket " + ref}
		}
		out = append(out, t.ID)
	}
	return out, nil
}

type httpError struct{ msg string }

func (e *httpError) Error() string { return e.msg }

// ticketDetail is everything a ticket's page shows.
type ticketDetail struct {
	Ticket     ticketView               `json:"ticket"`
	Ancestry   []ticketView             `json:"ancestry"`
	Children   []ticketView             `json:"children"`
	Blockers   []ticketView             `json:"blockers"`
	Dependents []ticketView             `json:"dependents"`
	Comments   []protocol.TicketComment `json:"comments"`
	Runs       []protocol.Task          `json:"runs"`
}

func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.resolveTicket(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	ctx := r.Context()
	d := ticketDetail{Ticket: s.viewTicket(r, t)}
	if chain, err := s.db.Ancestry(ctx, t.ID); err == nil && len(chain) > 1 {
		d.Ancestry = s.viewTickets(r, chain[:len(chain)-1])
	}
	if kids, err := s.db.Children(ctx, t.ID); err == nil {
		d.Children = s.viewTickets(r, kids)
	}
	var blockers []protocol.Ticket
	for _, id := range t.BlockedBy {
		if b, err := s.db.Ticket(ctx, id); err == nil {
			blockers = append(blockers, *b)
		}
	}
	d.Blockers = s.viewTickets(r, blockers)
	if deps, err := s.db.Dependents(ctx, t.ID); err == nil {
		d.Dependents = s.viewTickets(r, deps)
	}
	d.Comments, _ = s.db.TicketComments(ctx, t.ID, 300)
	if runs, err := s.db.TicketTasks(ctx, t.ID); err == nil {
		for i := range runs {
			runs[i].Goal = "" // long, and on the run's own page
		}
		d.Runs = runs
	}
	if d.Ancestry == nil {
		d.Ancestry = []ticketView{}
	}
	writeJSON(w, http.StatusOK, d)
}

type patchTicketRequest struct {
	Title       *string                `json:"title"`
	Description *string                `json:"description"`
	Status      *protocol.TicketStatus `json:"status"`
	Priority    *int                   `json:"priority"`
	AssigneeID  *string                `json:"assignee_id"`
	ReviewerID  *string                `json:"reviewer_id"`
	VerifierID  *string                `json:"verifier_id"`
	ParentID    *string                `json:"parent_id"`
	BudgetUSD   *float64               `json:"budget_usd"`
	BlockedBy   *[]string              `json:"blocked_by"`
}

func (s *Server) handlePatchTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.resolveTicket(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var req patchTicketRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p := tickets.Patch{Title: req.Title, Description: req.Description, Status: req.Status, Priority: req.Priority,
		AssigneeID: req.AssigneeID, ReviewerID: req.ReviewerID, VerifierID: req.VerifierID, BudgetUSD: req.BudgetUSD}
	if req.ParentID != nil {
		pid := *req.ParentID
		if pid != "" {
			parent, err := s.resolveTicket(r, pid)
			if err != nil {
				fail(w, http.StatusBadRequest, "no parent ticket "+pid)
				return
			}
			pid = parent.ID
		}
		p.ParentID = &pid
	}
	if req.BlockedBy != nil {
		ids, err := s.resolveTicketIDs(r, *req.BlockedBy)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		p.BlockedBy = &ids
	}
	out, err := s.tickets.Update(r.Context(), t.ID, p, actorFrom(r))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.viewTicket(r, out))
}

func (s *Server) handleDeleteTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.resolveTicket(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if err := s.tickets.Delete(r.Context(), t.ID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCommentTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.resolveTicket(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		fail(w, http.StatusBadRequest, "a comment needs a body")
		return
	}
	c, err := s.tickets.Comment(r.Context(), t.ID, strings.TrimSpace(req.Body), actorFrom(r))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleReopenTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.resolveTicket(r, r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.tickets.Reopen(r.Context(), t.ID, req.Reason, actorFrom(r))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.viewTicket(r, out))
}
