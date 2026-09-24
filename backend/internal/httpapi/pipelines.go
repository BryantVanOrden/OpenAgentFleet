package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/mcp"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/pipeline"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/telemetry"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// ----------------------------------------------------------------- MCP Servers ---

func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mcp.GlobalMCP.ListServers(r.Context()))
}

func (s *Server) handleRegisterMCPServer(w http.ResponseWriter, r *http.Request) {
	var srv protocol.MCPServer
	if err := json.NewDecoder(r.Body).Decode(&srv); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Bounded: registering a server now opens a connection, runs the MCP
	// handshake and lists tools, and for stdio spawns a process. A server that
	// never answers must not hold the request open indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	res, err := mcp.GlobalMCP.RegisterServer(ctx, srv)
	if err != nil {
		// 400, not 500: the server is unreachable or misconfigured, which is
		// something the operator can fix and needs to be told about. Registering
		// used to always report success and produce one invented tool.
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, redactMCP(res))
}

// redactMCP keeps the env map out of the API response.
//
// It holds whatever the operator put there, which for a hosted MCP server is an
// API key and for an HTTP one is usually a bearer token. The previous handler
// echoed the whole struct straight back.
func redactMCP(srv protocol.MCPServer) map[string]any {
	keys := make([]string, 0, len(srv.Env))
	for k := range srv.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return map[string]any{
		"id": srv.ID, "name": srv.Name, "transport": srv.Transport,
		"command": srv.Command, "args": srv.Args, "url": srv.URL,
		"tools_count": srv.ToolsCount, "active": srv.Active,
		"created_at": srv.CreatedAt, "updated_at": srv.UpdatedAt,
		// The names are useful for diagnosing a missing variable; the values
		// are not the console's business.
		"env_keys": keys,
	}
}

// handleListMCPResources serves the cached resource catalogue.
func (s *Server) handleListMCPResources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mcp.GlobalMCP.ListResources(r.Context(), r.URL.Query().Get("server_id")))
}

// handleListMCPPrompts serves the cached prompt catalogue.
func (s *Server) handleListMCPPrompts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mcp.GlobalMCP.ListPrompts(r.Context(), r.URL.Query().Get("server_id")))
}

type readMCPResourceReq struct {
	ServerID string `json:"server_id"`
	URI      string `json:"uri"`
}

// handleReadMCPResource fetches one resource's content from its server.
func (s *Server) handleReadMCPResource(w http.ResponseWriter, r *http.Request) {
	var req readMCPResourceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := mcp.GlobalMCP.ReadResource(ctx, req.ServerID, req.URI)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type getMCPPromptReq struct {
	ServerID  string            `json:"server_id"`
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// handleGetMCPPrompt renders one prompt template with arguments.
func (s *Server) handleGetMCPPrompt(w http.ResponseWriter, r *http.Request) {
	var req getMCPPromptReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := mcp.GlobalMCP.GetPrompt(ctx, req.ServerID, req.Name, req.Arguments)
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleRefreshMCPTools re-asks a server what tools it has.
//
// Servers may change their catalogue at runtime. Nothing subscribes to
// notifications/tools/list_changed yet, so this is the manual equivalent.
func (s *Server) handleRefreshMCPTools(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	tools, err := mcp.GlobalMCP.RefreshTools(ctx, r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools, "count": len(tools)})
}

func (s *Server) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mcp.GlobalMCP.DeleteServer(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMCPTools(w http.ResponseWriter, r *http.Request) {
	srvID := r.URL.Query().Get("server_id")
	writeJSON(w, http.StatusOK, mcp.GlobalMCP.ListTools(r.Context(), srvID))
}

type CallMCPToolReq struct {
	ServerID string         `json:"server_id"`
	ToolName string         `json:"tool_name"`
	Params   map[string]any `json:"params"`
}

func (s *Server) handleCallMCPTool(w http.ResponseWriter, r *http.Request) {
	var req CallMCPToolReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	res, err := mcp.GlobalMCP.CallTool(ctx, req.ServerID, req.ToolName, req.Params)
	if err != nil {
		// A tool that ran and reported failure is a 200 carrying is_error, not a
		// transport problem: the caller asked a question and got an answer.
		if errors.Is(err, mcp.ErrToolFailed) && res != nil {
			writeJSON(w, http.StatusOK, res)
			return
		}
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ----------------------------------------------------------- Workflow Pipelines ---

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pipeline.GlobalEngine.ListPipelines(r.Context()))
}

func (s *Server) handleSavePipeline(w http.ResponseWriter, r *http.Request) {
	var p protocol.WorkflowPipeline
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	res, err := pipeline.GlobalEngine.SavePipeline(r.Context(), p)
	if err != nil {
		// A pipeline is written once and run on a schedule, so an unrunnable
		// graph should fail in front of whoever is writing it rather than at
		// 3am on its first tick.
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := pipeline.GlobalEngine.GetPipeline(r.Context(), id)
	if !ok {
		fail(w, http.StatusNotFound, "pipeline not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pipeline.GlobalEngine.DeletePipeline(r.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunPipeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := pipeline.GlobalEngine.TriggerRun(r.Context(), id)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeJSON(w, http.StatusOK, pipeline.GlobalEngine.ListRuns(r.Context(), id))
}

// --------------------------------------------------------- Financial Telemetry ---

func (s *Server) handleGetFinancialSummary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, telemetry.GlobalTracker.GetSummary(r.Context()))
}

func (s *Server) handleListTelemetryRecords(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}
	writeJSON(w, http.StatusOK, telemetry.GlobalTracker.ListRecords(r.Context(), limit))
}

