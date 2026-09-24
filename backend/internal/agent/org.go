package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// TicketOps is what the runner needs from the ticket engine: the actions an
// agent performs on tickets, and the budget check after every turn.
type TicketOps interface {
	RequiresVerdict(ctx context.Context, taskID string) bool
	SetVerdict(taskID, verdict string)
	CreateFromAgent(ctx context.Context, taskID string, inst *protocol.Instance, target, title, text string, wait bool) (string, error)
	ReopenFromAgent(ctx context.Context, taskID string, inst *protocol.Instance, ref, reason string) (string, error)
	AfterTurn(ctx context.Context, instanceID, taskID string)
	UndelegatedReports(ctx context.Context, taskID string) []string
}

// External runs agents that are not desktops. Runner.Start hands it every
// task whose instance is an external kind, so everything that starts work --
// tickets, triggers, pipelines, the task endpoint -- reaches Claude Code or an
// OpenClaw gateway the same way it reaches a sandbox.
type External interface {
	Start(ctx context.Context, task *protocol.Task, inst *protocol.Instance) error
	Cancel(taskID string) bool
	IsRunning(taskID string) bool
	Ready(ctx context.Context, inst *protocol.Instance) (bool, string)
}

// SetTicketOps connects the ticket engine.
func (r *Runner) SetTicketOps(t TicketOps) { r.tickets = t }

// SetExternal connects the external-agent adapters.
func (r *Runner) SetExternal(x External) { r.external = x }

// Ready reports whether an agent can take a run now.
func (r *Runner) Ready(ctx context.Context, inst *protocol.Instance) (bool, string) {
	if inst.AgentKindOf().External() {
		if r.external == nil {
			return false, "external agents are not enabled on this server"
		}
		return r.external.Ready(ctx, inst)
	}
	switch inst.State {
	case protocol.InstanceRunning:
		return true, ""
	case protocol.InstancePaused:
		return false, "its desktop is paused"
	case protocol.InstanceProvisioning:
		return false, "its desktop is still starting"
	case protocol.InstanceStopped:
		return false, "its desktop is stopped"
	}
	return false, "its desktop is " + string(inst.State)
}

// DeliverAlert publishes an alert that is already stored: the live event and
// the phone push.
func (r *Runner) DeliverAlert(ctx context.Context, a *protocol.Alert) {
	r.bus.Emit("alert", a.InstanceID, a.TaskID, a)
	if r.notify != nil {
		_ = r.notify.Send(ctx, *a)
	}
}

// rosterLine is how one colleague appears in an agent's FLEET block: its
// name, title, how it relates to the reader in the org chart, what it is for,
// and what kind of agent it is.
func rosterLine(in protocol.Instance, self *protocol.Instance) string {
	var b strings.Builder
	b.WriteString("- " + in.Name)
	if in.Title != "" {
		b.WriteString(" (" + in.Title + ")")
	}
	if self != nil {
		switch {
		case self.ReportsTo == in.ID:
			b.WriteString(" — your manager")
		case in.ReportsTo == self.ID:
			b.WriteString(" — reports to you")
		}
	}
	if in.AgentKindOf().External() {
		b.WriteString(" [" + in.AgentKindOf().Label() + "]")
	}
	if in.Capabilities != "" {
		b.WriteString(": " + clip(in.Capabilities, 160))
	}
	fmt.Fprintf(&b, " (%s)\n", in.ID)
	return b.String()
}

// leadingVerdict reads a verdict a summary opens with.
func leadingVerdict(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	switch {
	case strings.HasPrefix(u, "PASS"):
		return protocol.VerdictPass
	case strings.HasPrefix(u, "FAIL"):
		return protocol.VerdictFail
	}
	return ""
}
