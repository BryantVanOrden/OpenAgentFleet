package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/pipeline"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/tickets"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Pipelines and missions file their work as tickets.
//
// They used to start bare tasks, which put their work outside everything the
// ticket engine does: no place on the Work board, no budget holds, no retry
// of a run that failed for reasons that had nothing to do with the work, no
// ownerless checks, no Claude Code or Codex agent able to take a stage. A
// pipeline run or a mission is now a ticket, and each stage or member's part
// a ticket under it that the engine runs like any other.

var groupTickets sync.Map // group key -> ticket id

// groupTicket finds or files the ticket a run's work sits under. Keyed by its
// stage ("pipeline:<run id>", "mission:<name>"), so a restart finds it again.
func (s *Server) groupTicket(ctx context.Context, key, title string) (*protocol.Ticket, error) {
	if id, ok := groupTickets.Load(key); ok {
		if t, err := s.db.Ticket(ctx, id.(string)); err == nil && t.Status != protocol.TicketCancelled {
			return t, nil
		}
	}
	roots, err := s.db.ListTickets(ctx, protocol.TicketFilter{RootsOnly: true, Limit: 500})
	if err == nil {
		for i := range roots {
			if roots[i].Stage == key && roots[i].Status != protocol.TicketCancelled {
				groupTickets.Store(key, roots[i].ID)
				return &roots[i], nil
			}
		}
	}
	t := &protocol.Ticket{Title: clipLine(title, 120), Description: title, Stage: key, Origin: title}
	if err := s.tickets.Create(ctx, t, tickets.Actor{Name: "Oaf"}); err != nil {
		return nil, err
	}
	groupTickets.Store(key, t.ID)
	return t, nil
}

// fileWorkTicket files one piece of a run's work for an agent. With a stage
// key, a ticket already filed for the same stage under the same group is
// returned instead of a second one: a resumed pipeline re-dispatches the
// stage that was running when the orchestrator stopped.
func (s *Server) fileWorkTicket(ctx context.Context, instanceID, archetype, title, goal, groupKey, groupTitle, stage string) (*protocol.Ticket, error) {
	inst, err := s.resolveTriggerTarget(ctx, instanceID, archetype)
	if err != nil {
		return nil, err
	}
	group, err := s.groupTicket(ctx, groupKey, groupTitle)
	if err != nil {
		return nil, err
	}
	if stage != "" {
		if kids, err := s.db.Children(ctx, group.ID); err == nil {
			for i := range kids {
				if kids[i].Stage == stage && kids[i].Status != protocol.TicketCancelled {
					return &kids[i], nil
				}
			}
		}
	}
	t := &protocol.Ticket{
		Title: clipLine(firstNonEmptyStr(title, firstSentence(goal)), 120), Description: goal,
		AssigneeID: inst.ID, ParentID: group.ID, Stage: stage, Origin: group.Origin, Thread: group.Thread,
	}
	if err := s.tickets.Create(ctx, t, tickets.Actor{Name: "Oaf"}); err != nil {
		return nil, err
	}
	return t, nil
}

// runPipelineNode runs one pipeline stage as a ticket and waits for it.
func (s *Server) runPipelineNode(ctx context.Context, node protocol.PipelineNode) (string, error) {
	run, _ := pipeline.RunFrom(ctx)
	short := strings.TrimPrefix(run.RunID, "run-")
	if len(short) > 6 {
		short = short[len(short)-6:]
	}
	t, err := s.fileWorkTicket(ctx, node.InstanceID, node.ArchetypeID, node.Name, node.GoalTemplate,
		"pipeline:"+run.RunID, fmt.Sprintf("Pipeline %s — run %s", firstNonEmptyStr(run.Pipeline, "run"), short),
		"pipeline-node:"+node.ID)
	if err != nil {
		return "", err
	}

	// Poll rather than subscribe: a stage is minutes of work, the run is
	// already asynchronous, and a dropped event would hang the whole graph.
	const (
		poll   = 5 * time.Second
		giveUp = 2 * time.Hour
	)
	deadline := time.Now().Add(giveUp)
	for {
		select {
		case <-ctx.Done():
			st := protocol.TicketCancelled
			_, _ = s.tickets.Update(context.WithoutCancel(ctx), t.ID, tickets.Patch{Status: &st}, tickets.Actor{Name: "Oaf"})
			return "", ctx.Err()
		case <-time.After(poll):
		}
		cur, err := s.db.Ticket(ctx, t.ID)
		if err != nil {
			return "", err
		}
		switch cur.Status {
		case protocol.TicketDone:
			return firstNonEmptyStr(strings.TrimSpace(cur.Result), "completed"), nil
		case protocol.TicketBlocked:
			return "", fmt.Errorf("%s is blocked: %s", cur.Ref(), firstNonEmptyStr(cur.BlockedReason, "no reason given"))
		case protocol.TicketCancelled:
			return "", errors.New(cur.Ref() + " was cancelled")
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%s did not finish within two hours", cur.Ref())
		}
	}
}

// startMissionWork is the mission coordinator's way to start work: a ticket
// under the mission's own, for the exact member named.
func (s *Server) startMissionWork(ctx context.Context, instanceID, goal, source string) (string, error) {
	mission := strings.TrimPrefix(strings.TrimPrefix(source, "swarm-review:"), "swarm:")
	t, err := s.fileWorkTicket(ctx, instanceID, "", "", goal, "mission:"+mission, "Mission: "+mission, "")
	if err != nil {
		return "", err
	}
	return t.Ref(), nil
}
