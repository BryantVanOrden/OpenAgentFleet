package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/agent"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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
	// Filtered, not merely gated: someone who may see two of twenty bots gets
	// two. Returning all of them and hiding the rest in the client would put
	// every org's bot names on the wire.
	writeJSON(w, http.StatusOK, redactAll(visibleInstances(accessFrom(r.Context()), list)))
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermView)
	if !ok {
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
	// Creating into an org you are not in would put a machine somewhere you
	// cannot then see it — and would let anyone seed bots into any department.
	if !accessFrom(r.Context()).CanInOrg(protocol.PermCreate, req.OrgID) {
		fail(w, http.StatusForbidden, "you cannot create bots in that organisation")
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
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit); !ok {
		return
	}
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
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermDelete); !ok {
		return
	}
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
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermView); !ok {
		return
	}
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
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermRead); !ok {
		return
	}
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
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermDesktop); !ok {
		return
	}
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
	// Gate every code-execution action, not just shell. python runs against a
	// live interpreter and mount_tool/call_tool define then invoke a Python
	// function body — all three are code execution by another name, so gating
	// only shell here let an operator on a shell-disabled instance run arbitrary
	// code through python. This mirrors the runner's gate (see runner.go) so the
	// manual-takeover path and the agent path enforce the same boundary.
	switch a.Action {
	case protocol.ActShell, protocol.ActPython, protocol.ActMountTool, protocol.ActCallTool:
		if !inst.ShellAccess {
			fail(w, http.StatusForbidden, "code execution ("+string(a.Action)+") is disabled for this instance")
			return
		}
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

// handleSetInstanceAccess toggles what an agent is allowed to do.
//
// Only shell_access is settable here, and deliberately so. It is checked by the
// orchestrator on every shell action, so flipping it takes effect on the next
// step of a running agent -- which is exactly what you want when a run starts
// doing something you would rather it could not.
//
// sudo_access is settable too, and takes effect immediately: it is enforced by
// the setuid bit on the sandbox's sudo binary, which can be changed on a
// running container. It used to depend on no-new-privileges, which the kernel
// applies at creation — meaning revoking sudo required recreating the
// container and, with no volume on these sandboxes, discarding the agent's
// work. See fleet.securityOpts for what that trade costs.
// handleSetInstanceModels assigns a bot its own ordered model chain.
//
// Replaces the list rather than merging: the order IS the setting, and a merge
// would make "move this model to the top" impossible to express.
func (s *Server) handleSetInstanceModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit); !ok {
		return
	}
	id := r.PathValue("id")
	inst, err := s.db.Instance(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}

	var req struct {
		ProviderIDs []string `json:"provider_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// A chain entry names either a single provider or a combination — that is
	// what lets "this pair of models, then that one" be written as one ordered
	// list. Unknown entries are rejected: a chain pointing at nothing looks
	// configured and silently falls through to the fleet default.
	known, err := s.db.ListProviders(r.Context(), false)
	if err != nil {
		failErr(w, err)
		return
	}
	byID := make(map[string]bool, len(known))
	for _, p := range known {
		byID[p.ID] = true
	}
	combos, err := s.db.ListModelCombos(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	for _, c := range combos {
		byID[c.ID] = true
	}
	seen := make(map[string]bool, len(req.ProviderIDs))
	chain := make([]string, 0, len(req.ProviderIDs))
	for _, pid := range req.ProviderIDs {
		if !byID[pid] {
			fail(w, http.StatusBadRequest, "no such provider or combination: "+pid)
			return
		}
		if seen[pid] {
			continue // a duplicate in a fallback order means nothing
		}
		seen[pid] = true
		chain = append(chain, pid)
	}

	inst.ProviderIDs = chain
	if err := s.db.UpdateInstance(r.Context(), inst); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleSetInstanceAccess(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit); !ok {
		return
	}
	inst, err := s.db.Instance(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}

	var req struct {
		ShellAccess *bool `json:"shell_access"`
		// Voice is a label on the instance rather than a container property.
		Voice *string `json:"voice"`
		// SystemPrompt is the bot's personality. Editable at any time: it is
		// read when a prompt is built, not baked into the container, so a
		// change applies to the agent's next turn and its next chat reply.
		SystemPrompt *string `json:"system_prompt"`
		// VoiceSpeed is a multiplier; 0 means the operator's default.
		VoiceSpeed *float64 `json:"voice_speed"`
		// SudoAccess is applied to the live container before it is recorded,
		// so a failure leaves the stored state matching reality.
		SudoAccess *bool `json:"sudo_access"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ShellAccess == nil && req.Voice == nil && req.SudoAccess == nil &&
		req.SystemPrompt == nil && req.VoiceSpeed == nil {
		fail(w, http.StatusBadRequest, "nothing to change")
		return
	}
	// The platform-wide kill switch still wins: an operator cannot grant shell
	// on a deployment that has disabled it entirely.
	if req.ShellAccess != nil {
		// The platform-wide kill switch still wins.
		if *req.ShellAccess && !s.cfg.AllowShell {
			fail(w, http.StatusForbidden, "shell access is disabled for this deployment")
			return
		}
		inst.ShellAccess = *req.ShellAccess
	}
	if req.Voice != nil {
		inst.Voice = strings.TrimSpace(*req.Voice)
	}
	if req.SystemPrompt != nil {
		inst.SystemPrompt = strings.TrimSpace(*req.SystemPrompt)
	}
	if req.VoiceSpeed != nil {
		// Clamped rather than rejected: the slider cannot produce anything
		// outside this, and a speed of 0.05 or 40 is not a preference, it is
		// a bot nobody can listen to.
		sp := *req.VoiceSpeed
		if sp != 0 && (sp < 0.5 || sp > 2.0) {
			fail(w, http.StatusBadRequest, "voice speed must be between 0.5 and 2.0")
			return
		}
		inst.VoiceSpeed = sp
	}
	if req.SudoAccess != nil && *req.SudoAccess != inst.SudoAccess {
		if inst.State != protocol.InstanceRunning {
			fail(w, http.StatusConflict,
				"sudo can only be changed while the agent is running")
			return
		}
		// Change the container first. Recording a grant that did not take
		// would leave the UI claiming sudo is off while it still works.
		if err := s.fleet.SetSudo(r.Context(), inst, *req.SudoAccess); err != nil {
			failErr(w, err)
			return
		}
		inst.SudoAccess = *req.SudoAccess
	}
	if err := s.db.UpdateInstance(r.Context(), inst); err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("instance.state", inst.ID, "", inst)
	writeJSON(w, http.StatusOK, redact(*inst))
}
