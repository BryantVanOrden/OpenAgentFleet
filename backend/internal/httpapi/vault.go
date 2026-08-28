package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
)

// ------------------------------------------------------------- Shared Secrets ---

func (s *Server) handleListSharedSecrets(w http.ResponseWriter, r *http.Request) {
	list := vault.GlobalBus.ListSecrets(r.Context())
	writeJSON(w, http.StatusOK, list)
}

type PutSharedSecretReq struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Scope string `json:"scope"`
	Note  string `json:"note"`
}

func (s *Server) handlePutSharedSecret(w http.ResponseWriter, r *http.Request) {
	var req PutSharedSecretReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Key == "" || req.Value == "" {
		fail(w, http.StatusBadRequest, "key and value are required")
		return
	}
	sec := vault.GlobalBus.PutSecret(r.Context(), req.Key, req.Value, req.Scope, req.Note, "operator")
	writeJSON(w, http.StatusOK, sec)
}

func (s *Server) handleDeleteSharedSecret(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	vault.GlobalBus.DeleteSecret(r.Context(), key)
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------ Shared Sessions ---

func (s *Server) handleListSharedSessions(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	list := vault.GlobalBus.ListSessions(r.Context(), domain)
	writeJSON(w, http.StatusOK, list)
}

type SaveSharedSessionReq struct {
	Domain           string `json:"domain"`
	Title            string `json:"title"`
	CookiesJSON      string `json:"cookies_json"`
	LocalStorageJSON string `json:"local_storage_json"`
	CreatedBy        string `json:"created_by"`
}

func (s *Server) handleSaveSharedSession(w http.ResponseWriter, r *http.Request) {
	var req SaveSharedSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Domain == "" || req.CookiesJSON == "" {
		fail(w, http.StatusBadRequest, "domain and cookies_json are required")
		return
	}
	sess := vault.GlobalBus.SaveSession(r.Context(), req.Domain, req.Title, req.CookiesJSON, req.LocalStorageJSON, req.CreatedBy)
	writeJSON(w, http.StatusCreated, sess)
}

// ---------------------------------------------------------- Inter-Agent Comms ---

func (s *Server) handleListPeerMessages(w http.ResponseWriter, r *http.Request) {
	instID := r.URL.Query().Get("instance_id")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	msgs := vault.GlobalBus.ListMessages(r.Context(), instID, limit)
	writeJSON(w, http.StatusOK, msgs)
}

type SendPeerMessageReq struct {
	FromInstanceID   string         `json:"from_instance_id"`
	FromInstanceName string         `json:"from_instance_name"`
	ToInstanceID     string         `json:"to_instance_id"`
	Kind             string         `json:"kind"`
	Content          string         `json:"content"`
	Data             map[string]any `json:"data"`
}

func (s *Server) handleSendPeerMessage(w http.ResponseWriter, r *http.Request) {
	var req SendPeerMessageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		fail(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.ToInstanceID == "" {
		req.ToInstanceID = "broadcast"
	}
	if req.FromInstanceName == "" {
		req.FromInstanceName = "Operator"
	}
	msg := vault.GlobalBus.SendMessage(r.Context(), req.FromInstanceID, req.FromInstanceName, req.ToInstanceID, req.Kind, req.Content, req.Data)
	writeJSON(w, http.StatusCreated, msg)
}
