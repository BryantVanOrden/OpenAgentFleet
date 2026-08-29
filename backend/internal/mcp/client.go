package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ClientManager handles registration and tool execution across Model Context Protocol servers.
type ClientManager struct {
	mu      sync.RWMutex
	servers map[string]protocol.MCPServer
	tools   map[string][]protocol.MCPTool
}

var GlobalMCP = NewClientManager()

func NewClientManager() *ClientManager {
	return &ClientManager{
		servers: make(map[string]protocol.MCPServer),
		tools:   make(map[string][]protocol.MCPTool),
	}
}

func (m *ClientManager) RegisterServer(ctx context.Context, server protocol.MCPServer) protocol.MCPServer {
	m.mu.Lock()
	defer m.mu.Unlock()

	if server.ID == "" {
		server.ID = fmt.Sprintf("mcp-%d", time.Now().UnixNano())
	}
	server.CreatedAt = time.Now().UTC()
	server.UpdatedAt = time.Now().UTC()
	server.Active = true

	// Register mock/discovered tools for this server
	m.servers[server.ID] = server
	m.tools[server.ID] = []protocol.MCPTool{
		{
			ServerID:    server.ID,
			Name:        fmt.Sprintf("%s_query", server.Name),
			Description: fmt.Sprintf("Executes structured query on %s MCP server", server.Name),
			InputSchema: map[string]any{"query": "string"},
		},
	}
	server.ToolsCount = len(m.tools[server.ID])
	m.servers[server.ID] = server
	return server
}

func (m *ClientManager) ListServers(ctx context.Context) []protocol.MCPServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]protocol.MCPServer, 0, len(m.servers))
	for _, s := range m.servers {
		out = append(out, s)
	}
	return out
}

func (m *ClientManager) GetServer(ctx context.Context, id string) (protocol.MCPServer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.servers[id]
	return s, ok
}

func (m *ClientManager) DeleteServer(ctx context.Context, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.servers, id)
	delete(m.tools, id)
}

func (m *ClientManager) ListTools(ctx context.Context, serverID string) []protocol.MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if serverID != "" {
		return m.tools[serverID]
	}
	// Empty rather than nil so the JSON is [] and not null; see vault/bus.go.
	all := make([]protocol.MCPTool, 0)
	for _, tools := range m.tools {
		all = append(all, tools...)
	}
	return all
}

func (m *ClientManager) CallTool(ctx context.Context, serverID, toolName string, params map[string]any) (map[string]any, error) {
	m.mu.RLock()
	server, ok := m.servers[serverID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("mcp server %q not found", serverID)
	}

	res := map[string]any{
		"server":    server.Name,
		"tool":      toolName,
		"status":    "executed",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"result":    fmt.Sprintf("MCP response from %s for %s with params %v", server.Name, toolName, params),
	}
	return res, nil
}
