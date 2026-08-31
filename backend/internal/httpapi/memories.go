package httpapi

import (
	"net/http"
	"strconv"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/memory"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// What a bot has chosen to keep.
//
// Agents decide for themselves what is worth remembering, and until now there
// was no way to see what that turned out to be — a bot could carry a wrong
// conclusion indefinitely with nobody able to look, let alone correct it.

func (s *Server) handleListInstanceMemories(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermRead); !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := s.db.Instance(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil {
			limit = l
		}
	}
	// Still a bare array. The Flutter memory screen decodes this as a list, so
	// wrapping it in an object to carry the shared pool alongside would break
	// the app on the next deploy. The shared pool and the search mode get their
	// own endpoint below instead.
	writeJSON(w, http.StatusOK,
		memory.GlobalEngine.ListNamespace(r.Context(), memory.BotNamespace(id), limit))
}

// handleFleetMemory lists the shared pool and says how search is scored.
//
// New rather than folded into the per-bot listing, for two reasons. The shared
// namespace is not any one bot's — it is the pool every agent reads and can now
// write to, which had no route at all because nothing ever wrote to it. And the
// search mode belongs somewhere visible: "semantic search" that has quietly
// fallen back to keyword overlap is the kind of thing an operator builds a
// workflow on top of.
func (s *Server) handleFleetMemory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil {
			limit = l
		}
	}

	model, semantic := memory.GlobalEngine.UsingEmbeddings()
	writeJSON(w, http.StatusOK, map[string]any{
		"shared": memory.GlobalEngine.ListNamespace(r.Context(), memory.FleetNamespace, limit),
		"tasks":  memory.GlobalEngine.ListNamespace(r.Context(), memory.TasksNamespace, limit),
		"search": map[string]any{
			"semantic": semantic,
			"model":    model,
		},
	})
}

// handleForgetMemory removes one memory from a bot.
func (s *Server) handleForgetMemory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit); !ok {
		return
	}
	if !memory.GlobalEngine.Forget(r.Context(), r.PathValue("memoryId")) {
		fail(w, http.StatusNotFound, "no such memory")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
