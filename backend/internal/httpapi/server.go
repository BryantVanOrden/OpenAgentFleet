// Package httpapi is the orchestrator's REST + WebSocket surface, shared by the
// admin panel and the Flutter companion app.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/agent"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/artifacts"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/bus"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/config"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
)

type Server struct {
	cfg    *config.Config
	db     *store.Store
	fleet  *fleet.Manager
	models *connectors.Registry
	runner *agent.Runner
	bus    *bus.Bus
	vault  *vault.Vault
	art    artifacts.Store
	log    *slog.Logger
}

func NewServer(
	cfg *config.Config,
	db *store.Store,
	fm *fleet.Manager,
	models *connectors.Registry,
	runner *agent.Runner,
	b *bus.Bus,
	v *vault.Vault,
	art artifacts.Store,
	log *slog.Logger,
) *Server {
	return &Server{cfg: cfg, db: db, fleet: fm, models: models, runner: runner,
		bus: b, vault: v, art: art, log: log}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// --- public ---
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/bootstrap", s.handleBootstrap)

	// --- authenticated ---
	auth := func(role string, h http.HandlerFunc) http.Handler {
		return s.requireAuth(role, h)
	}

	mux.Handle("GET /api/me", auth(roleAny, s.handleMe))

	mux.Handle("GET /api/tiers", auth(roleAny, s.handleTiers))
	mux.Handle("GET /api/templates", auth(roleAny, s.handleListTemplates))
	mux.Handle("GET /api/templates/{id}", auth(roleAny, s.handleGetTemplate))
	mux.Handle("GET /api/instances", auth(roleAny, s.handleListInstances))
	mux.Handle("POST /api/instances", auth(roleOperator, s.handleCreateInstance))
	mux.Handle("GET /api/instances/{id}", auth(roleAny, s.handleGetInstance))
	mux.Handle("DELETE /api/instances/{id}", auth(roleOperator, s.handleDeleteInstance))
	mux.Handle("POST /api/instances/{id}/start", auth(roleOperator, s.handleInstanceAction))
	mux.Handle("POST /api/instances/{id}/stop", auth(roleOperator, s.handleInstanceAction))
	mux.Handle("POST /api/instances/{id}/pause", auth(roleOperator, s.handleInstanceAction))
	mux.Handle("POST /api/instances/{id}/resume", auth(roleOperator, s.handleInstanceAction))
	mux.Handle("GET /api/instances/{id}/stats", auth(roleAny, s.handleInstanceStats))
	mux.Handle("POST /api/instances/{id}/act", auth(roleOperator, s.handleManualAct))
	mux.Handle("GET /api/instances/{id}/observe", auth(roleAny, s.handleObserve))

	// Recording studio.
	mux.Handle("POST /api/instances/{id}/record/start", auth(roleOperator, s.handleRecordStart))
	mux.Handle("POST /api/instances/{id}/record/stop", auth(roleOperator, s.handleRecordStop))

	mux.Handle("GET /api/skills", auth(roleAny, s.handleListSkills))
	mux.Handle("POST /api/skills", auth(roleOperator, s.handleUpsertSkill))
	mux.Handle("GET /api/skills/{id}", auth(roleAny, s.handleGetSkill))
	mux.Handle("PUT /api/skills/{id}", auth(roleOperator, s.handleUpsertSkill))
	mux.Handle("DELETE /api/skills/{id}", auth(roleOperator, s.handleDeleteSkill))
	mux.Handle("POST /api/skills/{id}/refine", auth(roleOperator, s.handleRefineSkill))

	mux.Handle("GET /api/tasks", auth(roleAny, s.handleListTasks))
	mux.Handle("POST /api/tasks", auth(roleOperator, s.handleCreateTask))
	mux.Handle("GET /api/tasks/{id}", auth(roleAny, s.handleGetTask))
	mux.Handle("GET /api/tasks/{id}/steps", auth(roleAny, s.handleTaskSteps))
	mux.Handle("POST /api/tasks/{id}/cancel", auth(roleOperator, s.handleCancelTask))
	mux.Handle("POST /api/tasks/{id}/synthesize-skill", auth(roleOperator, s.handleSynthesizeSkill))

	// Autonomous Multi-Agent Swarms & Mission Control
	mux.Handle("GET /api/swarms", auth(roleAny, s.handleListSwarms))
	mux.Handle("POST /api/swarms", auth(roleOperator, s.handleCreateSwarm))
	mux.Handle("GET /api/swarms/{id}", auth(roleAny, s.handleGetSwarm))
	mux.Handle("POST /api/swarms/{id}/messages", auth(roleOperator, s.handlePostSwarmMessage))

	// Event-Driven Webhooks & 24/7 Cron Triggers
	mux.Handle("GET /api/webhooks", auth(roleAdmin, s.handleListWebhooks))
	mux.Handle("POST /api/webhooks", auth(roleAdmin, s.handleCreateWebhook))
	mux.Handle("DELETE /api/webhooks/{id}", auth(roleAdmin, s.handleDeleteWebhook))
	mux.HandleFunc("POST /api/webhooks/{token}", s.handleIncomingWebhook)
	mux.Handle("GET /api/triggers/cron", auth(roleAdmin, s.handleListCronTriggers))
	mux.Handle("POST /api/triggers/cron", auth(roleAdmin, s.handleCreateCronTrigger))
	mux.Handle("DELETE /api/triggers/cron/{id}", auth(roleAdmin, s.handleDeleteCronTrigger))

	// Shared Fleet Vault & Inter-Agent P2P Comms
	mux.Handle("GET /api/vault/secrets", auth(roleAny, s.handleListSharedSecrets))
	mux.Handle("POST /api/vault/secrets", auth(roleOperator, s.handlePutSharedSecret))
	mux.Handle("DELETE /api/vault/secrets/{key}", auth(roleAdmin, s.handleDeleteSharedSecret))
	mux.Handle("GET /api/vault/sessions", auth(roleAny, s.handleListSharedSessions))
	mux.Handle("POST /api/vault/sessions", auth(roleOperator, s.handleSaveSharedSession))
	mux.Handle("GET /api/vault/comms", auth(roleAny, s.handleListPeerMessages))
	mux.Handle("POST /api/vault/comms", auth(roleOperator, s.handleSendPeerMessage))

	// Model Context Protocol (MCP) Bridge
	mux.Handle("GET /api/mcp/servers", auth(roleAny, s.handleListMCPServers))
	mux.Handle("POST /api/mcp/servers", auth(roleAdmin, s.handleRegisterMCPServer))
	mux.Handle("DELETE /api/mcp/servers/{id}", auth(roleAdmin, s.handleDeleteMCPServer))
	mux.Handle("GET /api/mcp/tools", auth(roleAny, s.handleListMCPTools))
	mux.Handle("POST /api/mcp/call", auth(roleOperator, s.handleCallMCPTool))

	// Multi-Bot Workflow DAG Pipelines
	mux.Handle("GET /api/pipelines", auth(roleAny, s.handleListPipelines))
	mux.Handle("POST /api/pipelines", auth(roleOperator, s.handleSavePipeline))
	mux.Handle("GET /api/pipelines/{id}", auth(roleAny, s.handleGetPipeline))
	mux.Handle("DELETE /api/pipelines/{id}", auth(roleAdmin, s.handleDeletePipeline))
	mux.Handle("POST /api/pipelines/{id}/run", auth(roleOperator, s.handleRunPipeline))
	mux.Handle("GET /api/pipelines/{id}/runs", auth(roleAny, s.handleListPipelineRuns))

	// Token & Financial Telemetry Cockpit
	mux.Handle("GET /api/telemetry/financials", auth(roleAny, s.handleGetFinancialSummary))
	mux.Handle("GET /api/telemetry/records", auth(roleAny, s.handleListTelemetryRecords))

	mux.Handle("GET /api/alerts", auth(roleAny, s.handleListAlerts))
	mux.Handle("POST /api/alerts/{id}/reply", auth(roleOperator, s.handleReplyAlert))

	mux.Handle("GET /api/providers", auth(roleAny, s.handleListProviders))
	mux.Handle("POST /api/providers", auth(roleAdmin, s.handleUpsertProvider))
	mux.Handle("PUT /api/providers/{id}", auth(roleAdmin, s.handleUpsertProvider))
	mux.Handle("DELETE /api/providers/{id}", auth(roleAdmin, s.handleDeleteProvider))
	mux.Handle("POST /api/providers/{id}/probe", auth(roleAdmin, s.handleProbeProvider))
	mux.Handle("GET /api/providers/ollama/models", auth(roleAdmin, s.handleOllamaModels))

	mux.Handle("GET /api/secrets", auth(roleAdmin, s.handleListSecrets))
	mux.Handle("PUT /api/secrets/{ref}", auth(roleAdmin, s.handlePutSecret))
	mux.Handle("DELETE /api/secrets/{ref}", auth(roleAdmin, s.handleDeleteSecret))

	mux.Handle("GET /api/users", auth(roleAdmin, s.handleListUsers))
	mux.Handle("POST /api/users", auth(roleAdmin, s.handleCreateUser))
	mux.Handle("PUT /api/users/{id}/role", auth(roleAdmin, s.handleSetRole))

	mux.Handle("GET /api/chat/{instanceID}", auth(roleAny, s.handleChatHistory))
	mux.Handle("POST /api/chat/{instanceID}", auth(roleOperator, s.handleChatSend))

	mux.Handle("POST /api/devices", auth(roleAny, s.handleRegisterDevice))
	mux.Handle("DELETE /api/devices/{token}", auth(roleAny, s.handleUnregisterDevice))

	mux.Handle("GET /api/artifacts/{key...}", auth(roleAny, s.handleArtifact))

	// Live event stream and the authenticated desktop proxy both accept the
	// token as a query parameter, because browsers cannot set headers on a
	// WebSocket handshake or on an <iframe> load.
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("/vnc/{id}/", s.handleVNCProxy)

	return withCORS(withLogging(s.log, mux))
}

// ------------------------------------------------------------------ helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

type apiError struct {
	Error string `json:"error"`
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// failErr maps store/domain errors onto sensible status codes so clients do not
// have to string-match.
func failErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, artifacts.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, connectors.ErrNoProvider):
		fail(w, http.StatusPreconditionFailed, err.Error())
	default:
		fail(w, http.StatusInternalServerError, err.Error())
	}
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

func queryBool(r *http.Request, key string) bool {
	v, _ := strconv.ParseBool(r.URL.Query().Get(key))
	return v
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	subs, dropped := s.bus.Stats()
	live, _ := s.db.CountLiveInstances(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"time":           time.Now().UTC(),
		"live_instances": live,
		"max_instances":  s.cfg.MaxInstances,
		"ws_subscribers": subs,
		"events_dropped": dropped,
	})
}

// ---------------------------------------------------------------- middleware ---

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets the WebSocket upgrader and the VNC proxy reach the underlying
// connection through this wrapper.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Streaming endpoints are long-lived; wrapping them adds nothing but
		// breaks hijacking, so let them through untouched.
		if strings.HasPrefix(r.URL.Path, "/vnc/") || r.URL.Path == "/api/events" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if sw.status >= 400 || time.Since(start) > 2*time.Second {
			log.Info("http", "method", r.Method, "path", r.URL.Path,
				"status", sw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}
