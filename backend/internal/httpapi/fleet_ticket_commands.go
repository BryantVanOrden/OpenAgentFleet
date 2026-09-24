package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The chat's view of the ticket queue and the org chart. The Work board and
// the Org page show the same thing with more room; these exist so a phone in
// a pocket can answer "what is everyone doing" and hand out work in a line.

func (s *Server) cmdTickets(r *http.Request, args string) commandResult {
	ctx := r.Context()
	open, err := s.db.OpenTickets(ctx)
	if err != nil {
		return failed("tickets", err)
	}
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed("tickets", err)
	}
	visible := visibleInstances(accessFrom(ctx), insts)
	names := map[string]string{}
	for _, in := range visible {
		names[in.ID] = in.Name
	}
	var only string
	if ref, _ := splitBotRef(args); ref != "" {
		inst, ok := resolveBotAnyState(visible, ref)
		if !ok {
			return commandResult{Command: "tickets", OK: false, Title: "No such agent",
				Body: fmt.Sprintf("Nobody is called `%s`. `/org` lists the fleet.", ref)}
		}
		only = inst.ID
	}
	var shown []protocol.Ticket
	for _, t := range open {
		if t.AssigneeID != "" && names[t.AssigneeID] == "" {
			continue // assigned to an agent this user cannot see
		}
		if only != "" && t.AssigneeID != only {
			continue
		}
		shown = append(shown, t)
	}
	if len(shown) == 0 {
		return commandResult{Command: "tickets", OK: true, Title: "Tickets",
			Body: "Nothing open. `/ticket @agent <what to do>` hands one out."}
	}
	order := map[protocol.TicketStatus]int{
		protocol.TicketInProgress: 0, protocol.TicketInReview: 1, protocol.TicketBlocked: 2,
		protocol.TicketTodo: 3, protocol.TicketBacklog: 4,
	}
	sort.SliceStable(shown, func(i, j int) bool { return order[shown[i].Status] < order[shown[j].Status] })
	var sb strings.Builder
	const max = 25
	for i, t := range shown {
		if i == max {
			fmt.Fprintf(&sb, "\n…and %d more on the Work board.", len(shown)-max)
			break
		}
		who := names[t.AssigneeID]
		if who == "" {
			who = "nobody"
		}
		line := fmt.Sprintf("- **%s** %s · _%s_ · %s", t.Ref(), clipText(t.Title, 90), strings.ReplaceAll(string(t.Status), "_", " "), who)
		if t.Status == protocol.TicketBlocked && t.BlockedReason != "" {
			line += " — " + clipText(t.BlockedReason, 100)
		}
		sb.WriteString(line + "\n")
	}
	return commandResult{Command: "tickets", OK: true, Title: fmt.Sprintf("Open tickets · %d", len(shown)), Body: sb.String()}
}

func (s *Server) cmdTicket(r *http.Request, args string) commandResult {
	ctx := r.Context()
	botRef, title := splitBotRef(args)
	if botRef == "" || title == "" {
		return commandResult{Command: "ticket", OK: false, Title: "Who, and what?",
			Body: "`/ticket @agent <what to do>`. `/org` lists who is available."}
	}
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed("ticket", err)
	}
	inst, ok := resolveBotAnyState(visibleInstances(accessFrom(ctx), insts), botRef)
	if !ok {
		return commandResult{Command: "ticket", OK: false, Title: "No such agent",
			Body: fmt.Sprintf("Nobody is called `%s`. `/org` lists the fleet.", botRef)}
	}
	if !accessFrom(ctx).Can(protocol.PermChat, inst.OrgIDs, inst.ID) {
		return commandResult{Command: "ticket", OK: false, Title: "Not allowed",
			Body: "You cannot give work to " + inst.Name + "."}
	}
	t := &protocol.Ticket{
		Title: clipText(title, 200), Description: title, AssigneeID: inst.ID,
		Origin: clipText(title, 200), OwnerID: userFrom(ctx).Subject,
	}
	if err := s.tickets.Create(ctx, t, actorFrom(r)); err != nil {
		return failed("ticket", err)
	}
	return commandResult{Command: "ticket", OK: true, Title: t.Ref() + " for " + inst.Name,
		Body: fmt.Sprintf("_%s_\n\nIt starts as soon as %s is free. `/tickets @%s` shows where it stands.",
			clipText(title, 200), inst.Name, inst.Name)}
}

func (s *Server) cmdOrg(r *http.Request) commandResult {
	ctx := r.Context()
	insts, err := s.db.ListInstances(ctx)
	if err != nil {
		return failed("org", err)
	}
	visible := visibleInstances(accessFrom(ctx), insts)
	if len(visible) == 0 {
		return commandResult{Command: "org", OK: true, Title: "Org chart",
			Body: "No agents yet. `/new <archetype> [name]` provisions one."}
	}
	byID := map[string]protocol.Instance{}
	for _, in := range visible {
		byID[in.ID] = in
	}
	reports := map[string][]protocol.Instance{}
	var top []protocol.Instance
	for _, in := range visible {
		if _, ok := byID[in.ReportsTo]; ok && in.ReportsTo != in.ID {
			reports[in.ReportsTo] = append(reports[in.ReportsTo], in)
		} else {
			top = append(top, in)
		}
	}
	byName := func(l []protocol.Instance) {
		sort.Slice(l, func(i, j int) bool { return strings.ToLower(l[i].Name) < strings.ToLower(l[j].Name) })
	}
	byName(top)
	var sb strings.Builder
	sb.WriteString("You\n")
	seen := map[string]bool{}
	var walk func(in protocol.Instance, depth int)
	walk = func(in protocol.Instance, depth int) {
		if seen[in.ID] {
			return
		}
		seen[in.ID] = true
		line := strings.Repeat("  ", depth) + "- **" + in.Name + "**"
		if in.Title != "" {
			line += " · " + in.Title
		}
		if k := in.AgentKindOf(); k != protocol.KindDesktop {
			line += " · _" + k.Label() + "_"
		}
		if in.Hold != "" {
			line += " · held at budget"
		}
		sb.WriteString(line + "\n")
		kids := reports[in.ID]
		byName(kids)
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	for _, in := range top {
		walk(in, 0)
	}
	sb.WriteString("\nDrag agents on the Org page to change who reports to whom.")
	return commandResult{Command: "org", OK: true, Title: fmt.Sprintf("Org chart · %d", len(visible)), Body: sb.String()}
}
