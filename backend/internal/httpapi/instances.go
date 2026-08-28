package httpapi

import (
	"net/http"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/agent"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

func (s *Server) handleTiers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.fleet.Tiers())
}

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListInstances(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redactAll(list))
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := s.db.Instance(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redact(*inst))
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req fleet.CreateRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	req.OwnerID = userFrom(r.Context()).Subject

	inst, err := s.fleet.Create(r.Context(), req)
	if err != nil {
		// A half-provisioned instance is still returned so the operator can see
		// the error against it in the fleet view rather than losing the record.
		if inst != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": err.Error(), "instance": redact(*inst),
			})
			return
		}
		failErr(w, err)
		return
	}
	s.bus.Emit("instance.state", inst.ID, "", redact(*inst))
	writeJSON(w, http.StatusCreated, redact(*inst))
}

func (s *Server) handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]

	var err error
	switch action {
	case "start":
		err = s.fleet.Start(r.Context(), id)
	case "stop":
		err = s.fleet.Stop(r.Context(), id)
	case "pause":
		err = s.fleet.Pause(r.Context(), id)
	case "resume":
		err = s.fleet.Resume(r.Context(), id)
	default:
		fail(w, http.StatusNotFound, "unknown action")
		return
	}
	if err != nil {
		failErr(w, err)
		return
	}
	inst, err := s.db.Instance(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("instance.state", id, "", redact(*inst))
	writeJSON(w, http.StatusOK, redact(*inst))
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Cancel anything still driving the sandbox before pulling it out.
	if tasks, err := s.db.ListTasks(r.Context(), id, 50); err == nil {
		for _, t := range tasks {
			if t.State == protocol.TaskRunning || t.State == protocol.TaskAwaitingHuman {
				s.runner.Cancel(t.ID)
			}
		}
	}
	if err := s.fleet.Delete(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("instance.deleted", id, "", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInstanceStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.fleet.Stats(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleObserve returns a single frame plus the accessibility tree. The mobile
// app uses it as a cheap poll when the operator does not want a live stream on
// a metered connection.
func (s *Server) handleObserve(w http.ResponseWriter, r *http.Request) {
	inst, err := s.db.Instance(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	if inst.State != protocol.InstanceRunning {
		fail(w, http.StatusConflict, "instance is "+string(inst.State))
		return
	}
	obs, err := agent.NewSandboxClient(inst.AgentdURL).Observe(r.Context(), agent.ObserveOptions{
		Screenshot: true,
		A11y:       queryBool(r, "a11y"),
		MaxWidth:   queryInt(r, "max_width", s.cfg.ScreenshotMaxW),
		Quality:    queryInt(r, "quality", 70),
	})
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obs)
}

// handleManualAct is the human takeover path: the operator drives the desktop
// through the same action vocabulary the agent uses, which means the audit trail
// stays uniform whoever was at the controls.
func (s *Server) handleManualAct(w http.ResponseWriter, r *http.Request) {
	inst, err := s.db.Instance(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var a protocol.Action
	if err := readJSON(r, &a); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.Action == protocol.ActShell && !inst.ShellAccess {
		fail(w, http.StatusForbidden, "shell is disabled for this instance")
		return
	}
	res, err := agent.NewSandboxClient(inst.AgentdURL).Act(r.Context(), a)
	if err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("manual.act", inst.ID, "", map[string]any{
		"action": a, "by": userFrom(r.Context()).Email,
	})
	writeJSON(w, http.StatusOK, res)
}

// redact strips the internal sandbox addresses before an instance leaves the
// orchestrator: clients reach the desktop through the authenticated proxy, and
// handing them a container IP only invites someone to try it directly.
func redact(in protocol.Instance) protocol.Instance {
	in.AgentdURL = ""
	in.VNCURL = "/vnc/" + in.ID + "/"
	return in
}

func redactAll(list []protocol.Instance) []protocol.Instance {
	out := make([]protocol.Instance, len(list))
	for i, in := range list {
		out[i] = redact(in)
	}
	return out
}
