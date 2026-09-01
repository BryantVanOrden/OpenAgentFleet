package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Managing the chats you have with one bot.
//
// A bot used to have a single unbounded history, so there was no way to start
// a fresh chat or clear one that had gone somewhere unhelpful without losing
// every conversation you had ever had with that agent.

func (s *Server) handleListChatSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("instanceID"), protocol.PermRead); !ok {
		return
	}
	id := r.PathValue("instanceID")
	if _, err := s.db.Instance(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	sessions, err := s.db.ListChatSessions(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

type chatSessionReq struct {
	Title  string `json:"title"`
	Pinned *bool  `json:"pinned"`
}

func (s *Server) handleCreateChatSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("instanceID"), protocol.PermChat); !ok {
		return
	}
	id := r.PathValue("instanceID")
	if _, err := s.db.Instance(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}

	var req chatSessionReq
	_ = json.NewDecoder(r.Body).Decode(&req) // an untitled chat is fine

	session, err := s.db.CreateChatSession(r.Context(), id, strings.TrimSpace(req.Title))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

// handleUpdateChatSession renames or pins a chat.
func (s *Server) handleUpdateChatSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("instanceID"), protocol.PermChat); !ok {
		return
	}
	instanceID := r.PathValue("instanceID")
	chatID := r.PathValue("chatID")

	if chatID == store.DefaultChatSessionID {
		// The default chat is not a row -- it stands for the messages recorded
		// before chats could be separated, so there is nothing to rename or pin.
		fail(w, http.StatusBadRequest, "the earlier chat cannot be renamed or pinned")
		return
	}
	ok, err := s.db.ChatSessionExists(r.Context(), instanceID, chatID)
	if err != nil {
		failErr(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "no such chat")
		return
	}

	var req chatSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Both fields are optional and independent: pinning a chat must not wipe
	// its name just because the request did not repeat it.
	if title := strings.TrimSpace(req.Title); title != "" {
		if err := s.db.RenameChatSession(r.Context(), instanceID, chatID, title); err != nil {
			failErr(w, err)
			return
		}
	}
	if req.Pinned != nil {
		if err := s.db.PinChatSession(r.Context(), instanceID, chatID, *req.Pinned); err != nil {
			failErr(w, err)
			return
		}
	}

	sessions, err := s.db.ListChatSessions(r.Context(), instanceID)
	if err != nil {
		failErr(w, err)
		return
	}
	for _, c := range sessions {
		if c.ID == chatID {
			writeJSON(w, http.StatusOK, c)
			return
		}
	}
	fail(w, http.StatusNotFound, "no such chat")
}

func (s *Server) handleDeleteChatSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePerm(w, r, r.PathValue("instanceID"), protocol.PermChat); !ok {
		return
	}
	instanceID := r.PathValue("instanceID")
	chatID := r.PathValue("chatID")

	ok, err := s.db.ChatSessionExists(r.Context(), instanceID, chatID)
	if err != nil {
		failErr(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "no such chat")
		return
	}
	if err := s.db.DeleteChatSession(r.Context(), instanceID, chatID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
