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
			// A finished task hands on whether or not it finished well.
			//
			// Waiting for success meant the chain died at the second hop every
			// time: an agent handed the work would run out of steps, or fail,
			// or simply not publish, and the colleague after it was never
			// told. Measured across three runs, the relay moved exactly once
			// each time. A failure is still information the next agent can act
			// on -- "the tester ran out of steps" is worth knowing before you
			// review something.
			state, done := terminalState(payload["state"])
			if !done {
				continue
			}
			result := fmt.Sprint(payload["result"])
			if state == protocol.TaskFailed {
				result = "They did not finish: " + fmt.Sprint(payload["error"]) +
					". Whatever they left in the catalog is what there is."
			}
			s.handOff(ctx, ev.TaskID, result)
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
			"%s\n\n"+
			"The original request was: %s",
		finished.Name, finished.Stage, successor.Stage,
		clipLine(result, 600), successor.Plan,
		briefFor(finished.Stage, successor.Stage), c.Request)

	task := &protocol.Task{
		ID:         store.NewID(),
		InstanceID: successor.InstanceID,
		Goal:       goal,
		State:      protocol.TaskQueued,
		MaxSteps:   s.cfg.MaxSteps,
		CreatedAt:  time.Now().UTC(),
		// Marked as a handoff so the runner does not park it waiting for a
		// person. A colleague handed this agent concrete work with concrete
		// instructions; there is no question a human is going to answer, and
		// each wait costs eight minutes of a chain standing still. Measured:
		// three such waits in twenty minutes on one run.
		Params: map[string]string{protocol.ParamHandoff: "1"},
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

// terminalState reports whether a task has stopped for good, and how.
//
// The runner emits the state as a TaskState; a JSON round trip would make it a
// string. Accept both rather than depend on which path the event took.
func terminalState(v any) (protocol.TaskState, bool) {
	var st protocol.TaskState
	switch t := v.(type) {
	case protocol.TaskState:
		st = t
	case string:
		st = protocol.TaskState(t)
	default:
		return "", false
	}
	switch st {
	case protocol.TaskSucceeded, protocol.TaskFailed:
		return st, true
	}
	// Cancelled is deliberately not terminal here: someone stopped that work
	// on purpose, and carrying on down the chain would undo the decision.
	return st, false
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
	// The task must still be the one the agent is running.
	//
	// The relay held on to tasks that had been cancelled, so when an agent
	// later published something for an entirely different request, it handed
	// work on for the old one -- "ToolCheck is done with the test, over to you
	// Auditor" arrived in the middle of a job about writing documentation.
	task, err := s.db.Task(ctx, taskID)
	if err != nil || task.State != protocol.TaskRunning {
		s.relay.forget(taskID)
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

// forget drops a task the relay should no longer act on.
func (r *relay) forget(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byTask, taskID)
}

// forgetRequest drops a whole job, used when its work is finished or
// abandoned so a later publish cannot revive it.
func (r *relay) forgetRequest(request string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, owner := range r.byTask {
		if owner.job.Request == request {
			delete(r.byTask, id)
		}
	}
	kept := r.pending[:0]
	for _, p := range r.pending {
		if p.Request != request {
			kept = append(kept, p)
		}
	}
	r.pending = kept
}

// forgetOthers drops every job except the one for this request.
//
// A fleet works on one thing at a time here: when the operator asks for
// something new, whatever was half-finished before is no longer what anyone
// should be handed.
func (r *relay) forgetOthers(request string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, owner := range r.byTask {
		if owner.job.Request != request {
			delete(r.byTask, id)
		}
	}
	kept := r.pending[:0]
	for _, p := range r.pending {
		if p.Request == request {
			kept = append(kept, p)
		}
	}
	r.pending = kept
}


// briefFor is what to actually do, in the terms of the hop being made.
//
// "Do your part" was too vague to act on: handed a review to apply, the
// builder stopped to ask what was wanted rather than applying it. A model
// given a concrete instruction -- fix these, republish under the same name --
// does not need to ask.
func briefFor(from, to relayStage) string {
	const readFirst = "Start with read_work to see what is actually in the " +
		"catalog. Work on what is there, not on what you imagine is there."

	switch {
	case to == stageBuild && from == stageReview,
		to == stageBuild && from == stageTest:
		return readFirst + " The defects they listed are the job: fix each one " +
			"in the file they named, then publish the corrected file with " +
			"publish_work under THE SAME work_name, so it replaces the broken " +
			"version rather than sitting beside it. Do not start something new " +
			"and do not ask which defect to fix first -- fix them all. When the " +
			"file is published and the defects are addressed, finish with done."
	case to == stageTest:
		return readFirst + " Try it as a user would and write down what actually " +
			"happens. Publish your findings with publish_work as a file. Be " +
			"specific: name the file, the line, what goes wrong and what would " +
			"fix it. \"Looks good\" ends the work; a concrete defect keeps it " +
			"moving. Then finish with done."
	case to == stageReview:
		return readFirst + " Read it as somebody who will have to maintain it. " +
			"Publish your defects with publish_work as a file, each one naming " +
			"the file, the line, what is wrong and what would fix it. If it is " +
			"genuinely sound, say so and say why. Then finish with done."
	case to == stageBuild:
		return readFirst + " Build what the design calls for and publish it with " +
			"publish_work. Then finish with done."
	}
	return readFirst + " Publish what you produce with publish_work, then finish " +
		"with done."
}
