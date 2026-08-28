package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/swarm"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

var globalSwarmCoordinator = swarm.NewCoordinator()

func (s *Server) handleListSwarms(w http.ResponseWriter, r *http.Request) {
	list := globalSwarmCoordinator.ListSwarms(r.Context())
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetSwarm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sw, err := globalSwarmCoordinator.GetSwarm(r.Context(), id)
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
}

func (s *Server) handleCreateSwarm(w http.ResponseWriter, r *http.Request) {
	var req CreateSwarmReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Mission == "" {
		fail(w, http.StatusBadRequest, "name and mission are required")
		return
	}
	if len(req.Members) == 0 {
		// Provide default 3-bot collaborative team if none specified
		req.Members = []protocol.SwarmMember{
			{InstanceID: "inst-lead", InstanceName: "Full-Stack Bot", Role: "Lead Architect", ArchetypeID: "fullstack_dev", Status: "working"},
			{InstanceID: "inst-qa", InstanceName: "QA/UX Bot", Role: "QA Auditor", ArchetypeID: "qa_ui_ux", Status: "working"},
			{InstanceID: "inst-sec", InstanceName: "CyberSec Bot", Role: "Security Auditor", ArchetypeID: "cyber_ops", Status: "working"},
		}
	}

	sw, err := globalSwarmCoordinator.CreateSwarm(r.Context(), req.Name, req.Mission, req.Members)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sw)
}

type PostSwarmMessageReq struct {
	FromBot   string   `json:"from_bot"`
	ToBot     string   `json:"to_bot"`
	Phase     string   `json:"phase"`
	Content   string   `json:"content"`
	Artifacts []string `json:"artifacts,omitempty"`
}

func (s *Server) handlePostSwarmMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req PostSwarmMessageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	msg, err := globalSwarmCoordinator.PostMessage(r.Context(), id, req.FromBot, req.ToBot, req.Phase, req.Content, req.Artifacts)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}
