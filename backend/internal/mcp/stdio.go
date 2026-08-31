package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// stdio transport: the server is a child process, and the wire is its stdin and
// stdout carrying newline-delimited JSON.
//
// This is the transport almost every MCP server ships with, so a bridge that
// did not implement it could not talk to anything real.
//
// Security. Registering a stdio server is asking the orchestrator to execute a
// command. That is what the transport is, not a flaw in this implementation, and
// the registration endpoint is admin-only — an admin already has the Docker
// socket, which docs/SECURITY.md is explicit is equivalent to host root. It is
// still the single loudest thing in this package, so it is logged at
// registration and can be switched off entirely with MCP_DISABLE_STDIO=true for
// operators who want the HTTP transport only.

// StdioDisabled reports whether the operator has turned stdio servers off.
func StdioDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MCP_DISABLE_STDIO")))
	return v == "true" || v == "1" || v == "yes"
}

// ErrStdioDisabled is returned when a stdio server is registered on a
// deployment that has switched the transport off.
var ErrStdioDisabled = errors.New(
	"stdio MCP servers are disabled on this deployment (MCP_DISABLE_STDIO); use an http server instead")

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan *rpcResponse
	closed  bool

	// writeMu serialises writes. Two concurrent agents calling tools on the
	// same server would otherwise interleave halves of two JSON frames into
	// stdin and desynchronise the server permanently.
	writeMu sync.Mutex

	// stderr keeps the last few lines the server complained about. An MCP
	// server that fails to start usually says why on stderr and then exits, and
	// without this the operator sees only "broken pipe".
	stderrMu sync.Mutex
	stderr   []string
}

func newStdioTransport(ctx context.Context, command string, args []string, env map[string]string) (*stdioTransport, error) {
	if StdioDisabled() {
		return nil, ErrStdioDisabled
	}
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("a stdio MCP server needs a command to run")
	}

	// Not bound to ctx: ctx is the registering HTTP request, and the child has
	// to outlive it. Close() is what stops the process.
	cmd := exec.Command(command, args...)

	// The parent environment plus the configured additions. A server that needs
	// PATH to find node, and an API key from the operator, needs both.
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	t := &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 1<<20),
		pending: make(map[int64]chan *rpcResponse),
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start %q: %w", command, err)
	}

	go t.drainStderr(stderr)
	go t.readLoop()
	return t, nil
}

// readLoop dispatches every frame the server sends to whoever is waiting.
//
// One reader, because two goroutines reading the same pipe would each get half
// the frames. Responses go to the caller blocked on that id; notifications are
// dropped, deliberately — nothing here subscribes to progress or list-changed
// yet, and buffering them would be a leak.
func (t *stdioTransport) readLoop() {
	defer t.failAllPending(errors.New("the MCP server closed its output"))
	for {
		line, err := t.stdout.ReadBytes('\n')
		if len(line) > 0 {
			t.dispatch(line)
		}
		if err != nil {
			return
		}
	}
}

func (t *stdioTransport) dispatch(line []byte) {
	line = trimFrame(line)
	if len(line) == 0 {
		return
	}
	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		// Not fatal. Some servers print a banner to stdout before speaking
		// JSON, and killing the session over a stray log line would make those
		// servers unusable for a cosmetic reason.
		return
	}
	if resp.ID == nil {
		return // a notification; see readLoop
	}

	t.mu.Lock()
	ch, ok := t.pending[*resp.ID]
	delete(t.pending, *resp.ID)
	t.mu.Unlock()
	if ok {
		ch <- &resp
	}
}

func trimFrame(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func (t *stdioTransport) drainStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		t.stderrMu.Lock()
		t.stderr = append(t.stderr, line)
		if len(t.stderr) > 20 {
			t.stderr = t.stderr[len(t.stderr)-20:]
		}
		t.stderrMu.Unlock()
	}
}

// LastStderr is what the server last complained about, for error messages.
func (t *stdioTransport) LastStderr() string {
	t.stderrMu.Lock()
	defer t.stderrMu.Unlock()
	if len(t.stderr) == 0 {
		return ""
	}
	return strings.Join(t.stderr, "; ")
}

func (t *stdioTransport) send(id *int64, method string, params any) error {
	frame, err := json.Marshal(rpcRequest{
		JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: params,
	})
	if err != nil {
		return err
	}
	frame = append(frame, '\n')

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.stdin.Write(frame); err != nil {
		if detail := t.LastStderr(); detail != "" {
			return fmt.Errorf("%w (server said: %s)", err, detail)
		}
		return err
	}
	return nil
}

func (t *stdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	ch := make(chan *rpcResponse, 1)

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, errors.New("the MCP server connection is closed")
	}
	t.pending[id] = ch
	t.mu.Unlock()

	if err := t.send(&id, method, params); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		// Abandoned rather than left to leak: the server may still answer, and
		// dispatch would block forever writing to a channel nobody reads if the
		// entry stayed and the buffer were full.
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) Notify(ctx context.Context, method string, params any) error {
	return t.send(nil, method, params)
}

func (t *stdioTransport) failAllPending(err error) {
	t.mu.Lock()
	pending := t.pending
	t.pending = make(map[int64]chan *rpcResponse)
	t.mu.Unlock()

	for _, ch := range pending {
		// Delivered as an error response so the caller unblocks with a reason
		// instead of waiting out its context.
		ch <- &rpcResponse{Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
}

func (t *stdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	// Closing stdin is how an MCP server is asked to shut down; the kill is the
	// fallback for one that ignores it.
	_ = t.stdin.Close()

	done := make(chan struct{})
	go func() {
		_ = t.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		<-done
	}
	t.failAllPending(errors.New("the MCP server connection was closed"))
	return nil
}
