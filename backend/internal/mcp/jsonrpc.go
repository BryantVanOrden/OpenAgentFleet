package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0, which is the wire format for every MCP transport.
//
// This package previously spoke no protocol at all: registering a server
// invented one tool named "<server>_query", and CallTool returned a formatted
// string saying it had executed something. The transport, command and URL
// fields were stored and never read. Everything from here down is the actual
// protocol.

const jsonRPCVersion = "2.0"

// rpcRequest is a call or a notification. A notification is the same envelope
// with no id, and the difference matters: a server must not answer one, so
// waiting for a reply to a notification hangs until the context expires.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	// Method is set on a server-initiated message. Servers are allowed to send
	// notifications (progress, list-changed) down the same channel, so a reader
	// that assumed every frame was a reply to something would mis-route them.
	Method string `json:"method,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("mcp error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message)
}

// transport carries JSON-RPC frames to one server.
//
// Two implementations: a child process over stdin/stdout, and HTTP POST with an
// optional SSE response body. They differ enough that the session layer above
// should not know which it has.
type transport interface {
	// Call sends a request and waits for the matching response.
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	// Notify sends a request with no id and does not wait.
	Notify(ctx context.Context, method string, params any) error
	// SetOnNotification registers a callback for server-initiated
	// notifications. Called from the transport's read path, so the callback
	// must not block; the manager's handler goes async immediately.
	SetOnNotification(fn func(method string))
	Close() error
}
