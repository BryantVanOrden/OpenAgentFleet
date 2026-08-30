package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

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

// maxPendingJobs caps jobs whose members are all still waiting.
//
// Generously more than a fleet works on at once; it exists so that a mistake
// somewhere else cannot turn into unbounded memory in a long-running process.
const maxPendingJobs = 8

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
	// Whole words only.
	//
	// Substring matching read "markdown preview box" as a review, so the agent
	// asked to build it registered as a reviewer, waited for work nobody was
	// making, and the job was correctly reported as one that could not start.
	// The bug was upstream of all of that, in one word inside another.
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(plan), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		words[w] = true
	}
	has := func(want ...string) bool {
		for _, w := range want {
			// A phrase is checked as a phrase; a single word as a word.
			if strings.ContainsRune(w, ' ') {
				if strings.Contains(strings.ToLower(plan), w) {
					return true
				}
				continue
			}
			if words[w] {
				return true
			}
		}
		return false
	}
	// Checked most-specific first: "write the test plan" is testing, not
	// building, and "review the design" is review, not design.
	switch {
	case has("review", "reviews", "reviewing", "audit", "audits", "auditing",
		"critique", "quality assurance", "qa report"):
		return stageReview
	case has("test", "tests", "testing", "verify", "verifies", "verifying",
		"validate", "validates", "check it", "try it"):
		return stageTest
	case has("design", "designs", "designing", "concept", "mechanic", "mechanics",
		"spec", "specs", "plan the", "architecture"):
		return stageDesign
	case has("write", "writes", "writing", "build", "builds", "building",
		"implement", "implements", "code", "produce", "produces", "create",
		"creates", "creating", "generate", "generates"):
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
	// Started is when the job was opened, so a job where everybody is waiting
	// for work nobody is making can be noticed rather than sitting silently.
	Started time.Time
	// Stalled records that the operator has already been told, so they are
	// told once rather than every minute.
	Stalled bool
	// HasProducer is set once any member takes a part that makes something.
	// Remembered on the job rather than recomputed from the members, because
	// the producer's task is removed when it hands on -- and a job that had
	// plainly been working then looked like one nobody had started.
	HasProducer bool
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
	// jobs holds every live collaboration, keyed by the request that started
	// it.
	//
	// Keyed by request rather than by task because a job outlives any one
	// task: when a builder finishes and hands on, its task entry goes, and a
	// colleague registering to wait afterwards used to find nothing and open a
	// second job containing only waiters -- which then looked exactly like a
	// job nobody was working on, and got reported as stalled while the work
	// was in fact proceeding.
	jobs map[string]*collaboration
}

type taskOwner struct {
	job    *collaboration
	member collaborator
}

func newRelay() *relay {
	return &relay{
		byTask: map[string]taskOwner{},
		jobs:   map[string]*collaboration{},
	}
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

	job := r.jobFor(request, thread)
	job.Members = append(job.Members, c)
	// Marked on whichever path registers a producer, not just the one that
	// starts a task: a job with somebody making something is never a job
	// whose members are all waiting for nothing.
	if !c.Stage.waitsForWork() {
		job.HasProducer = true
	}
}

// join records that an agent has started its part of a request.
func (r *relay) join(taskID, request, thread string, c collaborator) {
	r.mu.Lock()
	defer r.mu.Unlock()

	job := r.jobFor(request, thread)
	job.Members = append(job.Members, c)
	if !c.Stage.waitsForWork() {
		// Somebody is making something, so nobody on this job is waiting for
		// work that will never come.
		job.HasProducer = true
	}
	r.byTask[taskID] = taskOwner{job: job, member: c}
}

// jobFor returns the collaboration for a request, opening one if this is the
// first agent to answer it. Caller holds the lock.
func (r *relay) jobFor(request, thread string) *collaboration {
	if job, ok := r.jobs[request]; ok {
		return job
	}
	job := &collaboration{
		Request: request,
		Thread:  thread,
		Handed:  map[string]bool{},
		Started: time.Now(),
	}
	r.jobs[request] = job
	// A backstop: jobs are superseded by the next request, but a mistake
	// elsewhere should not grow this without limit in a process that runs for
	// weeks. Oldest first, since the newest is the one in play.
	if len(r.jobs) > maxPendingJobs {
		var oldest string
		var at time.Time
		for req, j := range r.jobs {
			if oldest == "" || j.Started.Before(at) {
				oldest, at = req, j.Started
			}
		}
		delete(r.jobs, oldest)
	}
	return job
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
	// The task must not be one that has finished for good.
	//
	// The relay held on to tasks that had been cancelled, so when an agent
	// later published something for an entirely different request, it handed
	// work on for the old one -- "ToolCheck is done with the test, over to you
	// Auditor" arrived in the middle of a job about writing documentation.
	//
	// But requiring it to be exactly running was too strict, and silently
	// broke the thing this exists for: an agent that publishes and then stops
	// to ask something is parked, not finished, and dropping it there meant
	// the artefact was in the catalog and nobody was ever handed it. Parked is
	// live -- the work exists and the next agent can start on it.
	task, err := s.db.Task(ctx, taskID)
	if err != nil {
		s.relay.forget(taskID)
		return
	}
	switch task.State {
	case protocol.TaskRunning, protocol.TaskAwaitingHuman, protocol.TaskQueued:
		// Still this agent's current work.
	default:
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
	delete(r.jobs, request)
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
	for req := range r.jobs {
		if req != request {
			delete(r.jobs, req)
		}
	}
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

// stalledJobs returns jobs where everybody is waiting and nothing is coming.
//
// An agent named only for testing or reviewing registers to wait, which is
// right -- but if nobody was asked to build the thing, nothing will ever
// arrive and it waits for good. Worse, it says so in the thread first: "I will
// test probe-three" reads exactly like work starting. This finds those, so
// they can be reported once instead of sitting silently.
func (r *relay) stalledJobs(olderThan time.Duration) []*collaboration {
	r.mu.Lock()
	defer r.mu.Unlock()

	// A job with any live task is progressing, whatever its members' stages.
	live := map[*collaboration]bool{}
	for _, owner := range r.byTask {
		live[owner.job] = true
	}

	cutoff := time.Now().Add(-olderThan)
	var out []*collaboration
	for _, p := range r.jobs {
		if p.Stalled || live[p] || p.HasProducer || p.Started.After(cutoff) {
			continue
		}
		// Work that has already flowed is not a job nobody started.
		if len(p.Handed) > 0 {
			continue
		}
		p.Stalled = true
		out = append(out, p)
	}
	return out
}

// watchForStalledJobs tells the operator when a job cannot start.
func (s *Server) watchForStalledJobs(ctx context.Context) {
	const (
		every   = time.Minute
		patient = 3 * time.Minute
	)
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, job := range s.relay.stalledJobs(patient) {
				var who []string
				for _, m := range job.Members {
					who = append(who, m.Name+" ("+m.Stage.String()+")")
				}
				msg := "Nothing is going to reach " + strings.Join(who, " or ") +
					": they are waiting to be handed work, and nobody was asked " +
					"to make anything. Name an agent to build or design it and " +
					"they will pick it up."
				vault.GlobalBus.SendMessageIn(ctx, job.Thread, "", "Fleet",
					"broadcast", peerReplyKind, msg, nil)
				s.log.Info("job cannot start; nobody was asked to produce anything",
					"request", clipLine(job.Request, 60))
			}
		}
	}
}
