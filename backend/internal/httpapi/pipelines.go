package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/mcp"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/pipeline"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/telemetry"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
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
	res := mcp.GlobalMCP.RegisterServer(r.Context(), srv)
	writeJSON(w, http.StatusCreated, res)
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
	res, err := mcp.GlobalMCP.CallTool(r.Context(), req.ServerID, req.ToolName, req.Params)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
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

// runPipelineNode turns one node into a real task and waits for it.
//
// This is what the pipeline engine was missing: it used to sleep half a second
// per node and record "verified deliverable created" without anything having
// run, then report the pipeline complete. Reusing the trigger dispatcher means
// a node that has nothing to run on is an error the operator sees, exactly as
// it is for a webhook or a cron tick.
func (s *Server) runPipelineNode(ctx context.Context, node protocol.PipelineNode) (string, error) {
	task, err := s.dispatchTrigger(ctx, node.InstanceID, node.ArchetypeID,
		node.GoalTemplate, "pipeline")
	if err != nil {
		return "", err
	}

	// Poll rather than subscribe: a pipeline node is minutes of work, the run
	// is already asynchronous, and a dropped event would hang the whole graph.
	const (
		poll   = 5 * time.Second
		giveUp = 2 * time.Hour
	)
	deadline := time.Now().Add(giveUp)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}

		t, err := s.db.Task(ctx, task.ID)
		if err != nil {
			return "", err
		}
		switch t.State {
		case protocol.TaskSucceeded:
			if t.Result != "" {
				return t.Result, nil
			}
			return "completed", nil
		case protocol.TaskFailed:
			return "", fmt.Errorf("%s", firstNonEmptyStr(t.Error, "the task failed"))
		case protocol.TaskCancelled:
			return "", errors.New("the task was cancelled")
		}
		if time.Now().After(deadline) {
			return "", errors.New("the node did not finish within two hours")
		}
	}
}
