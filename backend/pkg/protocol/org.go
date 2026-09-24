package protocol

import (
	"fmt"
	"strings"
	"time"
)

// ----------------------------------------------------------- agent kinds ---

// AgentKind is how an agent runs. A desktop agent is a sandbox this fleet
// provisions and drives itself; every other kind is an external runtime that
// runs somewhere else and is reached through an adapter, so it can sit in the
// same org chart, take the same tickets and be paid for from the same budget.
type AgentKind string

const (
	KindDesktop    AgentKind = "desktop"
	KindClaudeCode AgentKind = "claude_code"
	KindCodex      AgentKind = "codex"
	KindHermes     AgentKind = "hermes"
	KindOpenClaw   AgentKind = "openclaw"
	KindWebhook    AgentKind = "webhook"
)

// AgentKinds lists every kind in the order the console offers them.
var AgentKinds = []AgentKind{KindDesktop, KindClaudeCode, KindCodex, KindHermes, KindOpenClaw, KindWebhook}

// Valid reports whether k is a known kind. The empty kind is a desktop: every
// row written before kinds existed is one.
func (k AgentKind) Valid() bool {
	if k == "" {
		return true
	}
	for _, v := range AgentKinds {
		if v == k {
			return true
		}
	}
	return false
}

// External reports whether the agent runs outside this fleet's sandboxes.
func (k AgentKind) External() bool { return k != "" && k != KindDesktop }

// OnDevice reports whether the kind is a local CLI run by `fleetctl host` on
// a PC, as opposed to a network service the orchestrator calls itself.
func (k AgentKind) OnDevice() bool {
	return k == KindClaudeCode || k == KindCodex || k == KindHermes
}

// Label is the name people know the runtime by.
func (k AgentKind) Label() string {
	switch k {
	case KindClaudeCode:
		return "Claude Code"
	case KindCodex:
		return "Codex"
	case KindHermes:
		return "Hermes"
	case KindOpenClaw:
		return "OpenClaw"
	case KindWebhook:
		return "Webhook"
	}
	return "Desktop"
}

// AgentConnection is how an external agent is reached. It never holds a
// secret: TokenRef names a vault entry.
type AgentConnection struct {
	// DeviceID is the PC running `fleetctl host` that runs a local CLI.
	DeviceID string `json:"device_id,omitempty"`
	// Cwd is the folder on that device the agent works in. It must be one of
	// the folders the device exposes.
	Cwd string `json:"cwd,omitempty"`
	// Model is passed to the CLI when set (claude --model, codex -m ...).
	Model string `json:"model,omitempty"`
	// Args are extra arguments appended to the CLI invocation.
	Args []string `json:"args,omitempty"`
	// URL is the OpenClaw gateway or the webhook endpoint.
	URL string `json:"url,omitempty"`
	// TokenRef names the vault entry holding the gateway or webhook token.
	TokenRef string `json:"token_ref,omitempty"`
	// AgentID is the agent's id inside OpenClaw.
	AgentID string `json:"agent_id,omitempty"`
	// Autonomy: "full" lets a local CLI act without asking (inside its
	// folder); "edits" lets it edit files but not run commands. Empty is
	// "edits".
	Autonomy string `json:"autonomy,omitempty"`
	// TimeoutSec bounds one run. 0 means the default (30 minutes).
	TimeoutSec int `json:"timeout_sec,omitempty"`
	// AllowPrivate lets a webhook or OpenClaw agent be reached on a private
	// network. Set by the server when an admin gives an agent such an
	// address; never taken from a client or a template.
	AllowPrivate bool `json:"allow_private,omitempty"`
}

// Trust levels. A low-trust agent reads hostile input -- web pages, external
// tickets, untrusted repositories -- so what it writes reaches other agents
// only fenced as data, and it cannot hand work to them.
const (
	TrustStandard = "standard"
	TrustLow      = "low"
)

// Holds: why an agent is not being given work.
const (
	HoldNone   = ""
	HoldBudget = "budget"
)

// -------------------------------------------------------------- tickets ---

// TicketStatus is where a ticket is in its life.
type TicketStatus string

const (
	// Backlog: exists, but is waiting for something that has not been
	// arranged yet (a tester whose builder has not joined).
	TicketBacklog TicketStatus = "backlog"
	// Todo: ready to run as soon as its blockers are done.
	TicketTodo TicketStatus = "todo"
	// InProgress: a run holds it.
	TicketInProgress TicketStatus = "in_progress"
	// InReview: the work is finished and a reviewer is checking it.
	TicketInReview TicketStatus = "in_review"
	// Blocked: stopped for a reason that needs someone -- a manager, the
	// operator -- to act. BlockedReason says what.
	TicketBlocked   TicketStatus = "blocked"
	TicketDone      TicketStatus = "done"
	TicketCancelled TicketStatus = "cancelled"
)

// TicketStatuses in board order.
var TicketStatuses = []TicketStatus{TicketBacklog, TicketTodo, TicketInProgress, TicketInReview, TicketBlocked, TicketDone, TicketCancelled}

func (s TicketStatus) Valid() bool {
	for _, v := range TicketStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// Terminal reports whether the ticket is finished for good.
func (s TicketStatus) Terminal() bool { return s == TicketDone || s == TicketCancelled }

// TicketKind is what a ticket asks for.
type TicketKind string

const (
	TicketWork TicketKind = "work"
	// Review: check another ticket's work. The verdict is the deliverable: a
	// review that finds problems is done, with verdict fail.
	TicketReview TicketKind = "review"
	// Verify: a stopped subtree's claims checked against the evidence.
	TicketVerify TicketKind = "verify"
	// Unblock: a manager asked to get a report moving again.
	TicketUnblock TicketKind = "unblock"
)

func (k TicketKind) Valid() bool {
	switch k {
	case TicketWork, TicketReview, TicketVerify, TicketUnblock:
		return true
	}
	return false
}

// Verdicts a review or verify ticket ends with.
const (
	VerdictPass = "pass"
	VerdictFail = "fail"
)

// Ticket is one piece of work: one owner, the ticket it exists for, and the
// tickets it waits on.
type Ticket struct {
	ID          string       `json:"id"`
	Number      int64        `json:"number"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Kind        TicketKind   `json:"kind"`
	Status      TicketStatus `json:"status"`
	Priority    int          `json:"priority,omitempty"`
	ParentID    string       `json:"parent_id,omitempty"`
	TargetID    string       `json:"target_id,omitempty"`
	AssigneeID  string       `json:"assignee_id,omitempty"`
	// AssigneeUserID puts the ticket in a person's hands instead of an
	// agent's. Nothing is dispatched for it; a person closes it.
	AssigneeUserID string `json:"assignee_user_id,omitempty"`
	ReviewerID     string `json:"reviewer_id,omitempty"`
	VerifierID     string `json:"verifier_id,omitempty"`

	VerifiedFingerprint string `json:"-"`
	StallFingerprint    string `json:"-"`

	CreatedByID     string  `json:"created_by_id,omitempty"`
	CreatedByUserID string  `json:"created_by_user_id,omitempty"`
	OwnerID         string  `json:"owner_id,omitempty"`
	Thread          string  `json:"thread,omitempty"`
	Origin          string  `json:"origin,omitempty"`
	Stage           string  `json:"stage,omitempty"`
	TaskID          string  `json:"task_id,omitempty"`
	Attempts        int     `json:"attempts"`
	Rounds          int     `json:"rounds"`
	Wakes           int     `json:"wakes"`
	Verdict         string  `json:"verdict,omitempty"`
	Result          string  `json:"result,omitempty"`
	BlockedReason   string  `json:"blocked_reason,omitempty"`
	BudgetUSD       float64 `json:"budget_usd,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	DoneAt    *time.Time `json:"done_at,omitempty"`

	// Filled on read.
	BlockedBy []string `json:"blocked_by"`
	CostUSD   float64  `json:"cost_usd"`
}

// Ref is how people and agents name a ticket: T-42.
func (t *Ticket) Ref() string { return fmt.Sprintf("T-%d", t.Number) }

// ParseTicketRef reads "T-42", "t42", "#42" or "42" as ticket number 42.
func ParseTicketRef(s string) (int64, bool) {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.TrimPrefix(s, "#")
	s = strings.TrimPrefix(s, "T")
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return 0, false
	}
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int64(r-'0')
	}
	return n, n > 0
}

// TicketComment is one entry in a ticket's thread.
type TicketComment struct {
	ID           string    `json:"id"`
	TicketID     string    `json:"ticket_id"`
	AuthorID     string    `json:"author_id,omitempty"`
	AuthorUserID string    `json:"author_user_id,omitempty"`
	AuthorName   string    `json:"author_name,omitempty"`
	Kind         string    `json:"kind"` // comment | system | result | verdict
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

// TicketFilter narrows a ticket listing. Zero values match everything.
type TicketFilter struct {
	Status     []TicketStatus
	AssigneeID string
	ParentID   string
	RootsOnly  bool
	Limit      int
}

// -------------------------------------------------------------- org chart ---

// OrgNode is one agent in the org chart, with what the chart shows about it.
type OrgNode struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Title        string    `json:"title,omitempty"`
	Kind         AgentKind `json:"kind"`
	State        string    `json:"state"`
	ReportsTo    string    `json:"reports_to,omitempty"`
	Capabilities string    `json:"capabilities,omitempty"`
	Trust        string    `json:"trust"`
	Hold         string    `json:"hold,omitempty"`
	ArchetypeID  string    `json:"archetype_id,omitempty"`
	// Busy is true while a run is live; Ticket is the ticket it is on.
	Busy        bool    `json:"busy"`
	TicketRef   string  `json:"ticket_ref,omitempty"`
	TicketTitle string  `json:"ticket_title,omitempty"`
	SpendMonth  float64 `json:"spend_month_usd"`
	BudgetMonth float64 `json:"budget_month_usd"`
	OpenTickets int     `json:"open_tickets"`
	Online      bool    `json:"online"`
}

// --------------------------------------------------------- fleet templates ---

// FleetTemplate is a portable description of a fleet: its agents, how they
// report to each other, and what they are for. Exported with every secret
// scrubbed, imported into another deployment to recreate the org.
type FleetTemplate struct {
	Version    int             `json:"version"`
	Name       string          `json:"name,omitempty"`
	ExportedAt time.Time       `json:"exported_at"`
	Agents     []TemplateAgent `json:"agents"`
}

// TemplateAgent is one agent in a FleetTemplate. ReportsTo is a name, not an
// id: ids do not survive the move to another deployment.
type TemplateAgent struct {
	Name         string          `json:"name"`
	Kind         AgentKind       `json:"kind"`
	Title        string          `json:"title,omitempty"`
	ReportsTo    string          `json:"reports_to,omitempty"`
	Capabilities string          `json:"capabilities,omitempty"`
	ArchetypeID  string          `json:"archetype_id,omitempty"`
	Tier         Tier            `json:"tier,omitempty"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	ShellAccess  bool            `json:"shell_access,omitempty"`
	Voice        string          `json:"voice,omitempty"`
	BudgetMonth  float64         `json:"budget_month_usd,omitempty"`
	BudgetWarn   int             `json:"budget_warn_pct,omitempty"`
	Trust        string          `json:"trust,omitempty"`
	Connection   AgentConnection `json:"connection,omitempty"`
}
