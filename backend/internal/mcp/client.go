package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// ClientManager owns the fleet's MCP connections.
//
// What this replaced: registration invented a single tool called
// "<server>_query" whose schema was {"query":"string"}, ListTools returned that
// invention, and CallTool returned a formatted string claiming it had executed
// something. The transport, command, URL and env fields were stored and never
// read once, registrations died with the process, and no agent could reach any
// of it because `call_mcp` was not in the parser's accepted action set. The MCP
// Hub screen was a working-looking front end over nothing.
//
// Now a registration opens a real connection, completes the MCP handshake, and
// asks the server what tools it has. Connections are kept open — the handshake
// and, for stdio, a process spawn are too expensive to repeat per tool call —
// and are re-established lazily if a server dies.
type ClientManager struct {
	mu      sync.RWMutex
	servers map[string]protocol.MCPServer
	tools   map[string][]protocol.MCPTool
	// live holds the open connection per server. A server can be registered and
	// not connected: its process exited, or it was unreachable at boot. That is
	// reported rather than hidden, and the next call reconnects.
	live map[string]*session
	// lastErr is why a registered server has no connection.
	lastErr map[string]string

	store Store
	log   *slog.Logger
}

// Store is the durable half. An interface rather than *store.Store so the
// manager works with no database, which is how the tests build it.
type Store interface {
	UpsertMCPServer(ctx context.Context, s protocol.MCPServer) error
	ListMCPServers(ctx context.Context) ([]protocol.MCPServer, error)
	DeleteMCPServer(ctx context.Context, id string) error
}

var GlobalMCP = NewClientManager()

func NewClientManager() *ClientManager {
	return &ClientManager{
		servers: make(map[string]protocol.MCPServer),
		tools:   make(map[string][]protocol.MCPTool),
		live:    make(map[string]*session),
		lastErr: make(map[string]string),
	}
}

// AttachStore reloads registered servers and connects to them.
//
// Registrations used to die with the process: an operator configured a server,
// it worked until the next deploy, and then the Hub screen was empty with no
// explanation. Connecting happens in the background because a server that is
// slow or down must not hold up the whole orchestrator's boot.
func (m *ClientManager) AttachStore(ctx context.Context, st Store, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	saved, err := st.ListMCPServers(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.store = st
	m.log = log
	for _, s := range saved {
		if _, live := m.servers[s.ID]; live {
			continue // anything registered since boot is newer than the table
		}
		m.servers[s.ID] = s
		m.tools[s.ID] = []protocol.MCPTool{}
	}
	m.mu.Unlock()

	go func() {
		// Detached: this outlives the boot sequence that started it.
		bg := context.WithoutCancel(ctx)
		for _, s := range saved {
			if !s.Active {
				continue
			}
			connectCtx, cancel := context.WithTimeout(bg, 30*time.Second)
			if _, err := m.ensure(connectCtx, s.ID); err != nil {
				log.Warn("MCP server did not come back after restart",
					"id", s.ID, "name", s.Name, "err", err)
			}
			cancel()
		}
	}()
	return nil
}

// RegisterServer connects to a server, discovers its tools, and stores it.
//
// An error here means the server is not usable, and it is returned rather than
// swallowed: a registration that reports success and produces no tools is the
// behaviour this replaced.
func (m *ClientManager) RegisterServer(ctx context.Context, srv protocol.MCPServer) (protocol.MCPServer, error) {
	if strings.TrimSpace(srv.Name) == "" {
		return srv, errors.New("an MCP server needs a name")
	}

	// Infer the transport when it was not given, from which field was filled
	// in. Getting this wrong used to be silent because nothing read either.
	if normaliseTransport(srv.Transport) == "" {
		if strings.TrimSpace(srv.URL) != "" {
			srv.Transport = "http"
		} else {
			srv.Transport = "stdio"
		}
	}
	switch normaliseTransport(srv.Transport) {
	case "stdio":
		if strings.TrimSpace(srv.Command) == "" {
			return srv, errors.New("a stdio MCP server needs a command")
		}
	case "http":
		if strings.TrimSpace(srv.URL) == "" {
			return srv, errors.New("an http MCP server needs a url")
		}
	default:
		return srv, fmt.Errorf("unknown MCP transport %q: use \"stdio\" or \"http\"", srv.Transport)
	}

	if srv.ID == "" {
		srv.ID = fmt.Sprintf("mcp-%d", time.Now().UnixNano())
		srv.CreatedAt = time.Now().UTC()
	}
	srv.UpdatedAt = time.Now().UTC()
	srv.Active = true

	if normaliseTransport(srv.Transport) == "stdio" {
		m.logger().Warn("registering a stdio MCP server: the orchestrator will execute this command",
			"id", srv.ID, "name", srv.Name, "command", srv.Command)
	}

	// Connect before storing. A row that cannot connect is worse than no row:
	// it shows in the Hub as configured and every agent that reaches for it
	// fails.
	sess, err := connect(ctx, srv)
	if err != nil {
		return srv, err
	}

	tools, err := sess.ListTools(ctx, srv.ID)
	if err != nil {
		_ = sess.Close()
		return srv, fmt.Errorf("connected to %s but could not list its tools: %w", srv.Name, err)
	}
	srv.ToolsCount = len(tools)

	// Adopt the server's own name if the operator did not pick one that says
	// anything. Its self-reported name is generally the useful one.
	if name, version, _ := sess.Info(); name != "" {
		if strings.TrimSpace(srv.Name) == "" {
			srv.Name = name
		}
		m.logger().Info("MCP server connected", "id", srv.ID, "name", srv.Name,
			"server", name, "version", version, "tools", len(tools))
	}

	m.mu.Lock()
	// Replace any previous connection for this id rather than leaking it.
	if old := m.live[srv.ID]; old != nil {
		go old.Close()
	}
	m.servers[srv.ID] = srv
	m.tools[srv.ID] = tools
	m.live[srv.ID] = sess
	delete(m.lastErr, srv.ID)
	st := m.store
	m.mu.Unlock()

	if st != nil {
		if err := st.UpsertMCPServer(ctx, srv); err != nil {
			// Reported, not swallowed: a registration that vanishes on the next
			// deploy is exactly the failure this replaced.
			return srv, fmt.Errorf("connected to %s but could not store it: %w", srv.Name, err)
		}
	}
	return srv, nil
}

// ensure returns a live session, reconnecting if the previous one died.
func (m *ClientManager) ensure(ctx context.Context, serverID string) (*session, error) {
	m.mu.RLock()
	sess, ok := m.live[serverID]
	srv, known := m.servers[serverID]
	m.mu.RUnlock()

	if ok && sess != nil {
		return sess, nil
	}
	if !known {
		return nil, fmt.Errorf("mcp server %q not found", serverID)
	}

	sess, err := connect(ctx, srv)
	if err != nil {
		m.mu.Lock()
		m.lastErr[serverID] = err.Error()
		m.mu.Unlock()
		return nil, err
	}

	tools, err := sess.ListTools(ctx, serverID)
	if err != nil {
		_ = sess.Close()
		m.mu.Lock()
		m.lastErr[serverID] = err.Error()
		m.mu.Unlock()
		return nil, err
	}

	m.mu.Lock()
	if old := m.live[serverID]; old != nil && old != sess {
		go old.Close()
	}
	m.live[serverID] = sess
	m.tools[serverID] = tools
	srv.ToolsCount = len(tools)
	m.servers[serverID] = srv
	delete(m.lastErr, serverID)
	m.mu.Unlock()
	return sess, nil
}

// ServerView is a server plus its live connection state.
//
// The state is the point. A registered server whose process has exited looks
// identical to a working one in the stored row, and the console needs to be
// able to say which it is.
//
// Env is shadowed to nothing. It holds whatever the operator configured, which
// for a hosted MCP server is an API key and for an HTTP one is usually a bearer
// token, and this struct is what /api/mcp/servers serialises — a route open to
// every authenticated role, auditors included. Embedding the row and returning
// it whole handed those credentials to anyone with a login. The key *names* are
// kept because "which variable is missing" is a real diagnostic question.
type ServerView struct {
	protocol.MCPServer
	Env       map[string]string `json:"-"`
	EnvKeys   []string          `json:"env_keys,omitempty"`
	Connected bool              `json:"connected"`
	LastError string            `json:"last_error,omitempty"`
}

func (m *ClientManager) ListServers(ctx context.Context) []ServerView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServerView, 0, len(m.servers))
	for id, s := range m.servers {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		// Blanked on the embedded copy as well: the shadow field above only
		// silences the outer one, and the embedded struct's own `env` tag would
		// still marshal the real map.
		s.Env = nil
		out = append(out, ServerView{
			MCPServer: s,
			EnvKeys:   keys,
			Connected: m.live[id] != nil,
			LastError: m.lastErr[id],
		})
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
	sess := m.live[id]
	delete(m.servers, id)
	delete(m.tools, id)
	delete(m.live, id)
	delete(m.lastErr, id)
	st := m.store
	m.mu.Unlock()

	// Closed, not dropped: a stdio server is a child process, and forgetting the
	// handle leaves it running for the life of the orchestrator.
	if sess != nil {
		_ = sess.Close()
	}
	if st != nil {
		if err := st.DeleteMCPServer(ctx, id); err != nil {
			m.logger().Warn("MCP server removed but still in the database", "id", id, "err", err)
		}
	}
}

func (m *ClientManager) ListTools(ctx context.Context, serverID string) []protocol.MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if serverID != "" {
		out := make([]protocol.MCPTool, 0, len(m.tools[serverID]))
		return append(out, m.tools[serverID]...)
	}
	// Empty rather than nil so the JSON is [] and not null.
	all := make([]protocol.MCPTool, 0)
	for _, tools := range m.tools {
		all = append(all, tools...)
	}
	return all
}

// RefreshTools re-asks a server what it can do.
//
// Servers are allowed to change their catalogue at runtime and to announce it
// with notifications/tools/list_changed. Nothing subscribes to that yet, so this
// is the manual equivalent and the console offers it as a button.
func (m *ClientManager) RefreshTools(ctx context.Context, serverID string) ([]protocol.MCPTool, error) {
	sess, err := m.ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}
	tools, err := sess.ListTools(ctx, serverID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.tools[serverID] = tools
	if srv, ok := m.servers[serverID]; ok {
		srv.ToolsCount = len(tools)
		srv.UpdatedAt = time.Now().UTC()
		m.servers[serverID] = srv
	}
	m.mu.Unlock()
	return tools, nil
}

// FindTool locates a tool by name across every server, so an agent can name a
// tool without also having to know which server provides it.
func (m *ClientManager) FindTool(toolName string) (protocol.MCPTool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, tools := range m.tools {
		for _, t := range tools {
			if t.Name == toolName {
				return t, true
			}
		}
	}
	return protocol.MCPTool{}, false
}

// CallTool executes a tool on a server. This is the real thing now.
func (m *ClientManager) CallTool(ctx context.Context, serverID, toolName string, params map[string]any) (map[string]any, error) {
	if strings.TrimSpace(toolName) == "" {
		return nil, errors.New("a tool name is required")
	}
	// An agent that names only the tool gets the server resolved for it.
	if strings.TrimSpace(serverID) == "" {
		t, ok := m.FindTool(toolName)
		if !ok {
			return nil, fmt.Errorf("no MCP server offers a tool called %q", toolName)
		}
		serverID = t.ServerID
	}

	sess, err := m.ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}

	res, err := sess.CallTool(ctx, toolName, params)
	if err != nil {
		// A tool that ran and failed is a result, not a broken connection.
		// Dropping the session over one would restart a process for nothing.
		if errors.Is(err, ErrToolFailed) {
			return res, err
		}
		// A transport failure means this session is no longer trustworthy.
		// Retiring it makes the next call reconnect instead of failing forever.
		m.mu.Lock()
		if m.live[serverID] == sess {
			delete(m.live, serverID)
			m.lastErr[serverID] = err.Error()
		}
		m.mu.Unlock()
		go sess.Close()
		return nil, err
	}

	m.mu.RLock()
	name := m.servers[serverID].Name
	m.mu.RUnlock()
	res["server"] = name
	res["server_id"] = serverID
	return res, nil
}

// Close shuts every connection down. Called on orchestrator shutdown so stdio
// child processes do not outlive it.
func (m *ClientManager) Close() {
	m.mu.Lock()
	live := m.live
	m.live = make(map[string]*session)
	m.mu.Unlock()

	for _, sess := range live {
		_ = sess.Close()
	}
}

func (m *ClientManager) logger() *slog.Logger {
	m.mu.RLock()
	log := m.log
	m.mu.RUnlock()
	if log == nil {
		return slog.Default()
	}
	return log
}
