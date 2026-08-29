package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ------------------------------------------------------------- Shared Secrets ---

// sharedSecretView is the API projection of a shared fleet secret. It carries
// the metadata and deliberately omits the value, mirroring Vault.Refs ("lists
// secret names and notes — never values").
//
// This matters because GET /api/vault/secrets is gated at roleAny, which
// includes the read-only auditor role. Returning protocol.SharedSecret directly
// handed every caller the plaintext of every fleet credential. Consumers that
// legitimately need a value resolve it in-process through
// vault.GlobalBus.GetSecret; it is never sent over the wire.
type sharedSecretView struct {
	Key       string    `json:"key"`
	Scope     string    `json:"scope"`
	Note      string    `json:"note,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	HasValue  bool      `json:"has_value"`
}

func redactSharedSecret(sec protocol.SharedSecret) sharedSecretView {
	return sharedSecretView{
		Key:       sec.Key,
		Scope:     sec.Scope,
		Note:      sec.Note,
		CreatedBy: sec.CreatedBy,
		UpdatedAt: sec.UpdatedAt,
		HasValue:  sec.Value != "",
	}
}

func (s *Server) handleListSharedSecrets(w http.ResponseWriter, r *http.Request) {
	list := vault.GlobalBus.ListSecrets(r.Context())
	out := make([]sharedSecretView, 0, len(list))
	for _, sec := range list {
		out = append(out, redactSharedSecret(sec))
	}
	writeJSON(w, http.StatusOK, out)
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
	// Echo the metadata only — the response body is the kind of thing that ends
	// up in proxy logs and browser history, so the plaintext does not go back
	// out even to the caller that just supplied it.
	writeJSON(w, http.StatusOK, redactSharedSecret(sec))
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
	// ConversationID files the message in a thread. Empty means the message
	// is placed by its recipient, which is what agents that know nothing about
	// conversations still do.
	ConversationID   string         `json:"conversation_id"`
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
	// Addressing a two-party thread implies the recipient, so the operator does
	// not have to say both. Without this, a message typed into a pair thread
	// would broadcast to the whole fleet.
	if req.ToInstanceID == "broadcast" && req.ConversationID != "" &&
		req.ConversationID != protocol.BroadcastConversationID {
		if to, ok := s.otherMemberOf(r.Context(), req.ConversationID, req.FromInstanceID); ok {
			req.ToInstanceID = to
		}
	}
	if req.FromInstanceName == "" {
		req.FromInstanceName = "Operator"
	}
	msg := vault.GlobalBus.SendMessageIn(r.Context(), req.ConversationID, req.FromInstanceID,
		req.FromInstanceName, req.ToInstanceID, req.Kind, req.Content, req.Data)
	writeJSON(w, http.StatusCreated, msg)
}

// otherMemberOf resolves who a message in this thread is for, given who sent
// it. Only meaningful for two-party threads; a group message stays a broadcast
// because there is no single recipient to pick.
func (s *Server) otherMemberOf(ctx context.Context, conversationID, senderID string) (string, bool) {
	if senderID == "" {
		senderID = protocol.OperatorMemberID
	}
	for _, c := range vault.GlobalBus.ListConversations(ctx) {
		if c.ID != conversationID {
			continue
		}
		if len(c.Members) != 2 {
			return "", false
		}
		for _, m := range c.Members {
			if m != senderID && m != protocol.OperatorMemberID {
				return m, true
			}
		}
	}
	return "", false
}
