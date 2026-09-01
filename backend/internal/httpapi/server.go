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
	"github.com/BryantVanOrden/AgentFleet/backend/internal/telemetry"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
)

type Server struct {
	cfg    *config.Config
	db     *store.Store
	fleet  *fleet.Manager
	models *connectors.Registry
	// relay tracks which agents are collaborating on which request, so
	// finishing a part can wake whoever the next part belongs to.
	relay  *relay
	runner *agent.Runner
	bus    *bus.Bus
	vault  *vault.Vault
	host   *telemetry.HostCollector
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
	return &Server{cfg: cfg, db: db, fleet: fm, models: models, runner: runner, relay: newRelay(),
		bus: b, vault: v, art: art, log: log,
		host: telemetry.NewHostCollector()}
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
	mux.Handle("PUT /api/instances/{id}/access", auth(roleOperator, s.handleSetInstanceAccess))
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
	mux.Handle("POST /api/swarms/{id}/advance", auth(roleOperator, s.handleAdvanceSwarmPhase))
	mux.Handle("DELETE /api/swarms/{id}", auth(roleAdmin, s.handleDeleteSwarm))
	mux.Handle("POST /api/swarms/{id}/artifacts", auth(roleOperator, s.handlePublishArtifact))
	mux.Handle("POST /api/swarms/{id}/artifacts/{artifactId}/review", auth(roleOperator, s.handleReviewArtifact))

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
	// Combinations: which model does what.
	mux.Handle("GET /api/model-combos", auth(roleAdmin, s.handleListModelCombos))
	mux.Handle("POST /api/model-combos", auth(roleAdmin, s.handleUpsertModelCombo))
	mux.Handle("PUT /api/model-combos/{id}", auth(roleAdmin, s.handleUpsertModelCombo))
	mux.Handle("DELETE /api/model-combos/{id}", auth(roleAdmin, s.handleDeleteModelCombo))
	// What each role would actually resolve to for this bot.
	mux.Handle("GET /api/instances/{id}/models/resolved", auth(roleAdmin, s.handleResolveChain))
	// A bot's own model fallback chain.
	mux.Handle("PUT /api/instances/{id}/models", auth(roleAdmin, s.handleSetInstanceModels))
	// What each bot has chosen to remember, and a way to take one back out.
	mux.Handle("GET /api/instances/{id}/memories", auth(roleAny, s.handleListInstanceMemories))
	mux.Handle("DELETE /api/instances/{id}/memories/{memoryId}", auth(roleOperator, s.handleForgetMemory))
	mux.Handle("GET /api/memory/fleet", auth(roleAny, s.handleFleetMemory))
	// Conversations: the named threads comms messages are filed into.
	mux.Handle("GET /api/comms/conversations", auth(roleAny, s.handleListConversations))
	mux.Handle("POST /api/comms/conversations", auth(roleOperator, s.handleCreateConversation))
	mux.Handle("PATCH /api/comms/conversations/{id}", auth(roleOperator, s.handleUpdateConversation))
	mux.Handle("DELETE /api/comms/conversations/{id}", auth(roleOperator, s.handleDeleteConversation))
	mux.Handle("GET /api/comms/conversations/{id}/messages", auth(roleAny, s.handleListConversationMessages))
	mux.Handle("POST /api/comms/conversations/{id}/compact", auth(roleOperator, s.handleCompactConversation))

	// Model Context Protocol (MCP) Bridge
	mux.Handle("GET /api/mcp/servers", auth(roleAny, s.handleListMCPServers))
	mux.Handle("POST /api/mcp/servers", auth(roleAdmin, s.handleRegisterMCPServer))
	mux.Handle("DELETE /api/mcp/servers/{id}", auth(roleAdmin, s.handleDeleteMCPServer))
	mux.Handle("GET /api/mcp/tools", auth(roleAny, s.handleListMCPTools))
	// Archetype packaging. Export is readable by anyone who can see the fleet;
	// import creates skills, registers MCP servers and can provision a bot, so
	// it is an admin action.
	mux.Handle("GET /api/archetypes/{id}/export", auth(roleAny, s.handleExportArchetype))
	mux.Handle("POST /api/archetypes/import", auth(roleAdmin, s.handleImportArchetype))
	mux.Handle("POST /api/mcp/servers/{id}/refresh", auth(roleAdmin, s.handleRefreshMCPTools))
	mux.Handle("GET /api/mcp/resources", auth(roleAny, s.handleListMCPResources))
	mux.Handle("POST /api/mcp/resources/read", auth(roleOperator, s.handleReadMCPResource))
	mux.Handle("GET /api/mcp/prompts", auth(roleAny, s.handleListMCPPrompts))
	mux.Handle("POST /api/mcp/prompts/get", auth(roleOperator, s.handleGetMCPPrompt))
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
	// Live resource usage of the machine running the orchestrator. The existing
	// telemetry endpoints are LLM token accounting; per-instance CPU/memory
	// comes from Docker. Neither answers "is the host itself saturated".
	mux.Handle("GET /api/telemetry/host", auth(roleAny, s.handleHostStats))

	// Text to speech, proxied to the tts sidecar.
	mux.Handle("GET /api/voice/voices", auth(roleAny, s.handleListVoices))
	mux.Handle("POST /api/voice/speak", auth(roleAny, s.handleSpeak))
	mux.Handle("GET /api/telemetry/records", auth(roleAny, s.handleListTelemetryRecords))

	mux.Handle("GET /api/alerts", auth(roleAny, s.handleListAlerts))
	mux.Handle("POST /api/alerts/{id}/reply", auth(roleOperator, s.handleReplyAlert))

	// Provider connections, model combinations and per-bot fallback chains are
	// administration, not use. They carry base URLs, OAuth client ids and
	// sign-in state, and editing a chain changes what every bot on it costs and
	// which model sees its screen -- none of which belongs to an operator who
	// was given a bot to drive. The UI hides these too, but the gate is here.
	mux.Handle("GET /api/providers", auth(roleAdmin, s.handleListProviders))
	mux.Handle("POST /api/providers", auth(roleAdmin, s.handleUpsertProvider))
	mux.Handle("PUT /api/providers/{id}", auth(roleAdmin, s.handleUpsertProvider))
	mux.Handle("DELETE /api/providers/{id}", auth(roleAdmin, s.handleDeleteProvider))
	mux.Handle("POST /api/providers/reorder", auth(roleAdmin, s.handleReorderProviders))
	mux.Handle("POST /api/providers/{id}/probe", auth(roleAdmin, s.handleProbeProvider))
	// Signing in with an account rather than pasting a key.
	mux.Handle("POST /api/providers/{id}/signin", auth(roleAdmin, s.handleStartProviderSignIn))
	// In-app sign-in: the app loads the consent page in a webview and the
	// provider redirects back here.
	mux.Handle("POST /api/providers/{id}/signin/url", auth(roleAdmin, s.handleStartAuthCodeSignIn))
	mux.Handle("GET /api/providers/signin/status", auth(roleAdmin, s.handleAuthCodeStatus))
	mux.Handle("GET /api/providers/oauth/redirect", auth(roleAdmin, s.handleOAuthRedirectURI))
	// Unauthenticated: this receives the provider's redirect, which carries no
	// operator session. The unguessable single-use state is what authorises it.
	mux.HandleFunc("GET "+oauthCallbackPath, s.handleOAuthCallback)
	mux.Handle("GET /api/providers/{id}/signin", auth(roleAdmin, s.handleProviderSignInStatus))
	mux.Handle("DELETE /api/providers/{id}/signin", auth(roleAdmin, s.handleProviderSignOut))
	// Model discovery for any provider kind. The two routes below it predate
	// this one and are kept so existing clients keep working; new code should
	// use this, which covers every kind rather than two special cases.
	//
	// roleAdmin, not roleAny: the caller passes an API key as a query parameter
	// for an unsaved provider, and this is the only place in the API where a
	// credential arrives that way. Only admins configure engines.
	mux.Handle("GET /api/providers/models", auth(roleAdmin, s.handleDynamicModels))
	mux.Handle("GET /api/providers/ollama/models", auth(roleAdmin, s.handleOllamaModels))
	mux.Handle("GET /api/providers/antigravity/models", auth(roleAdmin, s.handleAntigravityModels))

	mux.Handle("GET /api/secrets", auth(roleAdmin, s.handleListSecrets))
	mux.Handle("PUT /api/secrets/{ref}", auth(roleAdmin, s.handlePutSecret))
	mux.Handle("DELETE /api/secrets/{ref}", auth(roleAdmin, s.handleDeleteSecret))

	// Organisations and departments. Gated at roleOperator rather than
	// roleAdmin: an org owner is not necessarily a deployment administrator,
	// and the per-org checks inside each handler are what actually decide.
	mux.Handle("GET /api/orgs", auth(roleAny, s.handleListOrgs))
	mux.Handle("POST /api/orgs", auth(roleOperator, s.handleUpsertOrg))
	mux.Handle("PUT /api/orgs/{id}", auth(roleOperator, s.handleUpsertOrg))
	mux.Handle("DELETE /api/orgs/{id}", auth(roleOperator, s.handleDeleteOrg))
	mux.Handle("GET /api/orgs/{id}/members", auth(roleAny, s.handleListOrgMembers))
	mux.Handle("POST /api/orgs/{id}/members", auth(roleOperator, s.handleUpsertOrgMember))
	mux.Handle("DELETE /api/orgs/{id}/members/{userID}", auth(roleOperator, s.handleRemoveOrgMember))
	// Which department a bot belongs to, and per-bot exceptions.
	mux.Handle("PUT /api/instances/{id}/org", auth(roleOperator, s.handleSetInstanceOrg))
	mux.Handle("GET /api/instances/{id}/grants", auth(roleOperator, s.handleListBotGrants))
	mux.Handle("PUT /api/instances/{id}/grants", auth(roleOperator, s.handleSetBotGrant))
	// What the caller may do, so the app can hide what it must.
	mux.Handle("GET /api/me/permissions", auth(roleAny, s.handleMyPermissions))
	mux.Handle("GET /api/users", auth(roleAdmin, s.handleListUsers))
	mux.Handle("POST /api/users", auth(roleAdmin, s.handleCreateUser))
	mux.Handle("PUT /api/users/{id}/role", auth(roleAdmin, s.handleSetRole))
	// Resetting a password is the only way back in on a deployment with no
	// mail server to send a reset link through.
	mux.Handle("PUT /api/users/{id}/password", auth(roleAdmin, s.handleSetUserPassword))
	// Disable rather than delete: deleting cascades a person's keys away and
	// orphans what they made.
	mux.Handle("PUT /api/users/{id}/disabled", auth(roleAdmin, s.handleSetUserDisabled))

	// The shared work catalog: what the agents have made, for each other and
	// for you. Reading is open to anyone who may read the department it is
	// filed under; publishing and deleting are checked per item.
	mux.Handle("GET /api/work", auth(roleAny, s.handleListWork))
	mux.Handle("GET /api/work/{id}", auth(roleAny, s.handleGetWork))
	mux.Handle("POST /api/work", auth(roleOperator, s.handlePutWork))
	mux.Handle("PATCH /api/work/{id}", auth(roleOperator, s.handleMoveWork))
	mux.Handle("DELETE /api/work/{id}", auth(roleOperator, s.handleDeleteWork))

	// Long-lived access keys for scripts and CI.
	mux.Handle("GET /api/api-keys", auth(roleAdmin, s.handleListAPIKeys))
	mux.Handle("POST /api/api-keys", auth(roleAdmin, s.handleCreateAPIKey))
	mux.Handle("DELETE /api/api-keys/{id}", auth(roleAdmin, s.handleRevokeAPIKey))

	mux.Handle("GET /api/chat/{instanceID}", auth(roleAny, s.handleChatHistory))
	mux.Handle("POST /api/chat/{instanceID}", auth(roleOperator, s.handleChatSend))
	// Separate chats with one bot: list, start, rename/pin, delete.
	mux.Handle("GET /api/chat/{instanceID}/chats", auth(roleAny, s.handleListChatSessions))
	mux.Handle("POST /api/chat/{instanceID}/chats", auth(roleOperator, s.handleCreateChatSession))
	mux.Handle("PATCH /api/chat/{instanceID}/chats/{chatID}", auth(roleOperator, s.handleUpdateChatSession))
	mux.Handle("DELETE /api/chat/{instanceID}/chats/{chatID}", auth(roleOperator, s.handleDeleteChatSession))
	// A plan proposed in chat becomes a task only when the operator says so.
	mux.Handle("POST /api/chat/{instanceID}/plans/{planID}/approve", auth(roleOperator, s.handleApprovePlan))
	mux.Handle("POST /api/chat/{instanceID}/plans/{planID}/discard", auth(roleOperator, s.handleDiscardPlan))

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
	case errors.Is(err, fleet.ErrInvalidRequest):
		// The caller asked for something malformed. Answering 500 would make
		// their mistake look like ours, and hide it from them.
		fail(w, http.StatusBadRequest, err.Error())
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

// handleHostStats reports live CPU, memory, disk and GPU for the host.
//
// Sampling is cheap (a few /proc reads plus an optional nvidia-smi), so it is
// taken per request rather than cached: a dashboard polling every few seconds
// wants the current value, and a stale one is worse than a slightly costlier
// read.
func (s *Server) handleHostStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.host.Sample(r.Context()))
}
