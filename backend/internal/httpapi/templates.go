package httpapi

import (
	"net/http"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	templates := protocol.DefaultBotTemplates()
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tmpl := protocol.BotTemplateByID(id)
	if tmpl == nil {
		fail(w, http.StatusNotFound, "bot template not found")
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}
