package httpapi

import (
	"net/http"
)

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListAlerts(r.Context(), queryBool(r, "open"), queryInt(r, "limit", 100))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleReplyAlert is the resolution-centre endpoint. Whatever the operator
// writes here is handed to the waiting agent as authoritative instruction — the
// one channel in the system that is trusted over what is on screen.
func (s *Server) handleReplyAlert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reply string `json:"reply"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	id := r.PathValue("id")
	if err := s.db.ResolveAlert(r.Context(), id, req.Reply); err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("alert.resolved", "", "", map[string]string{
		"alert_id": id, "by": userFrom(r.Context()).Email,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------- devices ---

func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Token == "" {
		fail(w, http.StatusBadRequest, "token is required")
		return
	}
	if req.Platform != "ios" {
		req.Platform = "android"
	}
	if err := s.db.RegisterDevice(r.Context(), req.Token, userFrom(r.Context()).Subject, req.Platform); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUnregisterDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteDevice(r.Context(), r.PathValue("token")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --------------------------------------------------------------- artifacts ---

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	if s.art == nil {
		fail(w, http.StatusNotFound, "artifact storage is not configured")
		return
	}
	body, contentType, err := s.art.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		failErr(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
