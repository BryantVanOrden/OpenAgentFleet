package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/tickets"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Filing the parts of a fleet request as tickets.
//
// The operator asks the fleet for something; each agent answers with the part
// it will take. Every part becomes a ticket under one ticket for the request
// itself, and what it waits on is decided once, here, from its stage:
//
//	design   -> blocks the builds that have not started
//	build    -> waits for design; its testers become its reviewers
//	test     -> reviews the builds (a review ticket per round, with a verdict)
//	review   -> verifies the whole request when it comes to rest
//
// After that nothing is read from chat. A hand-off is a blocker finishing, a
// review that finds problems sends the build back with the findings, and a
// restart loses nothing.

var fleetFiling sync.Mutex

// fileFleetPart files one agent's part of an operator request.
func (s *Server) fileFleetPart(ctx context.Context, inst protocol.Instance, msg protocol.PeerMessage, part string, stage relayStage, namedFirst bool) {
	fleetFiling.Lock()
	defer fleetFiling.Unlock()
	bg := context.WithoutCancel(ctx)

	root, err := s.requestRoot(bg, msg)
	if err != nil {
		s.log.Warn("could not file the request as a ticket", "err", err)
		return
	}
	kids, _ := s.db.Children(bg, root.ID)
	var siblings []protocol.Ticket
	for _, k := range kids {
		if k.Status == protocol.TicketCancelled {
			continue
		}
		if k.AssigneeID == inst.ID {
			return // this agent already has its part of this request
		}
		siblings = append(siblings, k)
	}
	plan := planPart(siblings, stage, namedFirst, root.VerifierID != "")
	actor := tickets.Actor{Name: "Oaf"}

	if plan.verifier {
		v := inst.ID
		if _, err := s.tickets.Update(bg, root.ID, tickets.Patch{VerifierID: &v}, actor); err == nil {
			s.reviewBrief(bg, root.ID, inst, part)
			s.log.Info("agent will verify the request when it comes to rest", "instance", inst.Name, "ticket", root.Ref())
		}
		return
	}
	for _, id := range plan.reviewFor {
		p := tickets.Patch{ReviewerID: &inst.ID}
		if t, err := s.db.Ticket(bg, id); err == nil && t.Status == protocol.TicketDone {
			// Finished before its reviewer arrived: into review now.
			st := protocol.TicketInReview
			p.Status = &st
		}
		if _, err := s.tickets.Update(bg, id, p, actor); err == nil {
			s.reviewBrief(bg, id, inst, part)
		}
	}
	if len(plan.reviewFor) > 0 && !plan.alsoWork {
		s.log.Info("agent will review the builds of this request", "instance", inst.Name, "reviews", len(plan.reviewFor))
		return
	}

	t := &protocol.Ticket{
		Title:       partTitle(inst, part),
		Description: partDescription(inst, part),
		Kind:        plan.kind,
		Status:      plan.status,
		ParentID:    root.ID,
		AssigneeID:  inst.ID,
		OwnerID:     root.OwnerID,
		Thread:      root.Thread,
		Origin:      root.Origin,
		Stage:       stage.String(),
		BlockedBy:   plan.blockedBy,
	}
	if err := s.tickets.Create(bg, t, actor); err != nil {
		s.log.Warn("could not file an agent's part", "instance", inst.Name, "err", err)
		return
	}
	for _, ph := range plan.adopt {
		// A tester that answered before any builder: it reviews this build.
		rid := ph.AssigneeID
		if _, err := s.tickets.Update(bg, t.ID, tickets.Patch{ReviewerID: &rid}, actor); err == nil {
			if rv, err := s.db.Instance(bg, rid); err == nil {
				s.reviewBrief(bg, t.ID, *rv, ph.Description)
			}
			_ = s.tickets.Delete(bg, ph.ID)
		}
	}
	for _, id := range plan.blockExisting {
		if ex, err := s.db.Ticket(bg, id); err == nil {
			bs := append(append([]string{}, ex.BlockedBy...), t.ID)
			_, _ = s.tickets.Update(bg, id, tickets.Patch{BlockedBy: &bs}, actor)
		}
	}
	s.log.Info("agent's part filed as a ticket", "instance", inst.Name, "ticket", t.Ref(),
		"stage", stage.String(), "status", t.Status, "waits_on", len(t.BlockedBy))
}

// partPlan is what filing one part does.
type partPlan struct {
	kind      protocol.TicketKind
	status    protocol.TicketStatus
	blockedBy []string
	// reviewFor are builds this agent reviews; alsoWork files a ticket of its
	// own besides (a second tester, after the builds).
	reviewFor []string
	alsoWork  bool
	// verifier makes the agent the request's verifier.
	verifier bool
	// adopt are review placeholders a new build takes as its reviewers.
	adopt []protocol.Ticket
	// blockExisting are unstarted tickets that now wait on the new one.
	blockExisting []string
}

func isProducerStage(st string) bool {
	return st == stageBuild.String() || st == stageUnknown.String() || st == ""
}

// planPart decides, from the request's other parts and this part's stage,
// what the new ticket waits on and who reviews what. Pure, so every rule is
// tested without a database.
func planPart(siblings []protocol.Ticket, stage relayStage, namedFirst, verifierSet bool) partPlan {
	p := partPlan{kind: protocol.TicketWork, status: protocol.TicketTodo}
	live := func(t protocol.Ticket) bool { return !t.Status.Terminal() }
	unstarted := func(t protocol.Ticket) bool {
		return (t.Status == protocol.TicketTodo || t.Status == protocol.TicketBacklog) && t.TaskID == ""
	}

	// Whoever the operator named first starts: what it needs is already in
	// the catalog or was never coming. "ToolCheck test the rollr app, Builder
	// fix what it finds" names a tester first, pointing at work that exists.
	if namedFirst && (stage == stageTest || stage == stageReview) {
		return p
	}

	switch stage {
	case stageDesign:
		for _, t := range siblings {
			if t.Kind == protocol.TicketWork && isProducerStage(t.Stage) && unstarted(t) {
				p.blockExisting = append(p.blockExisting, t.ID)
			}
		}
	case stageReview:
		if !verifierSet {
			p.verifier = true
			return p
		}
		fallthrough
	case stageTest:
		var builds []protocol.Ticket
		for _, t := range siblings {
			if t.Kind == protocol.TicketWork && isProducerStage(t.Stage) && t.Status != protocol.TicketCancelled {
				builds = append(builds, t)
			}
		}
		if len(builds) == 0 {
			// Nothing to review yet: park a placeholder the first build adopts.
			p.kind, p.status = protocol.TicketReview, protocol.TicketBacklog
			return p
		}
		for _, b := range builds {
			if b.ReviewerID == "" {
				p.reviewFor = append(p.reviewFor, b.ID)
			}
		}
		if len(p.reviewFor) == 0 {
			// Every build has a reviewer already: this agent tests after them.
			p.alsoWork = true
			for _, b := range builds {
				if live(b) {
					p.blockedBy = append(p.blockedBy, b.ID)
				}
			}
		}
		return p
	default: // build, and anything the scorer could not place
		for _, t := range siblings {
			if t.Kind == protocol.TicketWork && t.Stage == stageDesign.String() && live(t) {
				p.blockedBy = append(p.blockedBy, t.ID)
			}
			if t.Kind == protocol.TicketReview && t.Status == protocol.TicketBacklog && t.TargetID == "" {
				p.adopt = append(p.adopt, t)
			}
		}
	}
	return p
}

// requestRoot is the ticket for an operator request: one per request,
// found again when the second agent answers it.
func (s *Server) requestRoot(ctx context.Context, msg protocol.PeerMessage) (*protocol.Ticket, error) {
	conv := msg.ConversationID
	if conv == "" {
		conv = vault.GlobalBus.DefaultChannel()
	}
	roots, err := s.db.ListTickets(ctx, protocol.TicketFilter{RootsOnly: true, Limit: 50})
	if err != nil {
		return nil, err
	}
	for i := range roots {
		r := &roots[i]
		if r.Origin == msg.Content && r.Thread == conv && time.Since(r.CreatedAt) < 24*time.Hour {
			return r, nil
		}
	}
	root := &protocol.Ticket{
		Title:       clipLine(firstSentence(msg.Content), 90),
		Description: msg.Content,
		Kind:        protocol.TicketWork,
		Status:      protocol.TicketTodo,
		Origin:      msg.Content,
		Thread:      conv,
		OwnerID:     msg.FromUserID,
	}
	if err := s.tickets.Create(ctx, root, tickets.Actor{UserID: msg.FromUserID, Name: firstNonEmptyStr(msg.FromInstanceName, "the operator")}); err != nil {
		return nil, err
	}
	return root, nil
}

// reviewBrief records how a reviewer said it would check a ticket, so the
// review ticket it gets later carries the operator's instructions.
func (s *Server) reviewBrief(ctx context.Context, ticketID string, reviewer protocol.Instance, part string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return
	}
	// "Checker reviews it" leaves "reviews it", which tells a reviewer
	// nothing about what to check; the operator's whole request does.
	if len(strings.Fields(part)) < 8 {
		if t, err := s.db.Ticket(ctx, ticketID); err == nil && strings.TrimSpace(t.Origin) != "" && t.Origin != part {
			part += "\n\nThe request, in the operator's words: " + strings.TrimSpace(t.Origin)
		}
	}
	_ = s.db.AddTicketComment(ctx, &protocol.TicketComment{
		TicketID: ticketID, AuthorID: reviewer.ID, AuthorName: reviewer.Name, Kind: "review_brief", Body: part,
	})
}

func partTitle(inst protocol.Instance, part string) string {
	t := clipLine(firstSentence(part), 80)
	if t == "" {
		t = inst.Name + "'s part"
	}
	return t
}

func partDescription(inst protocol.Instance, part string) string {
	var b strings.Builder
	b.WriteString("Your part, which you chose: " + strings.TrimSpace(part) + "\n\n")
	b.WriteString(catalogGuidance)
	b.WriteString(" Publish what you produce with publish_work (work_name + path) so the rest of the fleet can build on it.\n\n")
	if inst.AgentKindOf().External() {
		b.WriteString("You work in your own folder. Colleagues cannot see your files unless you publish_work them or say where they are.\n")
	} else {
		b.WriteString(fmt.Sprintf("Every bot has its own machine. Your files are on yours only; a colleague cannot see them unless you publish_work them or serve them. Your machine is reachable to colleagues as http://%s:<port> (or http://%s:<port>): if you run a web server, say that address and port in your report.\n",
			tickets.SandboxHost(inst.ID), strings.ToLower(inst.Name)))
	}
	return b.String()
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{". ", "\n", "! ", "? "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(s), ".")
}

// fileFromPeerRequest turns a colleague's request that an agent agreed to
// into a ticket under the asker's own ticket, which then waits for it. The
// asker's reviewer is not given a second copy of the review it will get when
// the asker finishes.
func (s *Server) fileFromPeerRequest(ctx context.Context, inst protocol.Instance, msg protocol.PeerMessage, plan string) {
	bg := context.WithoutCancel(ctx)
	held, err := s.db.ListTickets(bg, protocol.TicketFilter{AssigneeID: msg.FromInstanceID, Status: []protocol.TicketStatus{protocol.TicketInProgress}, Limit: 1})
	if err != nil || len(held) == 0 || held[0].TaskID == "" {
		return // the asker is not working a ticket: a question, not a job
	}
	asking := held[0]
	if asking.ReviewerID == inst.ID {
		s.log.Info("not filing a peer request; this agent reviews that work when it finishes",
			"instance", inst.Name, "ticket", asking.Ref())
		return
	}
	// Only for agents on the job: part of the request's tree, or named in it.
	chain, _ := s.db.Ancestry(bg, asking.ID)
	root := asking
	if len(chain) > 0 {
		root = chain[0]
	}
	onJob := false
	if tree, err := s.db.Subtree(bg, root.ID); err == nil {
		for _, t := range tree {
			if t.AssigneeID == inst.ID || t.ReviewerID == inst.ID || t.VerifierID == inst.ID {
				onJob = true
			}
		}
	}
	if _, _, named := mentionSpan(root.Origin, inst.Name); named {
		onJob = true
	}
	if !onJob {
		s.log.Info("not filing a peer request; not on this job", "instance", inst.Name, "asker", msg.FromInstanceName)
		return
	}
	kids, _ := s.db.Children(bg, asking.ID)
	if len(kids) >= maxRelayRounds {
		s.log.Info("not filing a peer request; the ticket has had enough of them", "ticket", asking.Ref())
		return
	}
	asker, err := s.db.Instance(bg, msg.FromInstanceID)
	if err != nil {
		return
	}
	text := fmt.Sprintf("%s asked you:\n%s\n\nYou answered: %s\n\nDo that now. When it is done, publish what you produced with publish_work under a stable name and say in your report exactly where it is.",
		asker.Name, clipLine(msg.Content, 800), clipLine(plan, 600))
	out, err := s.tickets.CreateFromAgent(bg, asking.TaskID, asker, inst.Name, clipLine(firstSentence(plan), 80), text, true)
	if err != nil {
		s.log.Warn("could not file a peer request", "instance", inst.Name, "err", err)
		return
	}
	s.log.Info("agent started work a colleague asked for", "instance", inst.Name, "asker", asker.Name, "result", clipLine(out, 120))
}
