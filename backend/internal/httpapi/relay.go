package httpapi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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
var fileNames = regexp.MustCompile(`[\w./-]+\.(html?|js|ts|css|py|go|md|json|ya?ml|txt|sh)\b`)

func stageOf(plan string) relayStage {
	// Whole words only.
	//
	// Substring matching read "markdown preview box" as a review, so the agent
	// asked to build it registered as a reviewer, waited for work nobody was
	// making, and the job was correctly reported as one that could not start.
	// The bug was upstream of all of that, in one word inside another.
	// File names are not roles: "tests.html" is a thing being built.
	lower := fileNames.ReplaceAllString(strings.ToLower(plan), " ")
	counts := map[string]int{}
	for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		counts[w]++
	}
	// Scored, not first-match.
	//
	// The first version returned the most specific category with any hit,
	// and Builder's brief -- "create a single-page notes app ... write a
	// README.md and a tests.html page ... tell Checker what to test" -- came
	// back as a tester. It then waited to be handed work by itself. So each
	// category is counted and the biggest wins; a tie still goes to the most
	// specific, which is what keeps "write the test plan" a testing job.
	scores := map[relayStage]int{}
	count := func(st relayStage, want ...string) {
		for _, w := range want {
			if strings.ContainsRune(w, ' ') {
				scores[st] += strings.Count(lower, w)
				continue
			}
			scores[st] += counts[w]
		}
	}
	count(stageReview, "review", "reviews", "reviewing", "audit", "audits", "auditing",
		"critique", "quality assurance", "qa report")
	count(stageTest, "test", "tests", "testing", "tester", "verify", "verifies", "verifying",
		"validate", "validates", "check it", "try it",
		// Run 17: "you own quality ... run through the app as a real user ...
		// write a numbered findings list ordered by severity with repro steps"
		// scored build 1 (write), test 0, and the tester started building.
		"quality", "qa", "findings", "bug", "bugs", "defect", "defects", "repro",
		"as a real user", "user would")
	count(stageDesign, "design", "designs", "designing", "concept", "mechanic", "mechanics",
		"spec", "specs", "plan the", "architecture")
	count(stageBuild, "write", "writes", "writing", "build", "builds", "building",
		"implement", "implements", "code", "produce", "produces", "create",
		"creates", "creating", "generate", "generates")
	// "Verify every feature yourself before you report" is a builder checking
	// its own work, not a testing role. Each self-reference cancels one test
	// word.
	scores[stageTest] -= counts["myself"] + counts["yourself"]
	if scores[stageTest] < 0 {
		scores[stageTest] = 0
	}
	best, bestScore := stageUnknown, 0
	for _, st := range []relayStage{stageReview, stageTest, stageDesign, stageBuild} {
		if scores[st] > bestScore {
			best, bestScore = st, scores[st]
		}
	}
	return best
}

// defersToColleague reports whether a part or plan says it waits on somebody
// else's output -- "when Builder's part reaches you", "whenever Builder
// reports a version", "I will wait for Builder to send me the URLs". Such a
// part is downstream whatever verbs it uses, and an agent that starts on it
// at once tests nothing and is busy when the real work arrives.
func defersToColleague(text string) bool {
	m := defersRe.FindStringSubmatch(strings.ToLower(text))
	if m == nil {
		return false
	}
	subject := m[1]
	if subject == "" {
		subject = m[2]
	}
	// "tell Checker when it is ready" waits on nothing; the subject has to
	// be somebody.
	switch subject {
	case "it", "this", "that", "they", "everything", "all", "you", "i", "we", "the", "a", "my", "your", "our":
		return false
	}
	return true
}

var defersRe = regexp.MustCompile(
	`\b(?:wait(?:s|ing)? (?:for|until) ([a-z0-9'_-]+)|` +
		`(?:when|whenever|once|after) ([a-z0-9'_-]+)(?: [a-z]+){0,3} ` +
		`(?:reports?|reaches you|hands?|sends?|publishes|finishes|is ready|is done|is up))\b`)

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
	// Retries counts, per agent, the runs of its part that died for a reason
	// that said nothing about the work -- a reply the parser could not read, a
	// provider outage -- and were started again. Bounded by maxPartRetries.
	Retries map[string]int
	// Published is what each member has put in the catalog during its
	// current part, handed on with the part when it ends rather than the
	// moment the first file lands. See handOffFrom.
	Published map[string][]publishedItem
}

// publishedItem is one catalog entry a part produced: how to describe it to
// the next agent, and the name a fix would be republished under.
type publishedItem struct {
	desc, name string
}

// describePublished lists what a part produced, most recent last.
func describePublished(items []publishedItem) string {
	descs := make([]string, 0, len(items))
	for _, it := range items {
		descs = append(descs, it.desc)
	}
	return strings.Join(descs, "; ")
}

// maxPartRetries is how many times one agent's part is restarted after a
// system failure before the job moves on without it.
const maxPartRetries = 2

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
		Request:   request,
		Thread:    thread,
		Handed:    map[string]bool{},
		Retries:   map[string]int{},
		Published: map[string][]publishedItem{},
		Started:   time.Now(),
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

// notePublished records a catalog item an agent published while its part was
// still running, to go with the part when it ends.
func (r *relay) notePublished(taskID string, it publishedItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, ok := r.byTask[taskID]
	if !ok {
		return
	}
	if owner.job.Published == nil {
		owner.job.Published = map[string][]publishedItem{}
	}
	id := owner.member.InstanceID
	owner.job.Published[id] = append(owner.job.Published[id], it)
}

// published returns, and forgets, what an agent published during the part a
// task belongs to. Retries and marathon windows keep the same member, so the
// list survives them; it is cleared only when the part is handed on.
func (r *relay) published(taskID string) []publishedItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, ok := r.byTask[taskID]
	if !ok || owner.job.Published == nil {
		return nil
	}
	out := owner.job.Published[owner.member.InstanceID]
	delete(owner.job.Published, owner.member.InstanceID)
	return out
}

// rebind moves a task's place on the job to the task that continues it.
func (r *relay) rebind(oldID, newID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	owner, ok := r.byTask[oldID]
	if !ok {
		return
	}
	delete(r.byTask, oldID)
	r.byTask[newID] = owner
}

// retry moves a task's place on the job to a fresh task for the same agent,
// if that agent still has retries left. It reports false when the part has
// been restarted enough and should be handed on as a failure instead.
func (r *relay) retry(taskID, newTaskID string) (*collaboration, collaborator, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	owner, ok := r.byTask[taskID]
	if !ok {
		return nil, collaborator{}, false
	}
	if owner.job.Retries == nil {
		owner.job.Retries = map[string]int{}
	}
	if owner.job.Retries[owner.member.InstanceID] >= maxPartRetries {
		return nil, collaborator{}, false
	}
	owner.job.Retries[owner.member.InstanceID]++
	delete(r.byTask, taskID)
	r.byTask[newTaskID] = owner
	return owner.job, owner.member, true
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
				s.handOffFrom(ctx, ev.InstanceID, describeWork(ev.Payload), workNameOf(ev.Payload))
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
			// A marathon window that closed unfinished continues in a new
			// task. The job has to follow it: three windows and a failure
			// later, the relay still only knew the first task id, so the
			// failure was nobody's, no retry ran and no hand-off happened.
			// The job died silently at 17:36 with the tester still waiting.
			if st, _ := terminalState(payload["state"]); st == protocol.TaskContinued {
				if next, _ := payload["next_task_id"].(string); next != "" {
					s.relay.rebind(ev.TaskID, next)
				}
				continue
			}
			state, done := terminalState(payload["state"])
			if !done {
				continue
			}
			result := fmt.Sprint(payload["result"])
			if state == protocol.TaskFailed {
				// A run that died for a reason unrelated to the work -- the
				// model answered in a syntax the parser did not know, the
				// provider went away -- is restarted for the same agent, with
				// a note of where it got to. Handing it on as "they did not
				// finish" made a tester test nothing and left the operator a
				// stalled job, when the builder was seven steps into a
				// perfectly good build.
				if s.retryFailedPart(ctx, ev.TaskID, fmt.Sprint(payload["error"])) {
					continue
				}
				result = "They did not finish: " + fmt.Sprint(payload["error"]) +
					". Whatever they left in the catalog is what there is."
			}
			// What the part published while it ran goes with it. The last
			// item is what a fix would be republished under.
			name := ""
			if items := s.relay.published(ev.TaskID); len(items) > 0 {
				name = items[len(items)-1].name
				if state == protocol.TaskFailed {
					result += " They had published " + describePublished(items) + "."
				} else {
					result = "They published " + describePublished(items) +
						".\nTheir closing report: " + result
				}
			}
			s.handOff(ctx, ev.TaskID, result, name)
		}
	}
}

// systemFailure reports whether a task's error describes the machinery rather
// than the work: the kind of failure a second attempt can simply get past.
// The model's own verdict ("agent gave up", or whatever it said in a fail
// action) is not one of these; retrying an agent that decided the task was
// impossible only makes it decide again.
//
// A stall -- the same action repeated until the runner gave up -- is neither
// the machinery nor a verdict. It is a tester stuck on an address bar, and a
// fresh window told what it repeated and what to do instead gets past it,
// where handing the job on as "they did not finish" tests nothing.
func systemFailure(err string) bool {
	for _, p := range []string{
		"model would not produce a valid action",
		"every model provider failed",
		"could not observe the desktop",
		"stalled and could not reach the operator",
		"stalled ",
	} {
		if strings.HasPrefix(err, p) {
			return true
		}
	}
	return false
}

// retryFailedPart starts a failed part again for the same agent when the
// failure was the machinery's, telling the thread so. It reports false when
// the failure should be handed on instead: not a system failure, not a relay
// task, or the agent has already been retried enough.
func (s *Server) retryFailedPart(ctx context.Context, taskID, errText string) bool {
	if !systemFailure(errText) {
		return false
	}
	bg := context.WithoutCancel(ctx)
	old, err := s.db.Task(bg, taskID)
	if err != nil || old == nil {
		return false
	}
	note := fmt.Sprintf("\n\nYour previous attempt at this stopped at step %d, "+
		"not because of anything you did wrong: %s. Anything you already created is "+
		"still on disk. Look at what exists, then continue from there rather than "+
		"starting over. Answer with one JSON action object per turn, nothing else.",
		old.Step, clipLine(errText, 200))
	if strings.Contains(errText, "without making progress") {
		note = fmt.Sprintf("\n\nYour previous attempt at this stalled at step %d, repeating "+
			"one action that was not working (%s). Do not do that again. To open a web "+
			"address use open_url with the address -- never the address bar. If a click "+
			"is not taking, use the keyboard, or ask the author with message_peer for the "+
			"exact address or a screenshot. Anything you already created is still on "+
			"disk. Answer with one JSON action object per turn, nothing else.",
			old.Step, clipLine(errText, 160))
	}
	task := &protocol.Task{
		ID:         store.NewID(),
		InstanceID: old.InstanceID,
		OwnerID:    old.OwnerID,
		Goal:       old.Goal + note,
		State:      protocol.TaskQueued,
		MaxSteps:   old.MaxSteps,
		Params:     old.Params,
		CreatedAt:  time.Now().UTC(),
	}
	job, member, ok := s.relay.retry(taskID, task.ID)
	if !ok {
		return false
	}
	if err := s.db.CreateTask(bg, task); err != nil {
		s.log.Warn("could not queue a retry", "instance", member.Name, "err", err)
		return false
	}
	// A run that died before its first step met a condition that is still
	// there a second later -- two retries fired in the same second on
	// Checker's desktop and both died at step 0. Give it a moment.
	start := func() {
		if err := s.runner.Start(bg, task); err != nil {
			s.log.Warn("could not start a retry", "instance", member.Name, "err", err)
		}
	}
	if old.Step == 0 {
		time.AfterFunc(20*time.Second, start)
	} else {
		start()
	}
	vault.GlobalBus.SendMessageIn(bg, job.Thread, member.InstanceID, member.Name,
		"broadcast", peerReplyKind,
		fmt.Sprintf("My run stopped at step %d (%s). Picking it back up where I left off.",
			old.Step, clipLine(errText, 120)), nil)
	s.log.Info("restarted a failed part", "instance", member.Name, "task", task.ID,
		"attempt", job.Retries[member.InstanceID], "err", clipLine(errText, 80))
	return true
}

// handOff gives the next agent the work, with what the last one produced.
func (s *Server) handOff(ctx context.Context, taskID, result, produced string) {
	c, finished, successor, ok := s.relay.next(taskID)
	if !ok {
		return
	}
	bg := context.WithoutCancel(ctx)
	// A successor already running its own take on this same request -- a
	// tester that did not wait -- is superseded by the work actually handed
	// to it. Run 17: Checker started at the brief, was still poking at a
	// stale tab when Builder finished, and the hand-off was dropped because
	// it was "busy".
	if own, ok := s.relay.liveTaskOnJob(c, successor.InstanceID); ok {
		if !s.runner.Cancel(own) {
			_ = s.db.UpdateTaskState(bg, own, protocol.TaskCancelled, 0,
				"superseded by work handed on from "+finished.Name, "")
		}
		s.relay.forget(own)
		s.log.Info("cancelled a successor's own run to hand it the work", "task", own, "to", successor.Name)
	}
	if !s.readyForHandoff(bg, successor.InstanceID) {
		// Busy on something else. The hand-off waits for it rather than
		// being dropped: a dropped hand-off is a job that dies with the
		// work in the catalog and nobody told.
		s.log.Info("successor is busy; the hand-off waits", "to", successor.Name, "from", finished.Name)
		go func() {
			for i := 0; i < 90; i++ {
				time.Sleep(30 * time.Second)
				if s.readyForHandoff(bg, successor.InstanceID) {
					s.startHandoff(bg, c, finished, successor, result, produced)
					return
				}
			}
			s.log.Warn("hand-off abandoned; the successor never came free", "to", successor.Name)
		}()
		return
	}
	s.startHandoff(bg, c, finished, successor, result, produced)
}

// startHandoff tells the thread and starts the successor on the work.
func (s *Server) startHandoff(ctx context.Context, c *collaboration, finished, successor collaborator, result, produced string) {
	// The producer's own words to its successor -- "v1 is live at
	// http://af-...:8001, please test: 1. ... 2. ..." -- are the best brief
	// there is, and sat in a thread the hand-off never mentioned. Read
	// before the hand-off line below is posted, so that is not the last word.
	if last := s.lastWordFrom(ctx, finished.InstanceID, successor.InstanceID, c.Started); last != "" {
		result += "\n\nTheir last message to you: " + clipLine(last, 700)
	}

	// Say it in the thread as well as starting the work. A handoff nobody can
	// see looks like an agent starting something at random.
	vault.GlobalBus.SendMessageIn(ctx, c.Thread, finished.InstanceID, finished.Name,
		successor.InstanceID, peerReplyKind, handoffLine(finished, successor, result), nil)

	goal := fmt.Sprintf(
		"%s has finished the %s and handed it to you for the %s.\n\n"+
			"What they reported:\n%s\n\n"+
			"Your part, which you chose: %s\n\n"+
			"%s\n\n%s\n\n"+
			"The original request was: %s",
		finished.Name, finished.Stage, successor.Stage,
		clipLine(result, 1800), successor.Plan,
		briefFor(finished.Stage, successor.Stage, produced),
		machinesNote(finished), c.Request)

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
	bg := ctx
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

// lastWordFrom is the most recent thing one agent said to another (directly or
// in the fleet channel) since a job began.
func (s *Server) lastWordFrom(ctx context.Context, from, to string, since time.Time) string {
	msgs, err := s.db.ListPeerMessages(ctx, to, 200)
	if err != nil {
		return ""
	}
	last := ""
	for _, m := range msgs {
		if m.FromInstanceID != from || !m.CreatedAt.After(since) {
			continue
		}
		if m.Kind == peerSummaryKind || m.Kind == peerSystemKind {
			continue
		}
		if m.ToInstanceID != to && m.ToInstanceID != "broadcast" {
			continue
		}
		last = m.Content
	}
	return last
}

// sandboxHost is the name a bot's machine answers to on the sandbox network,
// the same alias the fleet manager gives its container.
func sandboxHost(instanceID string) string {
	if len(instanceID) < 12 {
		return "af-" + instanceID
	}
	return "af-" + instanceID[:12]
}

// machinesNote tells the agent receiving work where the work actually is.
//
// Handed Builder's build, Checker opened a terminal on its own desktop, ran
// `ls /home/agent/fleet-notes`, found nothing, and told Builder twice that the
// directory did not exist. Every bot has its own machine; nothing in the
// hand-off said so, and nothing said how to reach the other one.
func machinesNote(finished collaborator) string {
	host := sandboxHost(finished.InstanceID)
	return fmt.Sprintf("Where the work is: %s worked on its own machine, not yours -- its files "+
		"are not on your disk and `ls` here will not find them. What %s published is in "+
		"the shared catalog (read_work). Anything %s is serving is reachable from your "+
		"browser at http://%s:<port> (a local web server is usually port 8000: try "+
		"http://%s:8000). If neither has what you need, ask %s with message_peer to "+
		"publish_work the files or tell you the URL -- do not report them missing.",
		finished.Name, finished.Name, finished.Name, host, host, finished.Name)
}

// handoffLine is what the thread says when work moves on. A run that stopped
// short must not be announced as done: "Builder is done with the build" over a
// stalled window told the operator the opposite of what happened.
func handoffLine(finished, successor collaborator, result string) string {
	if strings.HasPrefix(result, "They did not finish") {
		return fmt.Sprintf("%s's %s stopped short (%s). Over to you, %s, for the %s -- with what is there.",
			finished.Name, finished.Stage, clipLine(strings.TrimPrefix(result, "They did not finish: "), 90),
			successor.Name, successor.Stage)
	}
	return fmt.Sprintf("%s is done with the %s. Over to you, %s, for the %s.",
		finished.Name, finished.Stage, successor.Name, successor.Stage)
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

// workNameOf is the catalog name on its own, for when something has to be
// addressed rather than described.
func workNameOf(payload any) string {
	item, ok := payload.(protocol.WorkItem)
	if !ok {
		return ""
	}
	return item.Name
}

func optionalDescription(d string) string {
	if strings.TrimSpace(d) == "" {
		return ""
	}
	return " — " + clipLine(d, 200)
}

// handOffFrom hands work on when an agent publishes, rather than when its task
// finishes.
func (s *Server) handOffFrom(ctx context.Context, instanceID, produced, name string) {
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
	case protocol.TaskRunning:
		// Still working: remember it and hand on when the part ends.
		//
		// Handing on at the first publish was measured wrong. Builder
		// published five files at steps 12-16 and spent the next twenty
		// steps checking them in Firefox and composing the announcement
		// with the address; Checker was started at step 12 with a brief
		// that named one file, gave no address and quoted a fragment of a
		// thought for "what they reported". It guessed a hostname, fought
		// the address bar for nineteen steps and stalled. The part's own
		// closing report and its last message are the brief the tester
		// needs, and both exist only when the part ends.
		s.relay.notePublished(taskID, publishedItem{desc: produced, name: name})
		return
	case protocol.TaskAwaitingHuman, protocol.TaskQueued:
		// Parked is live -- the case this exists for.
	default:
		s.relay.forget(taskID)
		return
	}
	earlier := s.relay.published(taskID)
	items := append(earlier, publishedItem{desc: produced, name: name})
	s.handOff(ctx, taskID, "They published "+describePublished(items), name)
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

// jobOfInstance returns the live job an agent is currently working a task on,
// and its place on it.
func (r *relay) jobOfInstance(instanceID string) (*collaboration, collaborator, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, owner := range r.byTask {
		if owner.member.InstanceID == instanceID {
			return owner.job, owner.member, true
		}
	}
	return nil, collaborator{}, false
}

// claimRound spends one of a job's rounds on a peer-requested task and
// registers it. It reports false when the job has no rounds left, which is
// what stops two agents asking each other for things forever.
func (r *relay) claimRound(job *collaboration, taskID string, c collaborator) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job.Round >= maxRelayRounds {
		return false
	}
	job.Round++
	found := false
	for _, m := range job.Members {
		if m.InstanceID == c.InstanceID {
			found = true
			break
		}
	}
	if !found {
		job.Members = append(job.Members, c)
	}
	r.byTask[taskID] = taskOwner{job: job, member: c}
	return true
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

// liveTaskOnJob returns the task an agent is running as part of this job, if
// any.
func (r *relay) liveTaskOnJob(job *collaboration, instanceID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for taskID, owner := range r.byTask {
		if owner.job == job && owner.member.InstanceID == instanceID {
			return taskID, true
		}
	}
	return "", false
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

// catalogGuidance is how to get at shared work, said the same way everywhere.
//
// A handoff brief said it and a directly started task did not, so an agent
// asked to test something it had not been handed went looking for it in the
// desktop's application launcher -- reasonably, having been told only that the
// thing existed in a catalog. Both paths say it now, from here, so they cannot
// drift apart again.
const catalogGuidance = "Start with read_work to see what is actually in the " +
	"catalog. Work on what is there, not on what you imagine is there. " +
	"read_work also opens anything a browser can show on this desktop and " +
	"tells you the file:// path: it will already be on screen when read_work " +
	"returns. Do not look for it in an application launcher -- it is not " +
	"installed -- and do not search the web, where you will find somebody " +
	"else's."

// briefFor is what to actually do, in the terms of the hop being made.
//
// "Do your part" was too vague to act on: handed a review to apply, the
// builder stopped to ask what was wanted rather than applying it. A model
// given a concrete instruction -- fix these, republish under the same name --
// does not need to ask.
func briefFor(from, to relayStage, produced string) string {
	const readFirst = catalogGuidance

	// Name the report, or the catalog fills up with near-duplicates.
	//
	// Told only to "publish your findings as a file", each round invented a
	// fresh name: convtest_defects.md, then convtest_defects_v2, then
	// convtest_additional_defects, then convtest_final_defects. Five files
	// describing one app, none of them obviously the current one. The build
	// hop already pins its name and stays a single item across versions; the
	// test and review hops need the same instruction.
	reportName := func(suffix string) string {
		if !usableWorkName(produced) {
			return "Publish it under one name and reuse that same name on every " +
				"later round, so it becomes a new version rather than a second file."
		}
		return fmt.Sprintf("Publish it with publish_work under exactly this "+
			"work_name: %q. If that already exists, publish under it anyway -- "+
			"it becomes a new version. Do not invent a variant name like "+
			"%q or %q.",
			produced+suffix, produced+suffix+"_v2", produced+"_final"+suffix)
	}

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
			"happens. Be specific: name the file, the line, what goes wrong and " +
			"what would fix it. \"Looks good\" ends the work; a concrete defect " +
			"keeps it moving. " + reportName("_test_report") +
			" Then finish with done."
	case to == stageReview:
		return readFirst + " Read it as somebody who will have to maintain it. " +
			"List your defects, each one naming the file, the line, what is " +
			"wrong and what would fix it. If it is genuinely sound, say so and " +
			"say why. " + reportName("_review") + " Then finish with done."
	case to == stageBuild:
		return readFirst + " Build what the design calls for and publish it with " +
			"publish_work. Then finish with done."
	}
	return readFirst + " Publish what you produce with publish_work, then finish " +
		"with done."
}

// usableWorkName reports whether a string can be handed to an agent as a name
// to publish under.
//
// It can not. The first version of this was given describeWork's output --
// `app "cdown" (version 1, 4321 bytes)` -- and duly instructed the tester to
// publish under that, which it did, so the catalog gained an item called
// `app \"cdown\" (version 1,`. The plumbing is fixed; this is here so that
// getting it wrong again degrades to the generic instruction instead of
// putting punctuation into the catalog.
func usableWorkName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
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
		every = time.Minute
		// How long a job may sit with everybody waiting before it is called
		// stuck. This is mostly a bet on how long a busy fleet takes to
		// answer: an agent that was mid-task when the request arrived replies
		// when its model call comes back, which under load is minutes.
		patient = 6 * time.Minute
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
				waiting := strings.Join(who, " or ")

				// Why it cannot start decides what to say. "Nobody was asked"
				// is wrong and unhelpful when somebody was asked and happened
				// to be mid-task: the operator reads it as having forgotten to
				// name a builder, names the same one again, and gets the same
				// notice. A busy agent does not come back to a broadcast it
				// was busy for, so this has to say so.
				// Somebody was asked and is still working: say nothing.
				//
				// This used to announce that the job was dead and advise
				// sending it again. Then the timeline showed the truth --
				// notice at 14:37, the same Builder starting that exact
				// request at 14:42, handing off a minute later. A busy agent's
				// reply is queued behind its model call, not dropped, so the
				// job was never stuck and following the advice would have
				// built the thing twice.
				if busy := s.namedButBusy(ctx, job); len(busy) > 0 {
					s.log.Info("job has not started yet; a named producer is busy",
						"request", clipLine(job.Request, 60),
						"busy", strings.Join(busy, ","))
					continue
				}
				msg := "Nothing is going to reach " + waiting +
					": they are waiting to be handed work, and nobody was asked " +
					"to make anything. Name an agent to build or design it and " +
					"they will pick it up."
				vault.GlobalBus.SendMessageIn(ctx, job.Thread, "", "Fleet",
					"broadcast", peerReplyKind, msg, nil)
				s.log.Info("job cannot start",
					"request", clipLine(job.Request, 60))
			}
		}
	}
}

// namedButBusy lists agents the request asked to produce something who never
// registered on the job because they were already working.
//
// Membership is the test for "did they take this up". An agent named to build
// that is neither a member nor idle was asked and could not answer.
func (s *Server) namedButBusy(ctx context.Context, job *collaboration) []string {
	instances, err := s.db.ListInstances(ctx)
	if err != nil {
		return nil
	}
	member := map[string]bool{}
	for _, m := range job.Members {
		member[m.InstanceID] = true
	}
	var busy []string
	for _, inst := range instances {
		if member[inst.ID] {
			continue
		}
		part := assignmentFor(job.Request, inst.Name, instances)
		if part == "" {
			continue
		}
		switch stageOf(part) {
		case stageDesign, stageBuild:
		default:
			continue // only a producer's absence strands the others
		}
		if s.instanceBusy(ctx, inst.ID) {
			busy = append(busy, inst.Name)
		}
	}
	return busy
}

// instanceBusy reports whether an agent currently holds work of any kind.
func (s *Server) instanceBusy(ctx context.Context, instanceID string) bool {
	tasks, err := s.db.ListTasks(ctx, instanceID, 20)
	if err != nil {
		return false
	}
	for _, t := range tasks {
		switch t.State {
		case protocol.TaskRunning, protocol.TaskAwaitingHuman, protocol.TaskQueued:
			return true
		}
	}
	return false
}
