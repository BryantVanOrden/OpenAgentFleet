package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListProviders(r.Context(), queryBool(r, "enabled_only"))
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// upsertProviderRequest carries the API key inline for convenience; it is sealed
// into the vault immediately and only the ref is persisted on the provider row.
type upsertProviderRequest struct {
	protocol.Provider
	APIKey string `json:"api_key,omitempty"`
}

func (s *Server) handleUpsertProvider(w http.ResponseWriter, r *http.Request) {
	var req upsertProviderRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if id := r.PathValue("id"); id != "" {
		req.ID = id
	}
	p := req.Provider
	if p.Name == "" || p.Model == "" {
		fail(w, http.StatusBadRequest, "name and model are required")
		return
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = 1024
	}
	if p.Priority <= 0 {
		p.Priority = 100
	}

	if req.APIKey != "" {
		ref := p.APIKeyRef
		if ref == "" {
			ref = "provider/" + slug(p.Name) + "/api_key"
			p.APIKeyRef = ref
		}
		if err := s.vault.Put(r.Context(), ref, req.APIKey, "API key for "+p.Name); err != nil {
			failErr(w, err)
			return
		}
	}
	if err := s.db.UpsertProvider(r.Context(), &p); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.db.DeleteProvider(r.Context(), r.PathValue("id")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleProbeProvider does a real round-trip so the operator finds out about a
// bad key here rather than three steps into a run.
func (s *Server) handleProbeProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.models.Probe(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleOllamaModels lists what a local Ollama host actually has pulled, so the
// model field is a dropdown rather than a guess.
func (s *Server) handleOllamaModels(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base_url")
	if base == "" {
		base = "http://" + s.cfg.HostGateway + ":11434"
	}
	models, err := connectors.ListOllamaModels(r.Context(), base)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleAntigravityModels returns the dynamically available Google Antigravity & Gemini models.
func (s *Server) handleAntigravityModels(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base_url")
	key := r.URL.Query().Get("api_key")
	models, err := connectors.ListAntigravityModels(r.Context(), base, key)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []connectors.AntigravityModelInfo{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleDynamicModels discovers models for any provider kind.
//
// A failed discovery still returns a usable list — the built-in catalogue is
// better than an empty dropdown — but it is reported as `live: false` with the
// reason. Returning a confident-looking list of models the provider may not
// serve is how an operator ends up picking one that 404s three steps into a run.
func (s *Server) handleDynamicModels(w http.ResponseWriter, r *http.Request) {
	kind := protocol.ProviderKind(r.URL.Query().Get("kind"))
	base := r.URL.Query().Get("base_url")
	key := r.URL.Query().Get("api_key")
	if kind == "" {
		kind = protocol.ProviderOllama
	}

	models, err := connectors.ListDynamicModels(r.Context(), kind, base, key)
	if models == nil {
		models = []connectors.ModelDescriptor{}
	}

	var fallback *connectors.CatalogueFallback
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"models": models, "live": true})
	case errors.As(err, &fallback):
		writeJSON(w, http.StatusOK, map[string]any{
			"models": models, "live": false, "reason": fallback.Reason,
		})
	default:
		// An unknown provider kind, not a discovery failure.
		writeJSON(w, http.StatusOK, map[string]any{
			"models": []connectors.ModelDescriptor{}, "live": false, "error": err.Error(),
		})
	}
}

// -------------------------------------------------------------- secrets ---

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	refs, err := s.vault.Refs(r.Context())
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refs)
}

func (s *Server) handlePutSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value string `json:"value"`
		Note  string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Value == "" {
		fail(w, http.StatusBadRequest, "value is required")
		return
	}
	if err := s.vault.Put(r.Context(), r.PathValue("ref"), req.Value, req.Note); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	if err := s.vault.Delete(r.Context(), r.PathValue("ref")); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----------------------------------------------------------------- helpers ---

func slug(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		case r == ' ' || r == '-' || r == '_':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "provider"
	}
	return string(out)
}

func jsonBytes(v any) ([]byte, error) { return json.Marshal(v) }
