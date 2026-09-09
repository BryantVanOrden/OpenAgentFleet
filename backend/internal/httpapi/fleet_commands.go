package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/pipeline"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The fleet chat's slash commands.
//
// The home screen of both clients is one conversation with the whole fleet.
// Plain text goes to the agents over the peer bus and they divide it up
// between themselves; a line starting with "/" is for the platform. Parsing
// and execution live here, server-side, so the console and the phone offer
// exactly the same verbs with exactly the same behaviour — a command that
// worked on one and not the other would be the parity bug this whole layer
// exists to prevent. Results come back as markdown, which both chats render.

type fleetCommand struct {
	Name        string `json:"name"`
	Usage       string `json:"usage"`
	Description string `json:"description"`
	// Mutating commands are recorded into the fleet channel as a system
	// note, so the chat history shows what was done; queries are not.
	Mutates bool `json:"mutates"`
}

var fleetCommands = []fleetCommand{
	{"help", "/help", "List every command.", false},
	{"setup", "/setup [url] [api-key]", "Find a model engine on this machine (or at an address) and connect it, vision detected automatically.", true},
	{"bots", "/bots", "Who is in the fleet, and what each is doing right now.", false},
	{"status", "/status", "Fleet at a glance: bots, running work, open alerts, missions.", false},
	{"new", "/new <archetype> [name]", "Provision a new agent from an archetype. Takes a minute; it appears in the fleet when ready.", true},
	{"task", "/task @bot <goal>", "Start a bot on a goal. It keeps working across step windows until it says done.", true},
	{"once", "/once @bot <goal>", "Like /task, but one step window only — no continuation.", true},
	{"stop", "/stop @bot", "Cancel whatever a bot is running.", true},
	{"pause", "/pause @bot", "Freeze a bot's desktop (every process stops instantly).", true},
	{"resume", "/resume @bot", "Resume a frozen bot.", true},
	{"mission", "/mission <goal>", "Give the whole running fleet one goal. Each bot gets a role from its archetype, plans first, then executes; artifacts are peer-reviewed.", true},
	{"missions", "/missions", "Every mission and where it stands.", false},
	{"approve", "/approve <artifact-id> [note]", "Approve a mission artifact awaiting review.", true},
	{"reject", "/reject <artifact-id> [note]", "Reject a mission artifact.", true},
	{"alerts", "/alerts", "What agents are waiting on you for.", false},
	{"ack", "/ack <alert-id> [reply]", "Answer an alert — or acknowledge it with no reply — and let the agent resume.", true},
	{"pipelines", "/pipelines", "The saved workflow pipelines.", false},
	{"run", "/run <pipeline>", "Run a pipeline by name or id.", true},
	{"skills", "/skills", "Recorded skills agents can follow.", false},
	{"say", "/say <text>", "Broadcast to every agent (the same as typing without a slash).", true},
	{"goal", "/goal <what to reach>", "In a session: Oaf keeps working toward it, checking in with progress, until it is reached.", true},
	{"loop", "/loop <every> <what to do>", "In a session: Oaf does it on an interval (30s, 5m, 2h, 1d) until cancelled.", true},
	{"jobs", "/jobs", "Goals and loops, and where each stands.", false},
	{"cancel", "/cancel <job-id>", "Stop a goal or loop.", true},
	{"devices", "/devices", "The PCs and phones Oaf can act on, and whether each is online.", false},
	{"sessions", "/sessions", "Your sessions with Oaf.", false},
}

type commandResult struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Title   string `json:"title"`
	// Body is markdown.
	Body string `json:"body"`
}

func (s *Server) handleFleetCommands(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, fleetCommands)
}

func (s *Server) handleFleetCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	name, args := parseCommand(req.Text)
	if name == "" {
		fail(w, http.StatusBadRequest, "a command starts with /")
		return
	}
	res := s.runFleetCommand(r, name, args)

	// The channel keeps a record of what was done, and only of that.
	if res.OK && commandByName(name).Mutates {
		vault.GlobalBus.SendMessageIn(r.Context(), protocol.BroadcastConversationID,
			"system", "Oaf", "broadcast", peerSystemKind,
			"**"+res.Title+"**\n\n"+res.Body, map[string]any{"command": name})
	}
	writeJSON(w, http.StatusOK, res)
}

// parseCommand splits "/task @bob do the thing" into ("task", "@bob do the thing").
func parseCommand(text string) (name, args string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/") {
		return "", ""
	}
	t = strings.TrimPrefix(t, "/")
	name, args, _ = strings.Cut(t, " ")
	return strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(args)
}

func commandByName(name string) fleetCommand {
	for _, c := range fleetCommands {
		if c.Name == name {
			return c
		}
	}
	return fleetCommand{}
}

func (s *Server) runFleetCommand(r *http.Request, name, args string) commandResult {
	ctx := r.Context()
	switch name {
	case "help":
		return helpResult()
	case "setup":
		return s.cmdSetup(r, args)
	case "bots":
		return s.cmdBots(ctx)
	case "status":
		return s.cmdStatus(ctx)
	case "new":
		return s.cmdNew(r, args)
	case "task", "once":
		return s.cmdTask(r, args, name == "once")
	case "stop", "pause", "resume":
		return s.cmdLifecycle(r, name, args)
	case "mission":
		return s.cmdMission(r, args)
	case "missions":
		return s.cmdMissions(ctx)
	case "approve", "reject":
		return s.cmdReview(r, name == "approve", args)
	case "alerts":
		return s.cmdAlerts(ctx)
	case "ack":
		return s.cmdAck(r, args)
	case "pipelines":
		return s.cmdPipelines(ctx)
	case "run":
		return s.cmdRun(ctx, args)
	case "skills":
		return s.cmdSkills(ctx)
	case "say":
		return s.cmdSay(r, args)
	case "goal":
		return s.cmdGoal(r, args)
	case "loop":
		return s.cmdLoop(r, args)
	case "jobs":
		return s.cmdJobs(r)
	case "cancel":
		return s.cmdCancelJob(r, args)
	case "devices":
		return s.cmdDevices(r)
	case "sessions":
		return s.cmdSessions(r)
	}
	return commandResult{Command: name, OK: false, Title: "Unknown command",
		Body: fmt.Sprintf("Oaf doesn't know `/%s`. Try `/help`.", name)}
}

func helpResult() commandResult {
	var sb strings.Builder
	for _, c := range fleetCommands {
		fmt.Fprintf(&sb, "- `%s` — %s\n", c.Usage, c.Description)
	}
	sb.WriteString("\nAnything without a slash goes to the agents. Name one (\"Builder, take the frontend\") " +
		"and it answers first; name nobody and the running fleet divides the work between themselves.")
	return commandResult{Command: "help", OK: true, Title: "Commands", Body: sb.String()}
}

// ------------------------------------------------------------------ queries ---

func (s *Server) cmdBots(ctx context.Context) commandResult {
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed("bots", err)
	}
	if len(insts) == 0 {
		return commandResult{Command: "bots", OK: true, Title: "Fleet",
			Body: "No agents yet. `/new <archetype> [name]` provisions one — `/help` lists the rest."}
	}
	var sb strings.Builder
	for _, inst := range insts {
		role := inst.ArchetypeID
		if t := protocol.BotTemplateByID(inst.ArchetypeID); t != nil {
			role = t.Name
		}
		if role == "" {
			role = "general purpose"
		}
		line := fmt.Sprintf("- **%s** · %s · %s", inst.Name, role, inst.State)
		if t := s.liveTask(ctx, inst.ID); t != nil {
			line += fmt.Sprintf(" — working: _%s_ (step %d/%d)", clipText(t.Goal, 80), t.Step, t.MaxSteps)
		}
		sb.WriteString(line + "\n")
	}
	return commandResult{Command: "bots", OK: true, Title: fmt.Sprintf("Fleet · %d agents", len(insts)), Body: sb.String()}
}

func (s *Server) cmdStatus(ctx context.Context) commandResult {
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed("status", err)
	}
	running, working := 0, 0
	for _, inst := range insts {
		if inst.State == protocol.InstanceRunning {
			running++
			if s.liveTask(ctx, inst.ID) != nil {
				working++
			}
		}
	}
	alerts, _ := s.db.ListAlerts(ctx, true, 100)
	waiting := 0
	for _, a := range alerts {
		if a.NeedsReply && a.ResolvedAt == nil {
			waiting++
		}
	}
	missions := 0
	if globalSwarmCoordinator != nil {
		missions = len(globalSwarmCoordinator.ListSwarms(ctx))
	}
	body := fmt.Sprintf("- **%d** agents, %d running, %d working right now\n- **%d** alerts waiting on you\n- **%d** missions",
		len(insts), running, working, waiting, missions)
	if waiting > 0 {
		body += "\n\n`/alerts` to see who needs you."
	}
	return commandResult{Command: "status", OK: true, Title: "Fleet status", Body: body}
}

func (s *Server) cmdAlerts(ctx context.Context) commandResult {
	alerts, err := s.db.ListAlerts(ctx, true, 50)
	if err != nil {
		return failed("alerts", err)
	}
	var sb strings.Builder
	n := 0
	for _, a := range alerts {
		if !a.NeedsReply || a.ResolvedAt != nil {
			continue
		}
		n++
		fmt.Fprintf(&sb, "- `%s` **%s** — %s\n", a.ID, a.Title, clipText(a.Body, 160))
	}
	if n == 0 {
		return commandResult{Command: "alerts", OK: true, Title: "Alerts",
			Body: "Oaf has nothing for you — no agent is waiting on a reply."}
	}
	sb.WriteString("\n`/ack <alert-id> <reply>` answers one; `/ack <alert-id>` alone acknowledges without instruction.")
	return commandResult{Command: "alerts", OK: true, Title: fmt.Sprintf("Waiting on you · %d", n), Body: sb.String()}
}

func (s *Server) cmdPipelines(ctx context.Context) commandResult {
	list := pipeline.GlobalEngine.ListPipelines(ctx)
	if len(list) == 0 {
		return commandResult{Command: "pipelines", OK: true, Title: "Pipelines", Body: "None saved yet."}
	}
	var sb strings.Builder
	for _, p := range list {
		fmt.Fprintf(&sb, "- **%s** · %d stages · `%s`\n", p.Name, len(p.Nodes), p.ID)
	}
	sb.WriteString("\n`/run <name>` starts one.")
	return commandResult{Command: "pipelines", OK: true, Title: "Pipelines", Body: sb.String()}
}

func (s *Server) cmdSkills(ctx context.Context) commandResult {
	skills, err := s.db.ListSkills(ctx)
	if err != nil {
		return failed("skills", err)
	}
	if len(skills) == 0 {
		return commandResult{Command: "skills", OK: true, Title: "Skills",
			Body: "No recorded skills yet. Record one from any bot's desktop: do the task once with the recorder on."}
	}
	var sb strings.Builder
	for _, sk := range skills {
		fmt.Fprintf(&sb, "- **%s** v%d · %d steps\n", sk.Name, sk.Version, len(sk.Steps))
	}
	return commandResult{Command: "skills", OK: true, Title: fmt.Sprintf("Skills · %d", len(skills)), Body: sb.String()}
}

func (s *Server) cmdMissions(ctx context.Context) commandResult {
	if globalSwarmCoordinator == nil {
		return commandResult{Command: "missions", OK: true, Title: "Missions", Body: "None."}
	}
	swarms := globalSwarmCoordinator.ListSwarms(ctx)
	if len(swarms) == 0 {
		return commandResult{Command: "missions", OK: true, Title: "Missions",
			Body: "None yet. `/mission <goal>` gives the whole running fleet one."}
	}
	var sb strings.Builder
	for _, sw := range swarms {
		fmt.Fprintf(&sb, "### %s · %s%s\n_%s_\n", sw.Name, sw.Status, phaseSuffix(sw.Phase), clipText(sw.Mission, 200))
		for _, m := range sw.Members {
			fmt.Fprintf(&sb, "- %s — %s (%s)\n", m.InstanceName, m.Role, m.Status)
		}
		for _, a := range sw.Artifacts {
			state := "awaiting review"
			if len(a.ApprovedBy) > 0 {
				state = "approved by " + strings.Join(a.ApprovedBy, ", ")
			}
			fmt.Fprintf(&sb, "- 📄 **%s** by %s · %s · `%s`\n", a.Title, a.Author, state, a.ID)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("`/approve <artifact-id>` or `/reject <artifact-id>` reviews an artifact.")
	return commandResult{Command: "missions", OK: true, Title: fmt.Sprintf("Missions · %d", len(swarms)), Body: sb.String()}
}

func phaseSuffix(phase string) string {
	if phase == "" {
		return ""
	}
	return " · " + phase
}

// ---------------------------------------------------------------- mutations ---

func (s *Server) cmdNew(r *http.Request, args string) commandResult {
	ctx := r.Context()
	archetype, name, _ := strings.Cut(args, " ")
	archetype = strings.TrimSpace(archetype)
	name = strings.TrimSpace(name)
	if archetype == "" {
		var sb strings.Builder
		for _, t := range protocol.DefaultBotTemplates() {
			fmt.Fprintf(&sb, "- `%s` — %s\n", t.ID, t.Name)
		}
		return commandResult{Command: "new", OK: false, Title: "Which archetype?",
			Body: "`/new <archetype> [name]`. Archetypes:\n" + sb.String()}
	}
	tmpl := protocol.BotTemplateByID(archetype)
	if tmpl == nil {
		// Loose match on the name too: "/new cybersec" should work.
		for _, t := range protocol.DefaultBotTemplates() {
			if strings.Contains(normalizeName(t.Name), normalizeName(archetype)) {
				tmpl = protocol.BotTemplateByID(t.ID)
				break
			}
		}
	}
	if tmpl == nil {
		return commandResult{Command: "new", OK: false, Title: "Unknown archetype",
			Body: fmt.Sprintf("No archetype called `%s`. `/new` alone lists them.", archetype)}
	}
	if !accessFrom(ctx).CanInOrg(protocol.PermCreate, "") {
		return commandResult{Command: "new", OK: false, Title: "Not allowed", Body: "You cannot create bots."}
	}
	req := fleet.CreateRequest{Name: name, ArchetypeID: tmpl.ID, Tier: tmpl.RecommendedTier,
		OwnerID: userFrom(ctx).Subject}

	// Provisioning blocks until the desktop answers — a minute, more for a
	// tool-heavy archetype — so it runs on, detached, and the fleet channel
	// gets a note when the machine is up (or why it is not).
	go func() {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Minute)
		defer cancel()
		inst, err := s.fleet.Create(bctx, req)
		switch {
		case err != nil && inst != nil:
			s.systemNote(bctx, fmt.Sprintf("**%s** did not come up cleanly: %s", inst.Name, err.Error()))
		case err != nil:
			s.systemNote(bctx, "A new "+tmpl.Name+" could not be provisioned: "+err.Error())
		default:
			s.bus.Emit("instance.state", inst.ID, "", redact(*inst))
			s.systemNote(bctx, fmt.Sprintf("**%s** is up — a %s, ready for work.", inst.Name, tmpl.Name))
		}
	}()
	label := name
	if label == "" {
		label = "a new " + tmpl.Name
	}
	return commandResult{Command: "new", OK: true, Title: "Provisioning " + label,
		Body: "Building the desktop now. It appears in the fleet when it answers — Oaf will say so here."}
}

// systemNote writes a platform note into the fleet channel. Agents never
// answer these (peerSystemKind).
func (s *Server) systemNote(ctx context.Context, body string) {
	vault.GlobalBus.SendMessageIn(ctx, protocol.BroadcastConversationID,
		"system", "Oaf", "broadcast", peerSystemKind, body, nil)
}

func (s *Server) cmdTask(r *http.Request, args string, once bool) commandResult {
	ctx := r.Context()
	cmd := "task"
	if once {
		cmd = "once"
	}
	botRef, goal := splitBotRef(args)
	if botRef == "" || goal == "" {
		return commandResult{Command: cmd, OK: false, Title: "Who, and what?",
			Body: fmt.Sprintf("`/%s @bot <goal>`. `/bots` lists who is available.", cmd)}
	}
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed(cmd, err)
	}
	inst, ok := resolveBotByName(insts, botRef, "")
	if !ok {
		return commandResult{Command: cmd, OK: false, Title: "No such bot",
			Body: fmt.Sprintf("Nobody running is called `%s`. `/bots` lists the fleet.", botRef)}
	}
	acc := accessFrom(ctx)
	if !acc.Can(protocol.PermChat, inst.OrgIDs, inst.ID) {
		return commandResult{Command: cmd, OK: false, Title: "Not allowed",
			Body: "You cannot start work on " + inst.Name + "."}
	}
	if t := s.liveTask(ctx, inst.ID); t != nil {
		return commandResult{Command: cmd, OK: false, Title: inst.Name + " is busy",
			Body: fmt.Sprintf("Already working on _%s_ (step %d/%d). `/stop @%s` first, or pick another bot.",
				clipText(t.Goal, 80), t.Step, t.MaxSteps, inst.Name)}
	}
	task := &protocol.Task{
		ID: store.NewID(), InstanceID: inst.ID, OwnerID: userFrom(ctx).Subject,
		Goal: goal, State: protocol.TaskQueued, MaxSteps: s.cfg.MaxSteps,
		CreatedAt: time.Now().UTC(),
	}
	if once {
		task.Params = map[string]string{protocol.ParamOnce: "true"}
	}
	if err := s.db.CreateTask(ctx, task); err != nil {
		return failed(cmd, err)
	}
	if err := s.runner.Start(ctx, task); err != nil {
		_ = s.db.UpdateTaskState(ctx, task.ID, protocol.TaskFailed, 0, err.Error(), "")
		return failed(cmd, err)
	}
	how := "It keeps working across step windows until it says done."
	if once {
		how = "One step window only."
	}
	return commandResult{Command: cmd, OK: true, Title: inst.Name + " is on it",
		Body: fmt.Sprintf("_%s_\n\n%s Watch it live from Fleet → %s.", goal, how, inst.Name)}
}

func (s *Server) cmdLifecycle(r *http.Request, verb, args string) commandResult {
	ctx := r.Context()
	botRef, _ := splitBotRef(args)
	if botRef == "" {
		return commandResult{Command: verb, OK: false, Title: "Which bot?", Body: fmt.Sprintf("`/%s @bot`", verb)}
	}
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed(verb, err)
	}
	// Lifecycle verbs apply to bots in any state — a paused bot is what
	// /resume is for — so the resolver is asked without the running filter.
	inst, ok := resolveBotAnyState(insts, botRef)
	if !ok {
		return commandResult{Command: verb, OK: false, Title: "No such bot", Body: fmt.Sprintf("Nobody is called `%s`.", botRef)}
	}
	acc := accessFrom(ctx)
	switch verb {
	case "stop":
		if !acc.Can(protocol.PermChat, inst.OrgIDs, inst.ID) {
			return commandResult{Command: verb, OK: false, Title: "Not allowed", Body: "You cannot stop work on " + inst.Name + "."}
		}
		t := s.liveTask(ctx, inst.ID)
		if t == nil {
			return commandResult{Command: verb, OK: true, Title: inst.Name + " is idle", Body: "Nothing to stop."}
		}
		s.runner.Cancel(t.ID)
		return commandResult{Command: verb, OK: true, Title: "Stopped " + inst.Name,
			Body: fmt.Sprintf("Cancelled _%s_ at step %d.", clipText(t.Goal, 80), t.Step)}
	case "pause":
		if !acc.Can(protocol.PermEdit, inst.OrgIDs, inst.ID) {
			return commandResult{Command: verb, OK: false, Title: "Not allowed", Body: "You cannot pause " + inst.Name + "."}
		}
		if err := s.fleet.Pause(ctx, inst.ID); err != nil {
			return failed(verb, err)
		}
		return commandResult{Command: verb, OK: true, Title: "Froze " + inst.Name, Body: "Every process on its desktop is stopped. `/resume @" + inst.Name + "` to continue."}
	default: // resume
		if !acc.Can(protocol.PermEdit, inst.OrgIDs, inst.ID) {
			return commandResult{Command: verb, OK: false, Title: "Not allowed", Body: "You cannot resume " + inst.Name + "."}
		}
		if err := s.fleet.Resume(ctx, inst.ID); err != nil {
			return failed(verb, err)
		}
		return commandResult{Command: verb, OK: true, Title: "Resumed " + inst.Name, Body: "Its desktop is running again."}
	}
}

func (s *Server) cmdMission(r *http.Request, goal string) commandResult {
	ctx := r.Context()
	if strings.TrimSpace(goal) == "" {
		return commandResult{Command: "mission", OK: false, Title: "What is the mission?",
			Body: "`/mission <goal>` — every running bot gets a role and a share of it."}
	}
	if globalSwarmCoordinator == nil {
		return commandResult{Command: "mission", OK: false, Title: "Missions unavailable", Body: "The coordinator is not running."}
	}
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed("mission", err)
	}
	acc := accessFrom(ctx)
	var members []protocol.SwarmMember
	for _, inst := range insts {
		if inst.State != protocol.InstanceRunning || !acc.Can(protocol.PermChat, inst.OrgIDs, inst.ID) {
			continue
		}
		if s.liveTask(ctx, inst.ID) != nil {
			continue // already committed elsewhere; a mission does not hijack it
		}
		role := "Contributor"
		if t := protocol.BotTemplateByID(inst.ArchetypeID); t != nil {
			role = t.Name
		}
		members = append(members, protocol.SwarmMember{
			InstanceID: inst.ID, InstanceName: inst.Name, Role: role, ArchetypeID: inst.ArchetypeID,
		})
	}
	if len(members) == 0 {
		return commandResult{Command: "mission", OK: false, Title: "Nobody is free",
			Body: "Every running bot is already working. `/bots` shows who; `/stop @bot` frees one, or `/new` adds one."}
	}
	name := "Mission: " + clipText(goal, 48)
	sw, err := globalSwarmCoordinator.CreateSwarm(ctx, name, goal, members, true)
	if err != nil {
		return failed("mission", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "_%s_\n\nTeam:\n", goal)
	for _, m := range sw.Members {
		fmt.Fprintf(&sb, "- **%s** — %s\n", m.InstanceName, m.Role)
	}
	sb.WriteString("\nEach bot plans its part first; execution starts when every plan is in. " +
		"Artifacts they publish are reviewed by their teammates and show under `/missions`, " +
		"where you can `/approve` or `/reject` them.")
	return commandResult{Command: "mission", OK: true, Title: fmt.Sprintf("Mission launched · %d bots", len(sw.Members)), Body: sb.String()}
}

func (s *Server) cmdReview(r *http.Request, approved bool, args string) commandResult {
	ctx := r.Context()
	verb := "reject"
	if approved {
		verb = "approve"
	}
	artifactID, note, _ := strings.Cut(strings.TrimSpace(args), " ")
	if artifactID == "" {
		return commandResult{Command: verb, OK: false, Title: "Which artifact?",
			Body: fmt.Sprintf("`/%s <artifact-id> [note]` — ids are listed under `/missions`.", verb)}
	}
	if globalSwarmCoordinator == nil {
		return commandResult{Command: verb, OK: false, Title: "Missions unavailable", Body: "The coordinator is not running."}
	}
	for _, sw := range globalSwarmCoordinator.ListSwarms(ctx) {
		for _, a := range sw.Artifacts {
			if a.ID != artifactID {
				continue
			}
			if note == "" {
				note = verb + "d in the fleet chat"
			}
			if _, err := globalSwarmCoordinator.ReviewArtifact(ctx, sw.ID, a.ID, "operator", approved, note); err != nil {
				return failed(verb, err)
			}
			return commandResult{Command: verb, OK: true, Title: strings.Title(verb) + "d “" + a.Title + "”",
				Body: fmt.Sprintf("By %s, in mission _%s_.", a.Author, sw.Name)}
		}
	}
	return commandResult{Command: verb, OK: false, Title: "No such artifact",
		Body: fmt.Sprintf("Nothing has id `%s`. `/missions` lists them.", artifactID)}
}

func (s *Server) cmdAck(r *http.Request, args string) commandResult {
	ctx := r.Context()
	alertID, reply, _ := strings.Cut(strings.TrimSpace(args), " ")
	if alertID == "" {
		return commandResult{Command: "ack", OK: false, Title: "Which alert?",
			Body: "`/ack <alert-id> [reply]` — ids are listed under `/alerts`."}
	}
	if err := s.db.ResolveAlert(ctx, alertID, strings.TrimSpace(reply)); err != nil {
		return failed("ack", err)
	}
	s.bus.Emit("alert.resolved", "", "", map[string]string{"alert_id": alertID, "by": userFrom(ctx).Email})
	if strings.TrimSpace(reply) == "" {
		return commandResult{Command: "ack", OK: true, Title: "Acknowledged", Body: "The agent resumes without further instruction."}
	}
	return commandResult{Command: "ack", OK: true, Title: "Sent — the agent is resuming", Body: "_" + reply + "_"}
}

func (s *Server) cmdRun(ctx context.Context, ref string) commandResult {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return commandResult{Command: "run", OK: false, Title: "Which pipeline?", Body: "`/run <name or id>` — `/pipelines` lists them."}
	}
	var match *protocol.WorkflowPipeline
	for _, p := range pipeline.GlobalEngine.ListPipelines(ctx) {
		p := p
		if p.ID == ref || normalizeName(p.Name) == normalizeName(ref) {
			match = &p
			break
		}
	}
	if match == nil {
		return commandResult{Command: "run", OK: false, Title: "No such pipeline", Body: fmt.Sprintf("Nothing called `%s`. `/pipelines` lists them.", ref)}
	}
	run, err := pipeline.GlobalEngine.TriggerRun(ctx, match.ID)
	if err != nil {
		return failed("run", err)
	}
	return commandResult{Command: "run", OK: true, Title: "Running " + match.Name,
		Body: fmt.Sprintf("%d stages · run `%s`. Progress shows under Pipelines.", len(match.Nodes), run.ID)}
}

func (s *Server) cmdSay(r *http.Request, text string) commandResult {
	if strings.TrimSpace(text) == "" {
		return commandResult{Command: "say", OK: false, Title: "Say what?", Body: "`/say <text>`"}
	}
	sp := s.speakerOf(r)
	name := sp.Name
	if name == "" {
		name = "Operator"
	}
	vault.GlobalBus.SendMessageAs(r.Context(), protocol.BroadcastConversationID, "", name, sp.ID,
		"broadcast", "message", strings.TrimSpace(text), nil)
	// Not a mutation to record: the message itself is already in the channel.
	return commandResult{Command: "say", OK: true, Title: "Sent to the fleet", Body: ""}
}

// ------------------------------------------------------------------ helpers ---

func failed(cmd string, err error) commandResult {
	return commandResult{Command: cmd, OK: false, Title: "That did not work", Body: err.Error()}
}

// splitBotRef pulls a leading "@name" (or bare first word) off the arguments.
// Names with spaces are addressed with the @ form and quotes are not needed:
// "@Tool Check build it" resolves through the loose matcher on the first two
// words if the first alone matches nobody — callers get the best-effort split.
func splitBotRef(args string) (bot, rest string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	first, remainder, _ := strings.Cut(args, " ")
	return strings.TrimPrefix(first, "@"), strings.TrimSpace(remainder)
}

func resolveBotAnyState(instances []protocol.Instance, raw string) (protocol.Instance, bool) {
	want := normalizeName(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	var best protocol.Instance
	bestDist, found := 3, false
	for _, inst := range instances {
		have := normalizeName(inst.Name)
		if have == want {
			return inst, true
		}
		if d := editDistance(have, want); d < bestDist {
			best, bestDist, found = inst, d, true
		}
	}
	return best, found
}

// liveTask is the task currently occupying an instance, if any.
func (s *Server) liveTask(ctx context.Context, instanceID string) *protocol.Task {
	tasks, err := s.db.ListTasks(ctx, instanceID, 20)
	if err != nil {
		return nil
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	for i := range tasks {
		switch tasks[i].State {
		case protocol.TaskRunning, protocol.TaskQueued, protocol.TaskAwaitingHuman:
			return &tasks[i]
		}
	}
	return nil
}

var _ = strconv.Itoa // keep strconv available for future numeric args
