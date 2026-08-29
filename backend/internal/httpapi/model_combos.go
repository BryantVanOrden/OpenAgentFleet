package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Combinations: which model does what.
//
// A simple one pairs a brain and a pair of hands. A complex one assigns every
// role separately. Either can sit in a bot's fallback chain next to a plain
// provider, which is how "this pair, then that single model" is expressed.

func (s *Server) handleListModelCombos(w http.ResponseWriter, r *http.Request) {
	combos, err := s.db.ListModelCombos(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, combos)
}

func (s *Server) handleUpsertModelCombo(w http.ResponseWriter, r *http.Request) {
	var c protocol.ModelCombo
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if id := r.PathValue("id"); id != "" {
		c.ID = id
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		fail(w, http.StatusBadRequest, "a combination needs a name")
		return
	}
	if len(c.Roles) == 0 {
		fail(w, http.StatusBadRequest, "assign at least one role")
		return
	}

	// Validate before storing. A combination naming a provider that does not
	// exist looks configured and quietly contributes nothing for that role.
	known, err := s.db.ListProviders(r.Context(), false)
	if err != nil {
		failErr(w, err)
		return
	}
	byID := make(map[string]protocol.Provider, len(known))
	for _, p := range known {
		byID[p.ID] = p
	}
	valid := map[string]bool{}
	for _, role := range protocol.ModelRoles {
		valid[role] = true
	}
	for role, providerID := range c.Roles {
		if !valid[role] {
			fail(w, http.StatusBadRequest, "unknown role: "+role)
			return
		}
		p, ok := byID[providerID]
		if !ok {
			fail(w, http.StatusBadRequest, "no such provider for role "+role)
			return
		}
		// The hands must be able to see. Assigning a text-only model here
		// produces an agent that is skipped on every turn that carries a
		// screenshot, which looks like the agent doing nothing at all.
		if role == protocol.RoleVision && !p.Vision {
			fail(w, http.StatusBadRequest,
				p.Name+" cannot see the screen, so it cannot be the hands")
			return
		}
	}

	if err := s.db.UpsertModelCombo(r.Context(), &c); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteModelCombo(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteModelCombo(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleResolveChain shows which model would actually serve each role for a
// bot, after combinations and fallbacks are applied.
//
// Worth exposing: a chain of combinations and providers with per-role fallback
// is not something an operator should have to simulate in their head to find
// out that their expensive reasoner is never reached.
func (s *Server) handleResolveChain(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermView)
	if !ok {
		return
	}
	providers, err := s.db.ListProviders(r.Context(), false)
	if err != nil {
		failErr(w, err)
		return
	}
	names := make(map[string]string, len(providers))
	for _, p := range providers {
		names[p.ID] = p.Name + " (" + p.Model + ")"
	}

	out := map[string]any{}
	for _, role := range protocol.ModelRoles {
		chain := s.models.ResolveChain(r.Context(), inst.ProviderIDs, role)
		labelled := make([]map[string]string, 0, len(chain))
		for _, id := range chain {
			labelled = append(labelled, map[string]string{
				"provider_id": id,
				"name":        names[id],
			})
		}
		out[role] = labelled
	}
	writeJSON(w, http.StatusOK, out)
}
