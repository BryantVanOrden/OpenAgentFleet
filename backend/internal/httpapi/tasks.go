package httpapi

import (
	"net/http"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

type createTaskRequest struct {
	InstanceID   string            `json:"instance_id"`
	Goal         string            `json:"goal"`
	SkillID      string            `json:"skill_id,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	ParentTaskID string            `json:"parent_task_id,omitempty"`
	AutoRefine   bool              `json:"auto_refine,omitempty"`
	ProviderID   string            `json:"provider_id,omitempty"`
	MaxSteps     int               `json:"max_steps,omitempty"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Goal == "" {
		fail(w, http.StatusBadRequest, "goal is required")
		return
	}
	inst, err := s.db.Instance(r.Context(), req.InstanceID)
	if err != nil {
		failErr(w, err)
		return
	}
	if inst.State != protocol.InstanceRunning {
		fail(w, http.StatusConflict, "instance is "+string(inst.State)+"; start it first")
		return
	}
	// One driver at a time (unless it is a child sub-agent of the active task)
	if existing, err := s.db.ListTasks(r.Context(), inst.ID, 20); err == nil {
		for _, t := range existing {
			if (t.State == protocol.TaskRunning || t.State == protocol.TaskAwaitingHuman) && req.ParentTaskID == "" {
				fail(w, http.StatusConflict, "task "+t.ID+" is already running on this instance")
				return
			}
		}
	}

	maxSteps := req.MaxSteps
	if maxSteps <= 0 || maxSteps > 500 {
		maxSteps = s.cfg.MaxSteps
	}
	task := &protocol.Task{
		ID:           store.NewID(),
		InstanceID:   inst.ID,
		OwnerID:      userFrom(r.Context()).Subject,
		Goal:         req.Goal,
		SkillID:      req.SkillID,
		Params:       req.Params,
		ParentTaskID: req.ParentTaskID,
		AutoRefine:   req.AutoRefine,
		State:        protocol.TaskQueued,
		MaxSteps:     maxSteps,
		ProviderID:   req.ProviderID,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.db.CreateTask(r.Context(), task); err != nil {
		failErr(w, err)
		return
	}
	if err := s.runner.Start(r.Context(), task); err != nil {
		_ = s.db.UpdateTaskState(r.Context(), task.ID, protocol.TaskFailed, 0, err.Error(), "")
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListTasks(r.Context(), r.URL.Query().Get("instance_id"), queryInt(r, "limit", 50))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.db.Task(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task": t, "live": s.runner.IsRunning(t.ID),
	})
}

func (s *Server) handleTaskSteps(w http.ResponseWriter, r *http.Request) {
	steps, err := s.db.ListSteps(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, steps)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.runner.Cancel(id) {
		// Not in flight here — mark it cancelled anyway so a task orphaned by a
		// restart does not sit in "running" forever.
		t, err := s.db.Task(r.Context(), id)
		if err != nil {
			failErr(w, err)
			return
		}
		if t.State == protocol.TaskRunning || t.State == protocol.TaskQueued || t.State == protocol.TaskAwaitingHuman {
			_ = s.db.UpdateTaskState(r.Context(), id, protocol.TaskCancelled, t.Step, "cancelled by operator", "")
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// handleSynthesizeSkill extracts a reusable SKILL.md from any completed task run.
func (s *Server) handleSynthesizeSkill(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	t, err := s.db.Task(r.Context(), taskID)
	if err != nil {
		failErr(w, err)
		return
	}
	steps, err := s.db.ListSteps(r.Context(), taskID)
	if err != nil {
		failErr(w, err)
		return
	}
	if len(steps) == 0 {
		fail(w, http.StatusBadRequest, "task has no recorded steps to synthesize from")
		return
	}

	skill, err := s.runner.Refiner().SynthesizeSkill(r.Context(), t, steps)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, skill)
}
