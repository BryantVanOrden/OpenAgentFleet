package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/swarm"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

var globalSwarmCoordinator = swarm.NewCoordinator()

func (s *Server) handleListSwarms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, globalSwarmCoordinator.ListSwarms(r.Context()))
}

func (s *Server) handleGetSwarm(w http.ResponseWriter, r *http.Request) {
	sw, err := globalSwarmCoordinator.GetSwarm(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sw)
}

type CreateSwarmReq struct {
	Name    string                 `json:"name"`
	Mission string                 `json:"mission"`
	Members []protocol.SwarmMember `json:"members"`
	// PlanFirst holds execution until every member has published a plan
	// artifact (or an operator advances the phase by hand).
	PlanFirst bool `json:"plan_first,omitempty"`
}

func (s *Server) handleCreateSwarm(w http.ResponseWriter, r *http.Request) {
	var req CreateSwarmReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// No invented default team any more. With no members specified this used to
	// fabricate three bots -- "inst-lead", "inst-qa", "inst-sec" -- that exist
	// on no fleet, so the screen showed a running mission staffed by fiction and
	// nothing could ever be dispatched to any of them.
	sw, err := globalSwarmCoordinator.CreateSwarm(r.Context(), req.Name, req.Mission, req.Members, req.PlanFirst)
	if err != nil {
		if errors.Is(err, swarm.ErrNoRunner) {
			fail(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sw)
}

func (s *Server) handleDeleteSwarm(w http.ResponseWriter, r *http.Request) {
	globalSwarmCoordinator.DeleteSwarm(r.Context(), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

type PostSwarmMessageReq struct {
	FromBot   string   `json:"from_bot"`
	ToBot     string   `json:"to_bot"`
	Phase     string   `json:"phase"`
	Content   string   `json:"content"`
	Artifacts []string `json:"artifacts,omitempty"`
}

func (s *Server) handlePostSwarmMessage(w http.ResponseWriter, r *http.Request) {
	var req PostSwarmMessageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	msg, err := globalSwarmCoordinator.PostMessage(r.Context(), r.PathValue("id"),
		req.FromBot, req.ToBot, req.Phase, req.Content, req.Artifacts)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

// ---------------------------------------------------------------- artifacts ---

type PublishArtifactReq struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Category string `json:"category"`
	Content  string `json:"content"`
}

// handlePublishArtifact posts a deliverable and sends it out for peer review.
//
// There was no route for this at all: PublishArtifact existed on the coordinator
// with nothing calling it, so the docs described publishing an artifact and
// there was no way to do it.
func (s *Server) handlePublishArtifact(w http.ResponseWriter, r *http.Request) {
	var req PublishArtifactReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	art, err := globalSwarmCoordinator.PublishArtifact(r.Context(), r.PathValue("id"),
		req.Title, req.Author, req.Category, req.Content)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, art)
}

type ReviewArtifactReq struct {
	Reviewer string `json:"reviewer"`
	Approved bool   `json:"approved"`
	Notes    string `json:"notes"`
}

// handleReviewArtifact records one member's verdict on an artifact.
//
// SwarmArtifact.ApprovedBy existed with nothing able to fill it in, so "verified
// deliverable" meant nothing had verified it.
func (s *Server) handleReviewArtifact(w http.ResponseWriter, r *http.Request) {
	var req ReviewArtifactReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	art, err := globalSwarmCoordinator.ReviewArtifact(r.Context(), r.PathValue("id"),
		r.PathValue("artifactId"), req.Reviewer, req.Approved, req.Notes)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, art)
}

// ------------------------------------------------------------------- wiring ---

// wireSwarms gives the coordinator a way to start real work.
//
// Without it CreateSwarm refuses rather than recording a mission nothing will
// ever act on -- the same choice the pipeline engine makes.
func (s *Server) wireSwarms() {
	globalSwarmCoordinator.Wire(
		// Instance-pinned, never archetype-resolved: a swarm names the exact
		// bots that are on the mission, and silently running a member's work
		// on some other free instance would make the member list a lie. Each
		// part is a ticket under the mission's (work_tickets.go).
		s.startMissionWork,
		func(ctx context.Context, instanceID string) (string, string, error) {
			inst, err := s.db.Instance(ctx, instanceID)
			if err != nil {
				return "", "", err
			}
			return inst.Name, inst.ArchetypeID, nil
		},
		s.logger(),
	)
}

// handleAdvanceSwarmPhase lifts a planning barrier by hand.
//
// The automatic path is every member publishing a plan; this is the operator
// override for the stuck case — one erroring member should not hold a mission
// hostage, and deciding to proceed anyway is a human call.
func (s *Server) handleAdvanceSwarmPhase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	who := "operator"
	if u := userFrom(r.Context()); u != nil && u.Email != "" {
		who = u.Email
	}
	if err := globalSwarmCoordinator.AdvancePhase(r.Context(), id, who); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	sw, err := globalSwarmCoordinator.GetSwarm(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sw)
}
