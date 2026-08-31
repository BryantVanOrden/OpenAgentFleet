package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// One live MCP connection: the handshake, the tool catalogue and tool calls.
//
// The handshake is not optional. A server that has not been initialised is
// entitled to reject tools/list, and several do, so the previous "register and
// list" flow could not have worked even with a transport under it.

// protocolVersion is the revision this client implements. Sent at initialize;
// the server answers with the version it will actually use.
const protocolVersion = "2025-06-18"

type session struct {
	transport transport

	mu sync.RWMutex
	// serverInfo is what the server called itself, for the console.
	serverName    string
	serverVersion string
	negotiated    string
	initialised   bool
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
	Capabilities struct {
		Tools *struct {
			ListChanged bool `json:"listChanged,omitempty"`
		} `json:"tools,omitempty"`
	} `json:"capabilities"`
}

// connect opens a transport for a server row and completes the handshake.
func connect(ctx context.Context, srv protocol.MCPServer) (*session, error) {
	var tr transport
	var err error

	switch normaliseTransport(srv.Transport) {
	case "stdio":
		tr, err = newStdioTransport(ctx, srv.Command, srv.Args, srv.Env)
	case "http":
		// Env doubles as extra headers for an HTTP server, which is how an
		// operator supplies a bearer token without a separate field.
		tr, err = newHTTPTransport(srv.URL, srv.Env)
	default:
		return nil, fmt.Errorf("unknown MCP transport %q: use \"stdio\" or \"http\"", srv.Transport)
	}
	if err != nil {
		return nil, err
	}

	s := &session{transport: tr}
	if err := s.initialize(ctx); err != nil {
		_ = tr.Close()
		return nil, err
	}
	return s, nil
}

// normaliseTransport maps the stored spellings onto the two real ones.
//
// "sse" is the older name for HTTP transport and appears in rows created before
// the rename, so it has to keep working.
func normaliseTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "stdio", "process", "command":
		if t == "" {
			return "" // caller decides; see Manager.RegisterServer
		}
		return "stdio"
	case "http", "sse", "streamable-http", "streamable_http", "https":
		return "http"
	default:
		return strings.ToLower(strings.TrimSpace(t))
	}
}

func (s *session) initialize(ctx context.Context) error {
	raw, err := s.transport.Call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		// Declared honestly: this client consumes tools and nothing else. A
		// server that sees sampling or roots advertised may try to use them.
		"capabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name":    "agentfleet",
			"version": "1",
		},
	})
	if err != nil {
		return fmt.Errorf("MCP handshake failed: %w", err)
	}

	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("MCP server sent an unreadable initialize result: %w", err)
	}

	s.mu.Lock()
	s.serverName = res.ServerInfo.Name
	s.serverVersion = res.ServerInfo.Version
	s.negotiated = res.ProtocolVersion
	s.initialised = true
	s.mu.Unlock()

	// Later requests carry the negotiated version.
	if h, ok := s.transport.(*httpTransport); ok && res.ProtocolVersion != "" {
		h.SetProtocolVersion(res.ProtocolVersion)
	}

	// Required by the spec, and not merely ceremony: a server may hold every
	// request until it arrives, so skipping it looks exactly like a hang.
	if err := s.transport.Notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("MCP initialized notification failed: %w", err)
	}
	return nil
}

type toolsListResult struct {
	Tools []struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
	} `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListTools fetches the real catalogue, following pagination.
func (s *session) ListTools(ctx context.Context, serverID string) ([]protocol.MCPTool, error) {
	out := make([]protocol.MCPTool, 0, 8)
	cursor := ""

	// Bounded: a server with a broken cursor that always returns the same one
	// would otherwise spin here forever.
	for page := 0; page < 50; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := s.transport.Call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var res toolsListResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("MCP server sent an unreadable tool list: %w", err)
		}
		for _, t := range res.Tools {
			out = append(out, protocol.MCPTool{
				ServerID:    serverID,
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
		if res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

// toolCallResult is what tools/call returns.
//
// Content is a list of typed blocks — text, image, audio, embedded resource —
// because a tool is allowed to return more than a string.
type toolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		// Non-text blocks. Kept so a call that returns an image is reported as
		// having returned an image rather than as having returned nothing.
		MimeType string          `json:"mimeType,omitempty"`
		Data     string          `json:"data,omitempty"`
		Resource json.RawMessage `json:"resource,omitempty"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	// IsError is a *tool* failure, as opposed to a protocol failure. The call
	// succeeded and the tool reported a problem; the distinction matters because
	// only one of the two is worth retrying.
	IsError bool `json:"isError,omitempty"`
}

// ErrToolFailed marks a tool that ran and reported failure.
var ErrToolFailed = errors.New("the MCP tool reported an error")

// CallTool invokes one tool and flattens the result.
func (s *session) CallTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		// Omitting arguments entirely makes strict servers reject the call for a
		// schema violation, so an empty object is sent instead of null.
		args = map[string]any{}
	}
	raw, err := s.transport.Call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}

	var res toolCallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("MCP server sent an unreadable tool result: %w", err)
	}

	// Flattened to text because that is what an agent's next prompt can carry.
	// Non-text blocks are described rather than dropped, so a tool returning an
	// image does not read as a tool returning nothing.
	var parts []string
	for _, c := range res.Content {
		switch {
		case c.Type == "text" || c.Text != "":
			parts = append(parts, c.Text)
		case c.Type == "resource" && len(c.Resource) > 0:
			parts = append(parts, "[embedded resource: "+clip(string(c.Resource), 400)+"]")
		case c.Data != "":
			parts = append(parts, fmt.Sprintf("[%s content, %d bytes, not shown]",
				firstNonEmpty(c.MimeType, c.Type), len(c.Data)))
		default:
			parts = append(parts, "["+firstNonEmpty(c.Type, "unknown")+" content]")
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" && len(res.StructuredContent) > 0 {
		text = string(res.StructuredContent)
	}

	out := map[string]any{
		"tool":     name,
		"text":     text,
		"is_error": res.IsError,
	}
	if len(res.StructuredContent) > 0 {
		var structured any
		if json.Unmarshal(res.StructuredContent, &structured) == nil {
			out["structured"] = structured
		}
	}
	if res.IsError {
		return out, fmt.Errorf("%w: %s", ErrToolFailed, clip(text, 300))
	}
	return out, nil
}

func (s *session) Info() (name, version, negotiated string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serverName, s.serverVersion, s.negotiated
}

func (s *session) Close() error { return s.transport.Close() }

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
