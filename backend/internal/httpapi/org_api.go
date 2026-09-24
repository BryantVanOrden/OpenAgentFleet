package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/external"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The org chart, agent profiles, external agents, and fleet templates.

// ---------------------------------------------------------------- the chart ---

type orgChart struct {
	Nodes []protocol.OrgNode `json:"nodes"`
	// Kinds are the agent kinds this server can run, for the add dialog.
	Kinds []orgKind `json:"kinds"`
}

type orgKind struct {
	Kind     protocol.AgentKind `json:"kind"`
	Label    string             `json:"label"`
	OnDevice bool               `json:"on_device"`
}

func (s *Server) handleOrgChart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := s.db.ListInstances(ctx)
	if err != nil {
		failErr(w, err)
		return
	}
	insts := visibleInstances(accessFrom(ctx), all)
	spend, _ := s.db.SpendByInstance(ctx, store.MonthStart(time.Now()))
	open, _ := s.db.OpenTickets(ctx)
	openBy := map[string]int{}
	current := map[string]*protocol.Ticket{}
	for i := range open {
		t := &open[i]
		if t.AssigneeID == "" {
			continue
		}
		openBy[t.AssigneeID]++
		if t.Status == protocol.TicketInProgress {
			current[t.AssigneeID] = t
		}
	}
	visible := map[string]bool{}
	for _, in := range insts {
		visible[in.ID] = true
	}
	chart := orgChart{Nodes: make([]protocol.OrgNode, 0, len(insts))}
	for _, in := range insts {
		n := protocol.OrgNode{
			ID: in.ID, Name: in.Name, Title: in.Title, Kind: in.AgentKindOf(), State: string(in.State),
			Capabilities: in.Capabilities, Trust: in.Trust, Hold: in.Hold, ArchetypeID: in.ArchetypeID,
			SpendMonth: spend[in.ID], BudgetMonth: in.BudgetMonthUSD, OpenTickets: openBy[in.ID],
		}
		// A manager someone cannot see is not drawn: the agent hangs from the
		// top instead, so the chart never points at a hidden box.
		if visible[in.ReportsTo] {
			n.ReportsTo = in.ReportsTo
		}
		if t := current[in.ID]; t != nil {
			n.Busy, n.TicketRef, n.TicketTitle = true, t.Ref(), t.Title
		}
		n.Online, _ = s.runner.Ready(ctx, &in)
		chart.Nodes = append(chart.Nodes, n)
	}
	for _, k := range protocol.AgentKinds {
		chart.Kinds = append(chart.Kinds, orgKind{Kind: k, Label: k.Label(), OnDevice: k.OnDevice()})
	}
	writeJSON(w, http.StatusOK, chart)
}

// ------------------------------------------------------------- the profile ---

type profileRequest struct {
	Title          *string                   `json:"title"`
	Capabilities   *string                   `json:"capabilities"`
	ReportsTo      *string                   `json:"reports_to"`
	BudgetMonthUSD *float64                  `json:"budget_month_usd"`
	BudgetWarnPct  *int                      `json:"budget_warn_pct"`
	Trust          *string                   `json:"trust"`
	Connection     *protocol.AgentConnection `json:"connection"`
	Token          *string                   `json:"token"`
}

// handleSetProfile edits an agent's place in the org chart and its limits.
// Budgets are cost controls and need an admin; the rest needs edit rights on
// the agent.
func (s *Server) handleSetProfile(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requirePerm(w, r, r.PathValue("id"), protocol.PermEdit)
	if !ok {
		return
	}
	var req profileRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if (req.BudgetMonthUSD != nil || req.BudgetWarnPct != nil) && !roleAllows(userFrom(ctx).Role, roleAdmin) {
		fail(w, http.StatusForbidden, "budgets are set by an admin")
		return
	}
	if req.Title != nil {
		inst.Title = strings.TrimSpace(*req.Title)
	}
	if req.Capabilities != nil {
		inst.Capabilities = strings.TrimSpace(*req.Capabilities)
	}
	budgetChanged := false
	if req.BudgetMonthUSD != nil {
		if *req.BudgetMonthUSD < 0 {
			fail(w, http.StatusBadRequest, "a budget cannot be negative")
			return
		}
		inst.BudgetMonthUSD = *req.BudgetMonthUSD
		budgetChanged = true
	}
	if req.BudgetWarnPct != nil {
		if *req.BudgetWarnPct < 1 || *req.BudgetWarnPct > 100 {
			fail(w, http.StatusBadRequest, "the warning is a percentage between 1 and 100")
			return
		}
		inst.BudgetWarnPct = *req.BudgetWarnPct
		budgetChanged = true
	}
	if req.Trust != nil {
		switch *req.Trust {
		case protocol.TrustStandard, protocol.TrustLow:
			inst.Trust = *req.Trust
		default:
			fail(w, http.StatusBadRequest, "trust is standard or low")
			return
		}
	}
	if req.Connection != nil {
		if !inst.AgentKindOf().External() {
			fail(w, http.StatusBadRequest, "a desktop has no connection to set")
			return
		}
		conn := *req.Connection
		conn.TokenRef = inst.Connection.TokenRef
		if err := s.validateConnection(ctx, inst.AgentKindOf(), &conn, userFrom(ctx).Subject); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		inst.Connection = conn
	}
	if req.Token != nil && inst.AgentKindOf().External() {
		ref := firstNonEmptyStr(inst.Connection.TokenRef, "agent/"+inst.ID+"/token")
		if err := s.vault.Put(ctx, ref, *req.Token, "Token for "+inst.Name); err != nil {
			failErr(w, err)
			return
		}
		inst.Connection.TokenRef = ref
	}
	if err := s.db.UpdateInstance(ctx, inst); err != nil {
		failErr(w, err)
		return
	}
	if req.ReportsTo != nil && *req.ReportsTo != inst.ReportsTo {
		if *req.ReportsTo != "" {
			if _, ok := s.requirePerm(w, r, *req.ReportsTo, protocol.PermView); !ok {
				return
			}
		}
		if err := s.db.SetReportsTo(ctx, inst.ID, *req.ReportsTo); err != nil {
			failErr(w, err)
			return
		}
		inst.ReportsTo = *req.ReportsTo
	}
	if budgetChanged {
		s.tickets.BudgetChanged(ctx, inst.ID)
	}
	fresh, err := s.db.Instance(ctx, inst.ID)
	if err != nil {
		failErr(w, err)
		return
	}
	s.bus.Emit("instance.state", fresh.ID, "", redact(*fresh))
	writeJSON(w, http.StatusOK, redact(*fresh))
}

// ------------------------------------------------------- external agents ---

// createExternal creates an agent that runs somewhere else: no sandbox is
// provisioned, and its runs go to its adapter.
func (s *Server) createExternal(ctx context.Context, req fleet.CreateRequest) (*protocol.Instance, error) {
	kind := req.Kind
	if !kind.Valid() || !kind.External() {
		return nil, fmt.Errorf("%w: unknown agent kind %q", fleet.ErrInvalidRequest, kind)
	}
	conn := req.Connection
	if err := s.validateConnection(ctx, kind, &conn, req.OwnerID); err != nil {
		return nil, fmt.Errorf("%w: %s", fleet.ErrInvalidRequest, err)
	}
	now := time.Now().UTC()
	name := strings.TrimSpace(req.Name)
	id := store.NewID()
	if name == "" {
		name = kind.Label() + " " + id[:4]
	}
	inst := &protocol.Instance{
		ID: id, Name: name, OwnerID: req.OwnerID, OrgIDs: orgIDsOf(req.OrgID),
		SystemPrompt: req.SystemPrompt, Tier: "external", State: protocol.InstanceRunning,
		Kind: kind, Title: req.Title, ReportsTo: req.ReportsTo, Capabilities: req.Capabilities,
		Connection: conn, BudgetMonthUSD: req.BudgetMonthUSD, BudgetWarnPct: req.BudgetWarnPct,
		Trust: req.Trust, Voice: req.Voice, Labels: req.Labels, CreatedAt: now, UpdatedAt: now,
	}
	if inst.Capabilities == "" {
		inst.Capabilities = defaultCapabilities(kind)
	}
	if strings.TrimSpace(req.Token) != "" {
		ref := "agent/" + id + "/token"
		if err := s.vault.Put(ctx, ref, strings.TrimSpace(req.Token), "Token for "+name); err != nil {
			return nil, err
		}
		inst.Connection.TokenRef = ref
	}
	if err := s.db.CreateInstance(ctx, inst); err != nil {
		return nil, err
	}
	if inst.ReportsTo != "" {
		if err := s.db.SetReportsTo(ctx, inst.ID, inst.ReportsTo); err != nil {
			inst.ReportsTo = ""
		}
	}
	return inst, nil
}

func orgIDsOf(org string) []string {
	if org == "" {
		return nil
	}
	return []string{org}
}

func defaultCapabilities(k protocol.AgentKind) string {
	switch k {
	case protocol.KindClaudeCode:
		return "Claude Code in a real repository: reading, writing and refactoring code, running tests and commands in its folder."
	case protocol.KindCodex:
		return "Codex in a real repository: code changes, tests and commands in its folder."
	case protocol.KindHermes:
		return "Hermes Agent: research, tool use and long-running work with its own tools."
	case protocol.KindOpenClaw:
		return "An OpenClaw agent with its own channels, memory and tools."
	case protocol.KindWebhook:
		return "An external service reached by webhook."
	}
	return ""
}

// validateConnection checks an external agent can actually be reached as
// configured: a CLI kind needs a connected device that has the CLI and
// exposes the folder; a network kind needs a well-formed address.
func (s *Server) validateConnection(ctx context.Context, kind protocol.AgentKind, c *protocol.AgentConnection, ownerID string) error {
	c.Model = strings.TrimSpace(c.Model)
	c.AllowPrivate = false // the server's to decide, below
	switch c.Autonomy {
	case "", "edits", "full":
	default:
		return fmt.Errorf("autonomy is edits or full")
	}
	if c.TimeoutSec < 0 || c.TimeoutSec > 24*3600 {
		return fmt.Errorf("the timeout is between 0 and 86400 seconds")
	}
	if kind.OnDevice() {
		if c.DeviceID == "" {
			return fmt.Errorf("%s runs on a PC: pick a device running `fleetctl host`", kind.Label())
		}
		dev, err := s.db.OafDevice(ctx, c.DeviceID)
		if err != nil || (ownerID != "" && dev.OwnerID != ownerID) {
			return fmt.Errorf("no device %s", c.DeviceID)
		}
		if len(dev.Runtimes) > 0 && !hasString(dev.Runtimes, string(kind)) {
			return fmt.Errorf("%s does not have %s installed (it reported: %s)", dev.Name, kind.Label(), strings.Join(dev.Runtimes, ", "))
		}
		if len(dev.Roots) == 0 {
			return fmt.Errorf("%s exposes no folders; start it with fleetctl host --root <folder>", dev.Name)
		}
		if strings.TrimSpace(c.Cwd) == "" {
			c.Cwd = dev.Roots[0]
		}
		if !underAnyRoot(c.Cwd, dev.Roots) {
			return fmt.Errorf("%s is not inside a folder %s exposes (%s)", c.Cwd, dev.Name, strings.Join(dev.Roots, ", "))
		}
		c.URL, c.AgentID = "", ""
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s needs a URL", kind.Label())
	}
	switch kind {
	case protocol.KindOpenClaw:
		if u.Scheme != "ws" && u.Scheme != "wss" {
			return fmt.Errorf("an OpenClaw gateway address starts with ws:// or wss://")
		}
	case protocol.KindWebhook:
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("a webhook address starts with http:// or https://")
		}
	}
	private, err := checkAgentHost(ctx, u.Hostname(), roleAllows(userFrom(ctx).Role, roleAdmin))
	if err != nil {
		return err
	}
	c.AllowPrivate = private
	c.URL = u.String()
	c.DeviceID, c.Cwd = "", ""
	return nil
}

// checkAgentHost refuses an agent address the orchestrator must never
// connect to (loopback, link-local, cloud metadata), and one on a private
// network unless an admin is setting it. It reports whether the address is
// private, which is what lets the agent's runs dial it.
func checkAgentHost(ctx context.Context, host string, admin bool) (private bool, err error) {
	private, err = external.CheckHost(ctx, host)
	if err != nil {
		return false, err
	}
	if private && !admin {
		return false, fmt.Errorf("%s is on a private network; only an admin can point an agent there", host)
	}
	return private, nil
}

// underAnyRoot reports whether path is one of roots or inside one. Compared
// case-insensitively with either slash, because the roots were reported by a
// PC that may well be Windows.
func underAnyRoot(path string, roots []string) bool {
	norm := func(p string) string {
		p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
		p = strings.TrimRight(filepath.ToSlash(p), "/")
		return strings.ToLower(p)
	}
	p := norm(path)
	for _, r := range roots {
		root := norm(r)
		if p == root || strings.HasPrefix(p, root+"/") {
			return true
		}
	}
	return false
}

func hasString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------- templates ---

func (s *Server) handleExportFleet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := s.db.ListInstances(ctx)
	if err != nil {
		failErr(w, err)
		return
	}
	insts := visibleInstances(accessFrom(ctx), all)
	names := map[string]string{}
	for _, in := range insts {
		names[in.ID] = in.Name
	}
	tpl := protocol.FleetTemplate{Version: 1, Name: r.URL.Query().Get("name"), ExportedAt: time.Now().UTC()}
	for _, in := range insts {
		conn := in.Connection
		// Scrubbed: ids and secrets do not survive the move, and a device is
		// somebody's PC.
		conn.TokenRef, conn.DeviceID, conn.AllowPrivate = "", "", false
		tpl.Agents = append(tpl.Agents, protocol.TemplateAgent{
			Name: in.Name, Kind: in.AgentKindOf(), Title: in.Title, ReportsTo: names[in.ReportsTo],
			Capabilities: in.Capabilities, ArchetypeID: in.ArchetypeID, Tier: in.Tier,
			SystemPrompt: in.SystemPrompt, ShellAccess: in.ShellAccess, Voice: in.Voice,
			BudgetMonth: in.BudgetMonthUSD, BudgetWarn: in.BudgetWarnPct, Trust: in.Trust, Connection: conn,
		})
	}
	w.Header().Set("Content-Disposition", `attachment; filename="fleet-template.json"`)
	writeJSON(w, http.StatusOK, tpl)
}

type importRequest struct {
	Template protocol.FleetTemplate `json:"template"`
	DryRun   bool                   `json:"dry_run"`
	// Rename gives an agent whose name is taken a new one ("Builder 2")
	// instead of skipping it.
	Rename bool `json:"rename"`
}

type importResult struct {
	Created []string `json:"created"`
	Skipped []string `json:"skipped"`
	Renamed []string `json:"renamed"`
	Notes   []string `json:"notes"`
}

func (s *Server) handleImportFleet(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Template.Version != 1 {
		fail(w, http.StatusBadRequest, "unsupported template version")
		return
	}
	ctx := r.Context()
	existing, err := s.db.ListInstances(ctx)
	if err != nil {
		failErr(w, err)
		return
	}
	taken := map[string]bool{}
	for _, in := range existing {
		taken[strings.ToLower(in.Name)] = true
	}
	res := importResult{Created: []string{}, Skipped: []string{}, Renamed: []string{}, Notes: []string{}}
	idByName := map[string]string{}
	for _, in := range existing {
		idByName[strings.ToLower(in.Name)] = in.ID
	}
	type pending struct{ id, reportsTo string }
	var links []pending
	owner := userFrom(ctx).Subject
	for _, a := range req.Template.Agents {
		name := strings.TrimSpace(a.Name)
		if name == "" || !a.Kind.Valid() {
			res.Notes = append(res.Notes, fmt.Sprintf("skipped an agent with no name or an unknown kind %q", a.Kind))
			continue
		}
		if taken[strings.ToLower(name)] {
			if !req.Rename {
				res.Skipped = append(res.Skipped, name)
				continue
			}
			base := name
			for n := 2; taken[strings.ToLower(name)]; n++ {
				name = fmt.Sprintf("%s %d", base, n)
			}
			res.Renamed = append(res.Renamed, base+" → "+name)
		}
		taken[strings.ToLower(name)] = true
		if req.DryRun {
			res.Created = append(res.Created, name)
			continue
		}
		creq := fleet.CreateRequest{
			Name: name, ArchetypeID: a.ArchetypeID, SystemPrompt: a.SystemPrompt, Tier: a.Tier,
			ShellAccess: a.ShellAccess, Voice: a.Voice, OwnerID: owner, Kind: a.Kind, Title: a.Title,
			Capabilities: a.Capabilities, BudgetMonthUSD: a.BudgetMonth, BudgetWarnPct: a.BudgetWarn,
			Trust: a.Trust, Connection: withoutGrants(a.Connection),
		}
		var inst *protocol.Instance
		if a.Kind.External() {
			// A template carries no device and no token: the agent is created
			// pointing at nothing, and is reconnected by editing it.
			inst, err = s.createExternalUnvalidated(ctx, creq)
			res.Notes = append(res.Notes, fmt.Sprintf("%s (%s) needs its connection set before it can take work", name, a.Kind.Label()))
		} else {
			inst, err = s.fleet.CreateAsync(ctx, creq, func(booted *protocol.Instance, _ error) {
				if booted != nil {
					s.bus.Emit("instance.state", booted.ID, "", redact(*booted))
				}
			})
		}
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("could not create %s: %v", name, err))
			continue
		}
		res.Created = append(res.Created, name)
		idByName[strings.ToLower(a.Name)] = inst.ID
		idByName[strings.ToLower(name)] = inst.ID
		if a.ReportsTo != "" {
			links = append(links, pending{inst.ID, a.ReportsTo})
		}
	}
	for _, l := range links {
		mgr := idByName[strings.ToLower(l.reportsTo)]
		if mgr == "" {
			res.Notes = append(res.Notes, "no agent called "+l.reportsTo+" to report to")
			continue
		}
		if err := s.db.SetReportsTo(ctx, l.id, mgr); err != nil {
			res.Notes = append(res.Notes, err.Error())
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// createExternalUnvalidated creates an external agent from a template, whose
// connection cannot be checked because it points at nothing yet.
func (s *Server) createExternalUnvalidated(ctx context.Context, req fleet.CreateRequest) (*protocol.Instance, error) {
	now := time.Now().UTC()
	inst := &protocol.Instance{
		ID: store.NewID(), Name: req.Name, OwnerID: req.OwnerID, SystemPrompt: req.SystemPrompt,
		Tier: "external", State: protocol.InstanceRunning, Kind: req.Kind, Title: req.Title,
		Capabilities: firstNonEmptyStr(req.Capabilities, defaultCapabilities(req.Kind)), Connection: req.Connection,
		BudgetMonthUSD: req.BudgetMonthUSD, BudgetWarnPct: req.BudgetWarnPct, Trust: req.Trust,
		Voice: req.Voice, CreatedAt: now, UpdatedAt: now,
	}
	inst.Connection.DeviceID, inst.Connection.TokenRef = "", ""
	return inst, s.db.CreateInstance(ctx, inst)
}

// withoutGrants drops what a template may not carry into a fleet: whether
// the agent may be reached on a private network is decided when an admin
// sets its address here, not by a file.
func withoutGrants(c protocol.AgentConnection) protocol.AgentConnection {
	c.AllowPrivate = false
	return c
}
