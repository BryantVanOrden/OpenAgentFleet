package httpapi

import (
	"context"
	"net/http"

	"encoding/base64"
	"fmt"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/agent"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/recorder"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
	"strings"
	"time"
)

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListSkills(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.db.Skill(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

// handleUpsertSkill backs the timeline editor: the operator prunes steps,
// renames things and marks a typed value as a parameter, and the SKILL.md is
// re-rendered from the edited steps rather than hand-maintained.
func (s *Server) handleUpsertSkill(w http.ResponseWriter, r *http.Request) {
	var sk protocol.Skill
	if err := readJSON(r, &sk); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if id := r.PathValue("id"); id != "" {
		sk.ID = id
	}
	if sk.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	for i := range sk.Steps {
		sk.Steps[i].Index = i + 1
	}
	sk.Markdown = recorder.Render(&sk)

	if err := s.db.UpsertSkill(r.Context(), &sk); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteSkill(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------- recording studio ---

func (s *Server) handleRecordStart(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermDesktop); !ok {
		return
	}
	inst, err := s.db.Instance(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" {
		req.Name = "Untitled recording"
	}
	if err := agent.NewSandboxClient(inst.AgentdURL).StartRecording(r.Context(), req.Name); err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("record.started", inst.ID, "", map[string]string{"name": req.Name})
	writeJSON(w, http.StatusOK, map[string]string{"status": "recording", "name": req.Name})
}

// handleRecordStop compiles the raw trace into a skill and saves it. The raw
// events are kept as an artifact so a compilation change can be re-run later
// without asking the human to demonstrate again.
func (s *Server) handleRecordStop(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermDesktop); !ok {
		return
	}
	inst, err := s.db.Instance(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return
	}
	res, err := agent.NewSandboxClient(inst.AgentdURL).StopRecording(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}

	skill := recorder.Compile(res.Name, res.Events)
	// The id has to exist before the pictures are filed, because it is what
	// they are filed under. Left to UpsertSkill, every recording wrote its
	// frames to "recordings//step-NN.webp" and the second recording overwrote
	// the first.
	if skill.ID == "" {
		skill.ID = store.NewID()
	}
	s.describeSteps(r.Context(), inst, skill)
	if err := s.db.UpsertSkill(r.Context(), skill); err != nil {
		failErr(w, err)
		return
	}
	if s.art != nil {
		key := "recordings/" + skill.ID + "/events.json"
		if body, err := jsonBytes(res.Events); err == nil {
			if err := s.art.Put(r.Context(), key, "application/json", body); err == nil {
				skill.SourceRunID = key
				_ = s.db.UpsertSkill(r.Context(), skill)
			}
		}
	}
	s.bus.Emit("record.stopped", inst.ID, "", map[string]any{
		"skill_id": skill.ID, "steps": len(skill.Steps),
	})
	writeJSON(w, http.StatusOK, skill)
}

// handleRefineSkill triggers AI self-refinement on an existing skill using the execution
// trace from its most recent task run.
func (s *Server) handleRefineSkill(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	sk, err := s.db.Skill(r.Context(), skillID)
	if err != nil {
		failErr(w, err)
		return
	}

	var req struct {
		TaskID string `json:"task_id"`
	}
	_ = readJSON(r, &req)

	var task *protocol.Task
	if req.TaskID != "" {
		// The task comes from the body, so the check happens here rather than
		// at the top: refinement reads a whole trajectory, which is the bot's
		// work in detail.
		var ok bool
		if task, ok = s.requirePermForTask(w, r, req.TaskID, protocol.PermRead); !ok {
			return
		}
	} else {
		// Find most recent task referencing this skill
		tasks, err := s.db.ListTasks(r.Context(), "", 50)
		if err != nil {
			failErr(w, err)
			return
		}
		// Same rule when the task is found rather than named: scanning the
		// fleet for a matching run must not reach into another department's.
		acc := accessFrom(r.Context())
		instances, _ := s.db.ListInstances(r.Context())
		orgOf := make(map[string][]string, len(instances))
		for _, in := range instances {
			orgOf[in.ID] = in.OrgIDs
		}
		for i := range tasks {
			if !acc.Can(protocol.PermRead, orgOf[tasks[i].InstanceID], tasks[i].InstanceID) {
				continue
			}
			if tasks[i].SkillID == skillID && tasks[i].State == protocol.TaskSucceeded {
				task = &tasks[i]
				break
			}
		}
		if task == nil {
			// Fall back to any task referencing this skill
			for i := range tasks {
				if tasks[i].SkillID == skillID {
					task = &tasks[i]
					break
				}
			}
		}
	}

	if task == nil {
		fail(w, http.StatusBadRequest, "no task runs found for this skill to refine against")
		return
	}

	steps, err := s.db.ListSteps(r.Context(), task.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	if len(steps) == 0 {
		fail(w, http.StatusBadRequest, "task has no recorded steps to refine against")
		return
	}

	refined, err := s.runner.Refiner().RefineSkill(r.Context(), task, sk, steps)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refined)
}

// describeSteps files each step's picture and says what is in it.
//
// A recorded step is a coordinate and, where the application exposes one, an
// accessible label. Firefox exposes nothing, so a demonstration of using a
// browser compiled to "Click at 690,121" -- which replays on an identical
// screen and tells an agent nothing about what it is aiming at. Watching one
// replay bears that out: it found the address bar, clicked it three more times,
// and stopped.
//
// The recorder now takes a picture at each interesting moment, so there is
// something to look at. This stores it and asks the vision model to name the
// control, turning a coordinate into "the address bar at the top of the
// browser window" -- which is what the skill file promised to contain all
// along.
func (s *Server) describeSteps(ctx context.Context, inst *protocol.Instance, sk *protocol.Skill) {
	if s.art == nil {
		return
	}
	chain := connectors.PreferredChain("", inst.ProviderIDs)

	for i := range sk.Steps {
		step := &sk.Steps[i]
		encoded := step.Meta["frame"]
		if encoded == "" {
			continue
		}
		// The picture never goes in the database: it is megabytes of base64
		// per recording, and jsonb is the wrong place for it.
		delete(step.Meta, "frame")

		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("recordings/%s/step-%02d.webp", sk.ID, step.Index)
		if err := s.art.Put(ctx, key, "image/webp", raw); err != nil {
			s.log.Warn("could not store a recorded step's picture",
				"skill", sk.ID, "step", step.Index, "err", err)
			continue
		}
		step.Meta["frame"] = key

		// An application that exposed a real label has already said it better
		// than a model will guess it.
		if strings.TrimSpace(step.Label) != "" {
			continue
		}
		if label := s.nameTheControl(ctx, chain, encoded, step); label != "" {
			step.Label = label
		}
	}
	// Compile rendered the markdown before any of this existed, so the
	// descriptions would never have reached the file anybody reads.
	sk.Markdown = recorder.Render(sk)
}

// nameTheControl asks what is at the point the demonstration touched.
func (s *Server) nameTheControl(ctx context.Context, chain []string, frameB64 string, step *protocol.SkillStep) string {
	where := "the screen"
	if len(step.Coordinates) == 2 {
		where = fmt.Sprintf("the point %d,%d", step.Coordinates[0], step.Coordinates[1])
	}
	what := "clicked"
	if step.Kind == protocol.ActKey {
		what = fmt.Sprintf("pressed %q with the focus", step.Key)
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	resp, err := s.models.CompleteRole(ctx, chain, protocol.RoleVision, connectors.Request{
		System: "You name user-interface controls. You answer with a short " +
			"noun phrase and nothing else: no sentence, no explanation, no " +
			"punctuation at the end.",
		MaxTokens: 40,
		Messages: []connectors.Message{{
			Role: connectors.RoleUser,
			Text: fmt.Sprintf(
				"Someone demonstrating a task %s at %s in this screenshot.\n\n"+
					"Name what is there, the way you would tell a colleague "+
					"where to click: its visible text if it has any, and where "+
					"it is if it does not. For example: the \"Roll Dice\" button, "+
					"or the address bar at the top of the browser.\n\n"+
					"If you cannot tell, answer exactly: unknown",
				what, where),
			Image:     frameB64,
			ImageMime: "image/webp",
		}},
	})
	if err != nil {
		s.log.Warn("could not describe a recorded step", "err", err)
		return ""
	}
	label := strings.TrimSpace(resp.Text)
	label = strings.Trim(label, "\"'.")
	if label == "" || strings.EqualFold(label, "unknown") || len(label) > 120 {
		return ""
	}
	return label
}
