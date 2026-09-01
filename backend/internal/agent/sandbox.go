package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// SandboxClient talks to agentd inside one sandbox. Everything the agent can
// perceive or do goes through here, which keeps the trust boundary explicit:
// agentd is the only component with access to the virtual input devices.
type SandboxClient struct {
	base string
	hc   *http.Client
}

func NewSandboxClient(base string) *SandboxClient {
	return &SandboxClient{
		base: base,
		hc:   &http.Client{Timeout: 4 * time.Minute},
	}
}

func (c *SandboxClient) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("agentd %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("agentd %s: %d: %s", path, resp.StatusCode, msg)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *SandboxClient) Health(ctx context.Context) error {
	return c.call(ctx, http.MethodGet, "/health", nil, nil)
}

// ObserveOptions controls how expensive a single observation is. Dropping the
// accessibility tree or shrinking the screenshot is the main lever on token cost.
type ObserveOptions struct {
	Screenshot bool `json:"screenshot"`
	A11y       bool `json:"a11y"`
	MaxWidth   int  `json:"max_width"`
	Quality    int  `json:"quality"`
}

func (c *SandboxClient) Observe(ctx context.Context, opt ObserveOptions) (*protocol.Observation, error) {
	var out protocol.Observation
	if err := c.call(ctx, http.MethodPost, "/observe", opt, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ActResult is what agentd reports back after executing an action.
type ActResult struct {
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Matched  bool   `json:"matched,omitempty"` // wait_for / assert outcome
}

func (c *SandboxClient) Act(ctx context.Context, a protocol.Action) (*ActResult, error) {
	var out ActResult
	if err := c.call(ctx, http.MethodPost, "/act", a, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartRecording begins capturing input and accessibility events.
func (c *SandboxClient) StartRecording(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/record/start", map[string]string{"name": name}, nil)
}

// RecordingResult is the raw trace agentd hands back when recording stops.
type RecordingResult struct {
	Name   string              `json:"name"`
	Events []protocol.RawEvent `json:"events"`
	Frames []string            `json:"frames,omitempty"` // object keys of key frames
}

func (c *SandboxClient) StopRecording(ctx context.Context) (*RecordingResult, error) {
	var out RecordingResult
	if err := c.call(ctx, http.MethodPost, "/record/stop", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Inject and ClearKeyring wrap agentd's `/keyring` endpoints.
//
// NOT WIRED UP. Nothing in the task lifecycle calls either method today: no run
// injects a vault secret into a sandbox, and no run clears one. They exist
// because the agentd endpoints exist, and are the client an operator tool or a
// future run-scoped credential feature would use. Until something calls them,
// treat "secrets are injected into the sandbox at runtime" as a plan, not a
// feature — docs/SECURITY.md says so plainly, and it must keep saying so until
// this changes.
//
// Two things to fix before wiring it up:
//   - agentd's KEYRING_DIR (/var/run/agentfleet/keyring) is an ordinary
//     directory on the container's writable layer, not tmpfs. Only /tmp is a
//     tmpfs mount (see fleet.Manager's HostConfig). A secret written there hits
//     the overlay filesystem.
//   - There is no ClearKeyring on the failure path, so a crashed orchestrator
//     would leave the value behind for the life of the container.
//
// Inject writes a secret into the sandbox keyring. The value is never echoed
// back and never enters the prompt history.
func (c *SandboxClient) Inject(ctx context.Context, name, value string) error {
	return c.call(ctx, http.MethodPost, "/keyring",
		map[string]string{"name": name, "value": value}, nil)
}

func (c *SandboxClient) ClearKeyring(ctx context.Context) error {
	return c.call(ctx, http.MethodDelete, "/keyring", nil, nil)
}
