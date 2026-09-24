package httpapi

import (
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/external"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/tickets"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The run callback API: how an external agent talks back about the one run it
// was given. Authenticated by the run token that came with the run, not by a
// user: the agent can report, finish, comment and hand work on for this run,
// and do nothing else.

// runFrom checks the run token against the task in the path.
func (s *Server) runFrom(w http.ResponseWriter, r *http.Request) (string, bool) {
	taskID := r.PathValue("taskId")
	got, err := external.VerifyRunToken(s.cfg.JWTSecret, r.Header.Get("Authorization"))
	if err != nil || got != taskID {
		fail(w, http.StatusUnauthorized, "a valid run token for this run is required")
		return "", false
	}
	return taskID, true
}

type runContext struct {
	TaskID     string         `json:"task_id"`
	Agent      string         `json:"agent"`
	Goal       string         `json:"goal"`
	Ticket     *ticketView    `json:"ticket,omitempty"`
	Colleagues []runColleague `json:"colleagues"`
}

type runColleague struct {
	Name         string             `json:"name"`
	Title        string             `json:"title,omitempty"`
	Kind         protocol.AgentKind `json:"kind"`
	Capabilities string             `json:"capabilities,omitempty"`
	Relation     string             `json:"relation,omitempty"`
}

func (s *Server) handleRunContext(w http.ResponseWriter, r *http.Request) {
	taskID, ok := s.runFrom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	task, err := s.db.Task(ctx, taskID)
	if err != nil {
		failErr(w, err)
		return
	}
	inst, err := s.db.Instance(ctx, task.InstanceID)
	if err != nil {
		failErr(w, err)
		return
	}
	out := runContext{TaskID: task.ID, Agent: inst.Name, Goal: task.Goal, Colleagues: []runColleague{}}
	if task.TicketID != "" {
		if t, err := s.db.Ticket(ctx, task.TicketID); err == nil {
			v := s.viewTicket(r, t)
			out.Ticket = &v
		}
	}
	if all, err := s.db.ListInstances(ctx); err == nil {
		for _, in := range all {
			if in.ID == inst.ID {
				continue
			}
			rel := ""
			switch {
			case inst.ReportsTo == in.ID:
				rel = "your manager"
			case in.ReportsTo == inst.ID:
				rel = "reports to you"
			}
			out.Colleagues = append(out.Colleagues, runColleague{Name: in.Name, Title: in.Title, Kind: in.AgentKindOf(), Capabilities: in.Capabilities, Relation: rel})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRunProgress(w http.ResponseWriter, r *http.Request) {
	taskID, ok := s.runFrom(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.external.Progress(r.Context(), taskID, req.Text) {
		fail(w, http.StatusGone, "that run is not live")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunComplete(w http.ResponseWriter, r *http.Request) {
	taskID, ok := s.runFrom(w, r)
	if !ok {
		return
	}
	var req external.Completion
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.external.Complete(taskID, req) {
		fail(w, http.StatusGone, "that run is not live, or has already finished")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunTicket(w http.ResponseWriter, r *http.Request) {
	taskID, ok := s.runFrom(w, r)
	if !ok {
		return
	}
	inst, live := s.external.RunInstance(taskID)
	if !live {
		fail(w, http.StatusGone, "that run is not live")
		return
	}
	var req struct {
		Target string `json:"target"`
		Title  string `json:"title"`
		Text   string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	msg, err := s.tickets.CreateFromAgent(r.Context(), taskID, inst, req.Target, req.Title, req.Text, true)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"result": msg})
}

func (s *Server) handleRunComment(w http.ResponseWriter, r *http.Request) {
	taskID, ok := s.runFrom(w, r)
	if !ok {
		return
	}
	inst, live := s.external.RunInstance(taskID)
	if !live {
		fail(w, http.StatusGone, "that run is not live")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		fail(w, http.StatusBadRequest, "a comment needs a body")
		return
	}
	t, err := s.db.TicketByTask(r.Context(), taskID)
	if err != nil {
		fail(w, http.StatusNotFound, "this run has no ticket")
		return
	}
	body := strings.TrimSpace(req.Body)
	if inst.Trust == protocol.TrustLow {
		body = tickets.FenceUntrusted(inst.Name, body)
	}
	c, err := s.tickets.Comment(r.Context(), t.ID, body, tickets.Actor{InstanceID: inst.ID, Name: inst.Name})
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// handleDeviceJobProgress takes what a PC reports about an agent run and
// answers whether to stop it.
func (s *Server) handleDeviceJobProgress(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownOafDevice(w, r)
	if !ok {
		return
	}
	job, err := s.db.DeviceJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		failErr(w, err)
		return
	}
	if job.DeviceID != d.ID {
		fail(w, http.StatusForbidden, "that job is not this device's")
		return
	}
	_ = s.db.TouchOafDevice(r.Context(), d.ID)
	var req struct {
		Events []external.ProgressEvent `json:"events"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	cancel, _ := s.external.DeviceProgress(r.Context(), job.ID, req.Events)
	writeJSON(w, http.StatusOK, map[string]bool{"cancel": cancel})
}
