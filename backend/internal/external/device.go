package external

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Local CLIs run as a device job: `fleetctl host` on the agent's PC claims
// it, runs claude / codex / hermes in the agent's folder, streams what it is
// doing to the progress endpoint, and posts the final answer with usage and
// the session id to resume next time.

// JobKindAgentRun is the device job kind for an external agent's run.
const JobKindAgentRun = "agent_run"

// DeviceResult is what the host posts when an agent run finishes, JSON in the
// job's result.
type DeviceResult struct {
	Answer       string  `json:"answer"`
	SessionID    string  `json:"session_id,omitempty"`
	Model        string  `json:"model,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CachedTokens int     `json:"cached_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	IsError      bool    `json:"is_error,omitempty"`
	Error        string  `json:"error,omitempty"`
	ExitCode     int     `json:"exit_code,omitempty"`
}

// ProgressEvent is one line the host streams while an agent works.
type ProgressEvent struct {
	Kind string `json:"kind"` // text | tool | status
	Text string `json:"text"`
}

func (d *Dispatcher) runOnDevice(ctx context.Context, r *run) outcome {
	inst, task := r.inst, r.task
	c := inst.Connection
	if ok, why := d.Ready(ctx, inst); !ok {
		return outcome{err: "device went offline: " + why}
	}
	session := d.resumableSession(ctx, task, c.Cwd)
	ticketRef := "your ticket"
	if task.TicketID != "" {
		if t, err := d.db.Ticket(ctx, task.TicketID); err == nil {
			ticketRef = t.Ref()
		}
	}
	token := MintRunToken(d.cfg.Secret, task.ID, 48*time.Hour)
	args := map[string]any{
		"runtime":     string(inst.AgentKindOf()),
		"agent":       inst.Name,
		"cwd":         c.Cwd,
		"prompt":      d.Prompt(r, apiHint(ticketRef)),
		"model":       c.Model,
		"extra_args":  c.Args,
		"autonomy":    firstNonEmpty(c.Autonomy, "edits"),
		"session_id":  session,
		"task_id":     task.ID,
		"ticket":      ticketRef,
		"timeout_sec": int(d.timeoutFor(inst) / time.Second),
		"env": map[string]string{
			"AGENTFLEET_RUN_TOKEN": token,
			"AGENTFLEET_TASK_ID":   task.ID,
			"AGENTFLEET_TICKET":    ticketRef,
			"AGENTFLEET_AGENT":     inst.Name,
		},
	}
	job := &protocol.DeviceJob{DeviceID: c.DeviceID, Kind: JobKindAgentRun, Args: args}
	if err := d.db.CreateDeviceJob(ctx, job); err != nil {
		return outcome{err: "agent cli exited: could not queue the run on the device: " + err.Error()}
	}
	d.mu.Lock()
	r.jobID = job.ID
	d.mu.Unlock()
	label := inst.AgentKindOf().Label()
	d.progress(ctx, r, "status", fmt.Sprintf("Sent to %s on its PC, in %s%s.", label, c.Cwd, resumeNote(session)))

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	claimedAt := time.Time{}
	for {
		select {
		case <-ctx.Done():
			// Stopped or timed out: the host learns on its next progress post
			// and kills the CLI. Give it a moment to say so.
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				if j, err := d.db.DeviceJob(context.WithoutCancel(ctx), job.ID); err == nil && terminalJob(j.State) {
					break
				}
				time.Sleep(time.Second)
			}
			return outcome{}
		case <-tick.C:
		}
		j, err := d.db.DeviceJob(ctx, job.ID)
		if err != nil {
			continue
		}
		switch j.State {
		case protocol.DeviceJobPending:
			if dev, err := d.db.OafDevice(ctx, c.DeviceID); err == nil && d.now().Sub(dev.LastSeen) > 2*d.cfg.DeviceOnline {
				return outcome{err: fmt.Sprintf("device went offline before %s picked the run up", dev.Name)}
			}
		case protocol.DeviceJobRunning:
			if claimedAt.IsZero() {
				claimedAt = d.now()
			}
			if dev, err := d.db.OafDevice(ctx, c.DeviceID); err == nil && d.now().Sub(dev.LastSeen) > 3*d.cfg.DeviceOnline {
				return outcome{err: fmt.Sprintf("device went offline during the run (%s stopped polling)", dev.Name)}
			}
		case protocol.DeviceJobDenied:
			return outcome{err: fmt.Sprintf("the operator declined the run on the PC: %s", firstNonEmpty(j.Error, "denied"))}
		case protocol.DeviceJobDone, protocol.DeviceJobFailed:
			return parseDeviceResult(j, label)
		}
	}
}

func terminalJob(st protocol.DeviceJobState) bool {
	return st == protocol.DeviceJobDone || st == protocol.DeviceJobFailed || st == protocol.DeviceJobDenied
}

func resumeNote(session string) string {
	if session == "" {
		return ""
	}
	return ", resuming its previous session on this ticket"
}

func (d *Dispatcher) timeoutFor(inst *protocol.Instance) time.Duration {
	if inst.Connection.TimeoutSec > 0 {
		return time.Duration(inst.Connection.TimeoutSec) * time.Second
	}
	return d.cfg.DefaultTimeout
}

// resumableSession is the CLI session a previous run on the same ticket left,
// when it ran in the same folder: the agent picks up with its own context
// instead of starting cold.
func (d *Dispatcher) resumableSession(ctx context.Context, task *protocol.Task, cwd string) string {
	if task.TicketID == "" {
		return ""
	}
	runs, err := d.db.TicketTasks(ctx, task.TicketID)
	if err != nil {
		return ""
	}
	for _, k := range runs {
		if k.ID == task.ID || k.InstanceID != task.InstanceID {
			continue
		}
		if sid := k.Params["session_id"]; sid != "" && strings.EqualFold(k.Params["session_cwd"], cwd) {
			return sid
		}
	}
	return ""
}

func parseDeviceResult(j *protocol.DeviceJob, label string) outcome {
	var res DeviceResult
	if err := json.Unmarshal([]byte(j.Result), &res); err != nil {
		// A host too old to send JSON: the text is the answer.
		if j.State == protocol.DeviceJobDone {
			return outcome{ok: true, answer: j.Result}
		}
		return outcome{err: "agent cli exited: " + firstNonEmpty(j.Error, j.Result, "no output")}
	}
	out := outcome{
		answer: res.Answer, model: res.Model, inTokens: res.InputTokens, outTokens: res.OutputTokens,
		cached: res.CachedTokens, costUSD: res.CostUSD, sessionID: res.SessionID,
	}
	if j.State == protocol.DeviceJobDone && !res.IsError {
		out.ok = true
		return out
	}
	why := firstNonEmpty(res.Error, j.Error, clip(res.Answer, 400), "no reason given")
	if isLoginError(why) {
		out.err = fmt.Sprintf("%s is not signed in on its PC: %s", label, clip(why, 300))
		return out
	}
	out.err = fmt.Sprintf("agent cli exited (%s, code %d): %s", label, res.ExitCode, clip(why, 600))
	return out
}

// isLoginError spots a CLI that needs someone to sign in. That is not a
// failure a retry gets past, so it is not reported as a system failure.
func isLoginError(s string) bool {
	l := strings.ToLower(s)
	for _, p := range []string{"please run /login", "not logged in", "invalid api key", "authentication_error", "please log in", "login required", "unauthorized"} {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// DeviceProgress records what a host reports about a running agent job, and
// tells it whether to stop.
func (d *Dispatcher) DeviceProgress(ctx context.Context, jobID string, events []ProgressEvent) (cancel bool, known bool) {
	d.mu.Lock()
	var r *run
	for _, x := range d.runs {
		if x.jobID == jobID {
			r = x
			break
		}
	}
	stopped := false
	if r != nil {
		stopped = r.stopped
	}
	d.mu.Unlock()
	if r == nil {
		// Not a run this process knows: the orchestrator restarted, or the
		// run was abandoned. Stop it rather than let it work for nobody.
		return true, false
	}
	for i, ev := range events {
		if i >= 50 {
			break
		}
		d.progress(ctx, r, ev.Kind, ev.Text)
	}
	return stopped || r.task == nil, true
}
