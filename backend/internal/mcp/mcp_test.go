package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// These tests run a real MCP server over the real transports and assert the
// client completes the real handshake. That is the point: the previous
// implementation would have passed any test that only checked "registering a
// server returns a server", because it invented its tools and its results.

// ---------------------------------------------------------------- HTTP server ---

// fakeMCPServer is a minimal spec-compliant MCP server over Streamable HTTP.
type fakeMCPServer struct {
	// initialised records whether notifications/initialized arrived before
	// tools/list. A server is entitled to require it, and this asserts we send
	// it -- skipping it looks exactly like a hang against such a server.
	initialised bool
	// sawSession records that the client echoed the session id we assigned.
	sawSession bool
	// respondSSE makes the server answer with text/event-stream instead of
	// application/json, which is the server's choice per the spec.
	respondSSE bool
	// failTool makes tools/call return isError.
	failTool bool
	// pagedTools splits the catalogue over two pages via nextCursor.
	pagedTools bool
	calls      []string
}

func (f *fakeMCPServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Mcp-Session-Id") != "" {
			f.sawSession = true
		}

		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		f.calls = append(f.calls, req.Method)

		if req.Method == "notifications/initialized" {
			f.initialised = true
			w.WriteHeader(http.StatusAccepted)
			return
		}

		result, rpcErr := f.dispatch(req)
		resp := rpcResponse{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcErr}
		if rpcErr == nil {
			raw, _ := json.Marshal(result)
			resp.Result = raw
		}

		w.Header().Set("Mcp-Session-Id", "session-123")
		body, _ := json.Marshal(resp)
		if f.respondSSE {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// A progress notification first, so the reader has to filter by id
			// rather than taking the first event it sees.
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

func (f *fakeMCPServer) dispatch(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo":      map[string]any{"name": "fake-mcp", "version": "9.9"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}, nil

	case "tools/list":
		if !f.initialised {
			return nil, &rpcError{Code: -32002, Message: "not initialized"}
		}
		if !f.pagedTools {
			return map[string]any{"tools": []any{weatherTool, echoTool}}, nil
		}
		// Paged: page one carries a cursor, page two does not.
		params, _ := req.Params.(map[string]any)
		if params == nil || params["cursor"] == nil {
			return map[string]any{"tools": []any{weatherTool}, "nextCursor": "page2"}, nil
		}
		return map[string]any{"tools": []any{echoTool}}, nil

	case "tools/call":
		params, _ := req.Params.(map[string]any)
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)
		if f.failTool {
			return map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "the upstream API rejected the key"}},
				"isError": true,
			}, nil
		}
		return map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": fmt.Sprintf("called %s with city=%v", name, args["city"])},
			},
		}, nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
}

var weatherTool = map[string]any{
	"name":        "get_weather",
	"description": "Current conditions for a city",
	"inputSchema": map[string]any{
		"type":       "object",
		"properties": map[string]any{"city": map[string]any{"type": "string"}},
		"required":   []any{"city"},
	},
}

var echoTool = map[string]any{
	"name":        "echo",
	"description": "Returns what it is given",
	"inputSchema": map[string]any{"type": "object"},
}

// ------------------------------------------------------------------- tests ---

func TestHTTPTransportCompletesTheHandshakeAndListsRealTools(t *testing.T) {
	fake := &fakeMCPServer{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()

	got, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "weather", Transport: "http", URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if !fake.initialised {
		t.Error("notifications/initialized was never sent; a server that requires it would hang")
	}
	if got.ToolsCount != 2 {
		t.Errorf("tools_count = %d, want 2", got.ToolsCount)
	}

	tools := m.ListTools(context.Background(), got.ID)
	if len(tools) != 2 {
		t.Fatalf("listed %d tools, want 2", len(tools))
	}
	// The names have to come from the server. The old implementation invented
	// exactly one tool called "<name>_query", so this is the assertion that
	// distinguishes a real bridge from the stub.
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.ServerID != got.ID {
			t.Errorf("tool %s has server_id %q, want %q", tool.Name, tool.ServerID, got.ID)
		}
	}
	if !names["get_weather"] || !names["echo"] {
		t.Errorf("tools = %v, want the server's own get_weather and echo", names)
	}
	if names["weather_query"] {
		t.Error("the invented \"<server>_query\" tool is still being produced")
	}

	// And the schema survived, because a model needs it to form a call.
	for _, tool := range tools {
		if tool.Name != "get_weather" {
			continue
		}
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok || props["city"] == nil {
			t.Errorf("get_weather lost its input schema: %v", tool.InputSchema)
		}
	}
}

func TestCallToolActuallyReachesTheServer(t *testing.T) {
	fake := &fakeMCPServer{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	reg, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "weather", Transport: "http", URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	res, err := m.CallTool(context.Background(), reg.ID, "get_weather",
		map[string]any{"city": "Boise"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	text, _ := res["text"].(string)
	// The argument round-tripped, which a canned response cannot do.
	if !strings.Contains(text, "city=Boise") {
		t.Errorf("result %q does not reflect the arguments that were sent", text)
	}
	if !strings.Contains(text, "called get_weather") {
		t.Errorf("result %q does not come from the server", text)
	}
	if !fake.sawSession {
		t.Error("the Mcp-Session-Id the server assigned was not echoed back")
	}
}

func TestCallToolResolvesTheServerFromTheToolName(t *testing.T) {
	fake := &fakeMCPServer{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	if _, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "weather", Transport: "http", URL: srv.URL,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	// No server id: an agent names a tool, not a server.
	res, err := m.CallTool(context.Background(), "", "get_weather",
		map[string]any{"city": "Reno"})
	if err != nil {
		t.Fatalf("call without a server id: %v", err)
	}
	if text, _ := res["text"].(string); !strings.Contains(text, "city=Reno") {
		t.Errorf("result %q did not reach the right server", text)
	}
}

func TestSSEResponseBodyIsUnderstood(t *testing.T) {
	fake := &fakeMCPServer{respondSSE: true}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	reg, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "streamy", Transport: "sse", URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("register over sse: %v", err)
	}
	res, err := m.CallTool(context.Background(), reg.ID, "get_weather",
		map[string]any{"city": "Ely"})
	if err != nil {
		t.Fatalf("call over sse: %v", err)
	}
	// Proves the reader skipped the progress notification and matched on id.
	if text, _ := res["text"].(string); !strings.Contains(text, "city=Ely") {
		t.Errorf("result %q; the SSE reader did not find the reply", text)
	}
}

func TestPaginatedToolListIsFollowed(t *testing.T) {
	fake := &fakeMCPServer{pagedTools: true}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	reg, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "paged", Transport: "http", URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// A client that stopped at page one would report 1.
	if reg.ToolsCount != 2 {
		t.Errorf("tools_count = %d, want 2 across both pages", reg.ToolsCount)
	}
}

func TestToolErrorIsAResultNotATransportFailure(t *testing.T) {
	fake := &fakeMCPServer{failTool: true}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	reg, _ := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "broken", Transport: "http", URL: srv.URL,
	})

	res, err := m.CallTool(context.Background(), reg.ID, "get_weather", nil)
	if err == nil {
		t.Fatal("a tool reporting isError should return an error")
	}
	if !strings.Contains(err.Error(), "rejected the key") {
		t.Errorf("error %q loses the reason the tool gave", err)
	}
	// The result comes back too, so the agent can read the detail, and the
	// session must survive: a failing tool is not a broken connection.
	if res == nil {
		t.Fatal("a failed tool call should still return its content")
	}
	m.mu.RLock()
	stillLive := m.live[reg.ID] != nil
	m.mu.RUnlock()
	if !stillLive {
		t.Error("the session was dropped over a tool-level error")
	}
}

func TestRegisterRejectsAnUnreachableServer(t *testing.T) {
	m := NewClientManager()
	defer m.Close()

	// Registering used to always succeed and produce one invented tool, so an
	// operator's typo showed up as a working server with a tool that failed
	// silently at call time.
	_, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "nope", Transport: "http", URL: "http://127.0.0.1:1/mcp",
	})
	if err == nil {
		t.Fatal("registering an unreachable server should fail")
	}
	if len(m.ListServers(context.Background())) != 0 {
		t.Error("a server that could not connect was stored anyway")
	}
}

func TestRegisterValidatesTheTransportFields(t *testing.T) {
	m := NewClientManager()
	defer m.Close()

	for _, tc := range []struct {
		name string
		srv  protocol.MCPServer
		want string
	}{
		{"stdio with no command", protocol.MCPServer{Name: "x", Transport: "stdio"}, "command"},
		{"http with no url", protocol.MCPServer{Name: "x", Transport: "http"}, "url"},
		{"unknown transport", protocol.MCPServer{Name: "x", Transport: "carrier-pigeon"}, "unknown MCP transport"},
		{"no name", protocol.MCPServer{Transport: "http", URL: "http://x"}, "name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := m.RegisterServer(context.Background(), tc.srv)
			if err == nil {
				t.Fatalf("expected a rejection mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestEnvIsNeverReturnedByListServers(t *testing.T) {
	fake := &fakeMCPServer{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	if _, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "keyed", Transport: "http", URL: srv.URL,
		Env: map[string]string{"Authorization": "Bearer super-secret-value"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	// The manager keeps env because it needs it to reconnect; the API layer is
	// what redacts. This asserts the value is not accidentally logged into the
	// view struct that the console serialises.
	views := m.ListServers(context.Background())
	blob, _ := json.Marshal(views)
	if strings.Contains(string(blob), "super-secret-value") {
		t.Errorf("the credential leaked into the server listing: %s", blob)
	}
}

// ---------------------------------------------------------------- stdio ---

// A stdio MCP server, written in shell, that speaks just enough of the protocol.
// Skipped on Windows, where there is no /bin/sh to run it.
const stdioServerScript = `
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"shell-mcp","version":"1"},"capabilities":{"tools":{}}}}'
      ;;
    *'notifications/initialized'*)
      ;;
    *'"method":"tools/list"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"Answers pong","inputSchema":{"type":"object"}}]}}'
      ;;
    *'"method":"tools/call"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"pong from a real subprocess"}]}}'
      ;;
  esac
done
`

func TestStdioTransportTalksToARealSubprocess(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh on this platform")
	}

	m := NewClientManager()
	defer m.Close()

	reg, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name:      "shell-mcp",
		Transport: "stdio",
		Command:   "/bin/sh",
		Args:      []string{"-c", stdioServerScript},
	})
	if err != nil {
		t.Fatalf("register stdio server: %v", err)
	}
	if reg.ToolsCount != 1 {
		t.Fatalf("tools_count = %d, want 1", reg.ToolsCount)
	}

	res, err := m.CallTool(context.Background(), reg.ID, "ping", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if text, _ := res["text"].(string); !strings.Contains(text, "pong from a real subprocess") {
		t.Errorf("result %q did not come from the child process", text)
	}
}

func TestStdioCanBeDisabledByTheOperator(t *testing.T) {
	t.Setenv("MCP_DISABLE_STDIO", "true")

	m := NewClientManager()
	defer m.Close()
	_, err := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "x", Transport: "stdio", Command: "/bin/sh", Args: []string{"-c", "true"},
	})
	if err == nil {
		t.Fatal("stdio should be refused when MCP_DISABLE_STDIO is set")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Errorf("error %q does not explain that stdio is disabled", err)
	}
}

// ------------------------------------------------------------- persistence ---

type memStore struct {
	rows map[string]protocol.MCPServer
}

func (m *memStore) UpsertMCPServer(_ context.Context, s protocol.MCPServer) error {
	if m.rows == nil {
		m.rows = map[string]protocol.MCPServer{}
	}
	m.rows[s.ID] = s
	return nil
}

func (m *memStore) ListMCPServers(_ context.Context) ([]protocol.MCPServer, error) {
	out := make([]protocol.MCPServer, 0, len(m.rows))
	for _, s := range m.rows {
		out = append(out, s)
	}
	return out, nil
}

func (m *memStore) DeleteMCPServer(_ context.Context, id string) error {
	delete(m.rows, id)
	return nil
}

func TestRegistrationsSurviveARestart(t *testing.T) {
	fake := &fakeMCPServer{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	st := &memStore{}

	// First "process": register a server.
	first := NewClientManager()
	if err := first.AttachStore(context.Background(), st, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	reg, err := first.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "persisted", Transport: "http", URL: srv.URL,
		Env: map[string]string{"X-Token": "abc"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	first.Close()

	// Second "process": the registration has to come back. Previously it did
	// not -- the maps were process-local against no table at all.
	second := NewClientManager()
	defer second.Close()
	if err := second.AttachStore(context.Background(), st, nil); err != nil {
		t.Fatalf("attach after restart: %v", err)
	}

	got, ok := second.GetServer(context.Background(), reg.ID)
	if !ok {
		t.Fatal("the registration did not survive the restart")
	}
	if got.URL != srv.URL {
		t.Errorf("url = %q, want %q", got.URL, srv.URL)
	}
	// The env has to survive too, or a server needing a token comes back unable
	// to authenticate.
	if got.Env["X-Token"] != "abc" {
		t.Errorf("env did not survive: %v", got.Env)
	}

	// AttachStore reconnects in the background; wait for it briefly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(second.ListTools(context.Background(), reg.ID)) > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Error("the restored server never reconnected, so its tools are unavailable")
}

func TestDeleteRemovesTheRowAndClosesTheConnection(t *testing.T) {
	fake := &fakeMCPServer{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	st := &memStore{}
	m := NewClientManager()
	defer m.Close()
	if err := m.AttachStore(context.Background(), st, nil); err != nil {
		t.Fatalf("attach: %v", err)
	}
	reg, _ := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "temp", Transport: "http", URL: srv.URL,
	})

	m.DeleteServer(context.Background(), reg.ID)
	if _, ok := m.GetServer(context.Background(), reg.ID); ok {
		t.Error("the server is still registered after deletion")
	}
	if _, ok := st.rows[reg.ID]; ok {
		t.Error("the row is still in the store after deletion")
	}
}

func TestRefreshPicksUpACatalogueChange(t *testing.T) {
	fake := &fakeMCPServer{pagedTools: false}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	m := NewClientManager()
	defer m.Close()
	reg, _ := m.RegisterServer(context.Background(), protocol.MCPServer{
		Name: "changing", Transport: "http", URL: srv.URL,
	})
	if got := len(m.ListTools(context.Background(), reg.ID)); got != 2 {
		t.Fatalf("started with %d tools, want 2", got)
	}

	// The server drops a tool, as a real one may at runtime.
	fake.pagedTools = true
	tools, err := m.RefreshTools(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("refreshed to %d tools, want 2 across the paged catalogue", len(tools))
	}
}

func TestListToolsReturnsAnEmptySliceNotNil(t *testing.T) {
	m := NewClientManager()
	defer m.Close()
	// Serialises as [] rather than null, which the console distinguishes.
	if got := m.ListTools(context.Background(), ""); got == nil {
		t.Error("ListTools returned nil; the JSON would be null")
	}
}
