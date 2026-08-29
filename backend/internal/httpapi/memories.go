package httpapi

import (
	"net/http"
	"strconv"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/memory"
)

// What a bot has chosen to keep.
//
// Agents decide for themselves what is worth remembering, and until now there
// was no way to see what that turned out to be — a bot could carry a wrong
// conclusion indefinitely with nobody able to look, let alone correct it.

func (s *Server) handleListInstanceMemories(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK,
		memory.GlobalEngine.ListNamespace(r.Context(), memory.BotNamespace(id), limit))
}

// handleForgetMemory removes one memory from a bot.
func (s *Server) handleForgetMemory(w http.ResponseWriter, r *http.Request) {
	if !memory.GlobalEngine.Forget(r.Context(), r.PathValue("memoryId")) {
		fail(w, http.StatusNotFound, "no such memory")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
