package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HTTP transport, covering both shapes an MCP server over HTTP can take.
//
// The current spec calls it "Streamable HTTP": every JSON-RPC frame is a POST,
// and the server answers with either a single application/json body or a
// text/event-stream carrying the reply as an SSE event. Which one you get is the
// server's choice and can differ per call, so both are handled on every
// request rather than being decided once at connect time.
//
// The stored transport value is "sse" in existing rows, which is the older name
// for the same thing; both "sse" and "http" map here.

type httpTransport struct {
	url    string
	client *http.Client
	header map[string]string

	nextID atomic.Int64

	// sessionID is the Mcp-Session-Id the server assigned at initialize, echoed
	// on every later request. Servers that keep per-session state reject
	// requests without it, and servers that do not use sessions never set it.
	mu        sync.RWMutex
	onNotify  func(method string)
	sessionID string
	// protocolVersion is echoed back on later requests, which newer servers
	// require once they have negotiated it.
	protocolVersion string
}

func newHTTPTransport(url string, header map[string]string) (*httpTransport, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, errors.New("an http MCP server needs a URL")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("MCP server URL must be http or https, got %q", url)
	}
	return &httpTransport{
		url: url,
		// Generous but bounded: a tool call can legitimately take a while, and
		// no timeout at all means one unresponsive server holds an agent's turn
		// open indefinitely.
		client: &http.Client{Timeout: 120 * time.Second},
		header: header,
	}, nil
}

func (t *httpTransport) SetProtocolVersion(v string) {
	t.mu.Lock()
	t.protocolVersion = v
	t.mu.Unlock()
}

func (t *httpTransport) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Both are advertised because the server picks: a single JSON body or an
	// SSE stream. Offering only one makes spec-compliant servers refuse.
	req.Header.Set("Accept", "application/json, text/event-stream")

	t.mu.RLock()
	session, version := t.sessionID, t.protocolVersion
	t.mu.RUnlock()
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	if version != "" {
		req.Header.Set("MCP-Protocol-Version", version)
	}
	for k, v := range t.header {
		req.Header.Set(k, v)
	}
	return t.client.Do(req)
}

func (t *httpTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	body, err := json.Marshal(rpcRequest{
		JSONRPC: jsonRPCVersion, ID: &id, Method: method, Params: params,
	})
	if err != nil {
		return nil, err
	}

	resp, err := t.post(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Captured before the status check: a server that assigns a session and
	// then reports an error still wants the id on the retry.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("MCP server returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return t.readSSE(resp.Body, id)
	}

	// Bounded read: a tool result is text, and an unbounded read from a server
	// that has misunderstood the request is a way to exhaust the orchestrator.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("MCP server returned an empty body")
	}
	return decodeFrame(raw, id)
}

// readSSE pulls events until the one answering our id arrives.
//
// The stream can legitimately carry other frames first — progress
// notifications, and replies to requests the server itself initiated — so this
// filters by id rather than taking the first event it sees.
func (t *httpTransport) readSSE(body io.Reader, want int64) (json.RawMessage, error) {
	sc := bufio.NewScanner(io.LimitReader(body, 32<<20))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)

	var data strings.Builder
	flush := func() (json.RawMessage, bool, error) {
		if data.Len() == 0 {
			return nil, false, nil
		}
		payload := data.String()
		data.Reset()
		result, err := decodeFrame([]byte(payload), want)
		if err != nil {
			if errors.Is(err, errFrameNotMine) {
				// Not our reply — possibly a notification riding the stream.
				t.handOffNotification([]byte(payload))
				return nil, false, nil
			}
			return nil, true, err
		}
		return result, true, nil
	}

	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			// Blank line ends an event.
			if result, done, err := flush(); done || err != nil {
				return result, err
			}
		case strings.HasPrefix(line, "data:"):
			// Multi-line data fields concatenate with newlines per the SSE spec.
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		default:
			// event:, id:, retry: and comments are not needed here.
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// A stream that ended without a blank line still has a complete event.
	if result, done, err := flush(); done || err != nil {
		return result, err
	}
	return nil, errors.New("the MCP server's stream ended without answering")
}

// errFrameNotMine marks a well-formed frame addressed to a different request.
var errFrameNotMine = errors.New("frame belongs to another request")

// handOffNotification recognises a server-initiated notification frame and
// hands its method to the callback. HTTP has no standing server->client
// channel in this client (the optional GET stream is not opened), so the only
// place a notification can arrive is interleaved in a call's own SSE stream —
// which is exactly where servers send list_changed after a tools/call that
// mutated their catalogue.
func (t *httpTransport) handOffNotification(raw []byte) {
	var resp rpcResponse
	if json.Unmarshal(bytes.TrimSpace(raw), &resp) != nil {
		return
	}
	if resp.ID != nil || resp.Method == "" {
		return
	}
	t.mu.RLock()
	fn := t.onNotify
	t.mu.RUnlock()
	if fn != nil {
		fn(resp.Method)
	}
}

func (t *httpTransport) SetOnNotification(fn func(method string)) {
	t.mu.Lock()
	t.onNotify = fn
	t.mu.Unlock()
}

func decodeFrame(raw []byte, want int64) (json.RawMessage, error) {
	// A batch is legal JSON-RPC and some servers use it even for one call.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []rpcResponse
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			return nil, fmt.Errorf("unreadable MCP batch response: %w", err)
		}
		for _, r := range batch {
			if r.ID != nil && *r.ID == want {
				if r.Error != nil {
					return nil, r.Error
				}
				return r.Result, nil
			}
		}
		return nil, errFrameNotMine
	}

	var resp rpcResponse
	if err := json.Unmarshal(trimmed, &resp); err != nil {
		return nil, fmt.Errorf("unreadable MCP response: %w", err)
	}
	// A notification, or a reply to something else.
	if resp.ID == nil || *resp.ID != want {
		return nil, errFrameNotMine
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

func (t *httpTransport) Notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(rpcRequest{
		JSONRPC: jsonRPCVersion, Method: method, Params: params,
	})
	if err != nil {
		return err
	}
	resp, err := t.post(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drained so the connection can be reused rather than abandoned.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("MCP server returned %d to a %s notification", resp.StatusCode, method)
	}
	return nil
}

// Close releases the server-side session if there is one.
//
// A DELETE is how the spec says to end a session; servers that do not implement
// it answer 404 or 405, which is not an error worth reporting to anyone.
func (t *httpTransport) Close() error {
	t.mu.RLock()
	session := t.sessionID
	t.mu.RUnlock()
	if session == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Mcp-Session-Id", session)
	for k, v := range t.header {
		req.Header.Set(k, v)
	}
	resp, err := t.client.Do(req)
	if err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
	}
	return nil
}
