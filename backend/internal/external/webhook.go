package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A webhook agent is anything that accepts a POST. It either answers in the
// response -- {"status":"done","result":"..."} -- or answers later by calling
// back with the run token it was given. Unlike a fire-and-forget hook, a run
// is not over until it says so, so the ticket it holds finishes properly.

// WebhookRequest is the body a webhook agent receives.
type WebhookRequest struct {
	Agent struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"agent"`
	RunID    string `json:"run_id"`
	Ticket   string `json:"ticket,omitempty"`
	Prompt   string `json:"prompt"`
	Callback struct {
		Token    string `json:"token"`
		Complete string `json:"complete_url"`
		Progress string `json:"progress_url"`
		Tickets  string `json:"tickets_url"`
		Comments string `json:"comments_url"`
		Context  string `json:"context_url"`
	} `json:"callback"`
}

// Completion is how a webhook agent reports the end of a run, either in its
// response or at the complete URL.
type Completion struct {
	Status       string  `json:"status"` // done | failed | accepted
	Result       string  `json:"result"`
	Error        string  `json:"error,omitempty"`
	Verdict      string  `json:"verdict,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	Model        string  `json:"model,omitempty"`
}

var webhookClient = &http.Client{Timeout: 60 * time.Second}

func (d *Dispatcher) runWebhook(ctx context.Context, r *run) outcome {
	inst, task := r.inst, r.task
	token := ""
	if inst.Connection.TokenRef != "" && d.secrets != nil {
		if v, err := d.secrets.Open(ctx, inst.Connection.TokenRef); err == nil {
			token = v
		}
	}
	ticketRef := ""
	if task.TicketID != "" {
		if t, err := d.db.Ticket(ctx, task.TicketID); err == nil {
			ticketRef = t.Ref()
		}
	}
	base := strings.TrimRight(d.cfg.PublicURL, "/") + "/api/runs/" + task.ID
	var body WebhookRequest
	body.Agent.ID, body.Agent.Name = inst.ID, inst.Name
	body.RunID, body.Ticket = task.ID, ticketRef
	body.Prompt = d.Prompt(r, "")
	body.Callback.Token = MintRunToken(d.cfg.Secret, task.ID, 48*time.Hour)
	body.Callback.Complete, body.Callback.Progress = base+"/complete", base+"/progress"
	body.Callback.Tickets, body.Callback.Comments, body.Callback.Context = base+"/tickets", base+"/comments", base
	raw, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.Connection.URL, bytes.NewReader(raw))
	if err != nil {
		return outcome{err: "the webhook address is not usable: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentFleet-Run", task.ID)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := webhookClient.Do(req)
	if err != nil {
		return outcome{err: "the agent could not be reached: the webhook did not answer: " + err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return outcome{err: fmt.Sprintf("the webhook answered %d: %s", resp.StatusCode, clip(strings.TrimSpace(string(data)), 300))}
	}
	var sync Completion
	if json.Unmarshal(data, &sync) == nil {
		switch strings.ToLower(sync.Status) {
		case "done", "succeeded", "ok":
			return completionOutcome(sync, true)
		case "failed", "error":
			return completionOutcome(sync, false)
		}
	}
	d.progress(ctx, r, "status", "The webhook accepted the run; waiting for it to report back.")
	select {
	case out := <-r.callback:
		return out
	case <-ctx.Done():
		return outcome{}
	}
}

func completionOutcome(c Completion, ok bool) outcome {
	out := outcome{ok: ok, answer: c.Result, err: c.Error, verdict: strings.ToLower(c.Verdict),
		costUSD: c.CostUSD, inTokens: c.InputTokens, outTokens: c.OutputTokens, model: c.Model}
	if !ok && out.err == "" {
		out.err = firstNonEmpty(c.Result, "the webhook agent reported a failure")
	}
	return out
}

// Complete is the callback that finishes a webhook run (or any external run
// that calls back). It reports whether the run was waiting for it.
func (d *Dispatcher) Complete(taskID string, c Completion) bool {
	d.mu.Lock()
	r, ok := d.runs[taskID]
	d.mu.Unlock()
	if !ok {
		return false
	}
	st := strings.ToLower(c.Status)
	out := completionOutcome(c, st == "done" || st == "succeeded" || st == "ok" || st == "")
	select {
	case r.callback <- out:
		return true
	default:
		return false
	}
}

// Progress records a line an external agent reports through its callback.
func (d *Dispatcher) Progress(ctx context.Context, taskID, text string) bool {
	d.mu.Lock()
	r, ok := d.runs[taskID]
	d.mu.Unlock()
	if !ok {
		return false
	}
	d.progress(ctx, r, "progress", text)
	return true
}

// RunInstance is the agent a live run belongs to, for callbacks that act on
// its behalf.
func (d *Dispatcher) RunInstance(taskID string) (*protocol.Instance, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.runs[taskID]
	if !ok {
		return nil, false
	}
	return r.inst, true
}
