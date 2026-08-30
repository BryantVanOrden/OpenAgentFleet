package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Handing work on when an agent finishes it.
//
// Agents could be given parts of a job but had no way to pass one back. A
// builder finished, published, and stopped; the reviewer had already answered
// the original request and was idle; and nothing connected the two. Asked for
// something, agents ended up asking each other for it in a loop instead --
// "could you share the design", "I am currently idle" -- because the only
// coordination they had was conversation.
//
// A relay is the missing piece: finishing a part wakes whoever the next part
// belongs to, and hands them what was produced.

// maxRelayRounds bounds a collaboration.
//
// Build, review, fix, re-review is four. Beyond that a pair of agents is
// either converging so slowly that a person should look, or arguing -- and an
// unbounded loop of agents starting each other is the one failure mode of this
// design that costs real money.
const maxRelayRounds = 6

// relayStage is what an agent's part is for, decided from what it said it
// would do. The order here is the order work flows in.
type relayStage int

const (
	stageDesign relayStage = iota
	stageBuild
	stageTest
	stageReview
	stageUnknown
)

func (s relayStage) String() string {
	switch s {
	case stageDesign:
		return "design"
	case stageBuild:
		return "build"
	case stageTest:
		return "test"
	case stageReview:
		return "review"
	}
	return "unknown"
}

// stageOf reads an agent's stated part.
//
// Keyword matching on purpose. Asking a second model to classify a sentence
// costs a round trip per agent per handoff, and gets it wrong in less
// predictable ways than a word list does.
func stageOf(plan string) relayStage {
	p := strings.ToLower(plan)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(p, w) {
				return true
			}
		}
		return false
	}
	// Checked most-specific first: "write the test plan" is testing, not
	// building, and "review the design" is review, not design.
	switch {
	case has("review", "audit", "critique", "quality assurance", "qa report"):
		return stageReview
	case has("test", "verify", "validate", "check it", "try it"):
		return stageTest
	case has("design", "concept", "mechanic", "spec", "plan the", "architecture"):
		return stageDesign
	case has("write", "build", "implement", "code", "produce", "create"):
		return stageBuild
	}
	return stageUnknown
}

// collaborator is one agent's place in a job.
type collaborator struct {
	InstanceID string
	Name       string
	Plan       string
	Stage      relayStage
}

// collaboration is one operator request and the agents working on it.
type collaboration struct {
	Request string
	Thread  string
	Members []collaborator
	Round   int
	// Done marks stages already handed on, so finishing twice does not start
	// the next agent twice.
	Handed map[string]bool
}

// relay tracks live collaborations.
//
// In memory: a collaboration is a conversation that is happening now, and one
// interrupted by a restart is better dropped than resumed hours later against
// work that has moved on.
type relay struct {
	mu sync.Mutex
	// byTask maps a running task to the collaboration it belongs to, and to
	// the member running it. Both are needed: the collaboration says who else
	// is on the job, and the member says which of them just finished.
	byTask map[string]taskOwner
	// pending holds jobs whose members are all still waiting to be handed
	// work, before anyone has started a task to key them by.
	pending []*collaboration
}

type taskOwner struct {
	job    *collaboration
	member collaborator
}

func newRelay() *relay {
	return &relay{byTask: map[string]taskOwner{}}
}

// waitsForWork reports whether a stage needs something to exist first.
//
// Test and review do. An agent that starts testing the moment it is asked is
// testing nothing, and — more damagingly — it is busy, so when the builder
// actually publishes there is nobody free to hand it to. Every agent starting
// at once is why the relay never moved: four agents, four running tasks, no
// recipients.
func (st relayStage) waitsForWork() bool {
	return st == stageTest || st == stageReview
}

// waitFor registers an agent that will be handed work rather than starting now.
func (r *relay) waitFor(request, thread string, c collaborator) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.byTask {
		if existing.job.Request == request {
			existing.job.Members = append(existing.job.Members, c)
			return
		}
	}
	// A colleague may already be waiting on the same request. Checking only
	// the started jobs made the second waiter open a job of its own, so the
	// tester and the reviewer ended up on separate ones and neither could be
	// handed to.
	for _, p := range r.pending {
		if p.Request == request {
			p.Members = append(p.Members, c)
			return
		}
	}
	r.pending = append(r.pending, &collaboration{
		Request: request,
		Thread:  thread,
		Members: []collaborator{c},
		Handed:  map[string]bool{},
	})
}

// join records that an agent has started its part of a request.
func (r *relay) join(taskID, request, thread string, c collaborator) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Everyone answering the same operator message is on the same job.
	for _, existing := range r.byTask {
		if existing.job.Request == request {
			existing.job.Members = append(existing.job.Members, c)
			r.byTask[taskID] = taskOwner{job: existing.job, member: c}
			return
		}
	}
	// Agents that registered to wait got here first.
	for _, p := range r.pending {
		if p.Request == request {
			p.Members = append(p.Members, c)
			r.byTask[taskID] = taskOwner{job: p, member: c}
			return
		}
	}
	r.byTask[taskID] = taskOwner{
		job: &collaboration{
			Request: request,
			Thread:  thread,
			Members: []collaborator{c},
			Handed:  map[string]bool{},
		},
		member: c,
	}
}

// register adds a member for a task started by the relay itself, so a handoff
// can be handed on again.
func (r *relay) register(taskID string, job *collaboration, c collaborator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byTask[taskID] = taskOwner{job: job, member: c}
}

// next returns the agent that should pick the work up after this one, and the
// collaboration it belongs to.
//
// Work flows design → build → test → review. After a review it goes back to
// whoever built, because a review that finds nothing to fix is rare and a
// review nobody acts on is decoration.
func (r *relay) next(taskID string) (*collaboration, collaborator, collaborator, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	owner, ok := r.byTask[taskID]
	if !ok {
		return nil, collaborator{}, collaborator{}, false
	}
	delete(r.byTask, taskID)

	c, finished := owner.job, owner.member
	if c.Round >= maxRelayRounds {
		return nil, collaborator{}, collaborator{}, false
	}
	if c.Handed[finished.InstanceID+finished.Stage.String()] {
		return nil, collaborator{}, collaborator{}, false
	}

	successor, ok := c.successorFor(finished)
	if !ok {
		return nil, collaborator{}, collaborator{}, false
	}
	c.Round++
	c.Handed[finished.InstanceID+finished.Stage.String()] = true
	return c, finished, successor, true
}

// successorFor picks who takes the work next.
func (c *collaboration) successorFor(finished collaborator) (collaborator, bool) {
	want := []relayStage{}
	switch finished.Stage {
	case stageDesign:
		want = []relayStage{stageBuild}
	case stageBuild:
		want = []relayStage{stageTest, stageReview}
	case stageTest:
		want = []relayStage{stageReview, stageBuild}
	case stageReview:
		// Back to whoever builds: a review nobody acts on is decoration.
		want = []relayStage{stageBuild}
	default:
		return collaborator{}, false
	}
	for _, stage := range want {
		for _, m := range c.Members {
			if m.InstanceID != finished.InstanceID && m.Stage == stage {
				return m, true
			}
		}
	}
	return collaborator{}, false
}

// watchForHandoffs starts work on the next agent when one finishes its part.
func (s *Server) watchForHandoffs(ctx context.Context) {
	sub := s.bus.Subscribe("")
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.C:
			// Publishing is the real signal that a part is done.
			//
			// Waiting for the task to succeed sounded right and did not work:
			// these agents publish their design or their code and then stall
			// on ask_human rather than finishing cleanly, so every task sat at
			// awaiting_human and nothing was ever handed on. The artefact in
			// the catalog is the thing the next agent actually needs, and it
			// exists whether or not the model remembered to say "done".
			if ev.Type == "work" && ev.InstanceID != "" {
				s.handOffFrom(ctx, ev.InstanceID, describeWork(ev.Payload))
				continue
			}
			if ev.Type != "task.state" || ev.TaskID == "" {
				continue
			}
			payload, ok := ev.Payload.(map[string]any)
			if !ok {
				continue
			}
			// The runner emits the state as a TaskState; a JSON round trip
			// would make it a string. Accept both rather than depend on which
			// path the event took.
			if !isSucceeded(payload["state"]) {
				continue
			}
			s.handOff(ctx, ev.TaskID, fmt.Sprint(payload["result"]))
		}
	}
}

// handOff gives the next agent the work, with what the last one produced.
func (s *Server) handOff(ctx context.Context, taskID, result string) {
	c, finished, successor, ok := s.relay.next(taskID)
	if !ok {
		return
	}
	if !s.readyForHandoff(ctx, successor.InstanceID) {
		return
	}

	// Say it in the thread as well as starting the work. A handoff nobody can
	// see looks like an agent starting something at random.
	vault.GlobalBus.SendMessageIn(ctx, c.Thread, finished.InstanceID, finished.Name,
		successor.InstanceID, peerReplyKind,
		fmt.Sprintf("%s is done with the %s. Over to you, %s, for the %s.",
			finished.Name, finished.Stage, successor.Name, successor.Stage), nil)

	goal := fmt.Sprintf(
		"%s has finished the %s and handed it to you for the %s.\n\n"+
			"What they reported:\n%s\n\n"+
			"Your part, which you chose: %s\n\n"+
			"Start by using read_work to see what is in the shared catalog — "+
			"they published there. Do your part on what is actually there, not "+
			"on what you imagine is there, and publish what you produce with "+
			"publish_work so the next agent can build on it.\n\n"+
			"If you are reviewing or testing, be specific: name the file, the "+
			"line, what is wrong and what would fix it. \"Looks good\" ends the "+
			"work; a concrete defect keeps it moving.\n\n"+
			"The original request was: %s",
		finished.Name, finished.Stage, successor.Stage,
		clipLine(result, 600), successor.Plan, c.Request)

	task := &protocol.Task{
		ID:         store.NewID(),
		InstanceID: successor.InstanceID,
		Goal:       goal,
		State:      protocol.TaskQueued,
		MaxSteps:   s.cfg.MaxSteps,
		CreatedAt:  time.Now().UTC(),
	}
	bg := context.WithoutCancel(ctx)
	if err := s.db.CreateTask(bg, task); err != nil {
		s.log.Warn("could not queue a handoff", "to", successor.Name, "err", err)
		return
	}
	if err := s.runner.Start(bg, task); err != nil {
		s.log.Warn("could not start a handoff", "to", successor.Name, "err", err)
		return
	}
	// The agent receiving the work is registered too, so it can hand on in
	// turn. Without this the relay moves once and stops, which is a handoff
	// rather than the loop it is supposed to be.
	s.relay.register(task.ID, c, successor)

	s.log.Info("handed work on", "from", finished.Name, "to", successor.Name,
		"stage", successor.Stage.String(), "round", c.Round)
}


func isSucceeded(v any) bool {
	switch t := v.(type) {
	case protocol.TaskState:
		return t == protocol.TaskSucceeded
	case string:
		return t == string(protocol.TaskSucceeded)
	}
	return false
}


// describeWork summarises what was published, for the next agent's brief.
func describeWork(payload any) string {
	item, ok := payload.(protocol.WorkItem)
	if !ok {
		return "something in the shared work catalog"
	}
	return fmt.Sprintf("%s %q (version %d, %d bytes)%s",
		item.Kind, item.Name, item.Version, len(item.Content),
		optionalDescription(item.Description))
}

func optionalDescription(d string) string {
	if strings.TrimSpace(d) == "" {
		return ""
	}
	return " — " + clipLine(d, 200)
}

// handOffFrom hands work on when an agent publishes, rather than when its task
// finishes.
func (s *Server) handOffFrom(ctx context.Context, instanceID, produced string) {
	taskID, ok := s.relay.taskFor(instanceID)
	if !ok {
		return
	}
	s.handOff(ctx, taskID, "They published "+produced)
}


// taskFor returns the live task an agent is running as part of a job.
func (r *relay) taskFor(instanceID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for taskID, owner := range r.byTask {
		if owner.member.InstanceID == instanceID {
			return taskID, true
		}
	}
	return "", false
}


// readyForHandoff reports whether an agent can take the next part, and clears
// a parked task if that is all that is in the way.
//
// A task waiting on a human counts as busy everywhere else, and rightly: it is
// mid-run. But these agents routinely park on ask_human rather than finishing
// cleanly, so treating that as busy meant every handoff was skipped and the
// relay never moved once. A parked task is a question nobody is answering; new
// work from a colleague supersedes it. A genuinely running task is left alone.
func (s *Server) readyForHandoff(ctx context.Context, instanceID string) bool {
	tasks, err := s.db.ListTasks(ctx, instanceID, 20)
	if err != nil {
		return false
	}
	parked := []string{}
	for _, t := range tasks {
		switch t.State {
		case protocol.TaskRunning, protocol.TaskQueued:
			return false
		case protocol.TaskAwaitingHuman:
			parked = append(parked, t.ID)
		}
	}
	for _, id := range parked {
		if !s.runner.Cancel(id) {
			_ = s.db.UpdateTaskState(ctx, id, protocol.TaskCancelled, 0,
				"superseded by work handed on from a colleague", "")
		}
		s.log.Info("cleared a parked task to accept a handoff", "task", id)
	}
	return true
}
