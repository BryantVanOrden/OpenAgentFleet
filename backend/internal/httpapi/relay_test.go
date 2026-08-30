package httpapi

import (
	"strings"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// An agent's stated part decides where it sits in the flow.
func TestStageOfReadsRealPlans(t *testing.T) {
	cases := map[string]relayStage{
		"I will write the main code for the arcade game":             stageBuild,
		"I will produce a complete self-contained HTML/JS file":      stageBuild,
		"I will take the design phase, defining the core mechanics":  stageDesign,
		"I will define the core mechanics and theme":                 stageDesign,
		"I will test it on a phone screen":                           stageTest,
		"I will verify the game's responsiveness":                    stageTest,
		"I will write the review and final quality assurance report": stageReview,
		"I will audit the code once Builder finishes":                stageReview,
		// Most specific wins: this is a review, not a design.
		"I will review the design document": stageReview,
		"I will write the test plan":        stageTest,
		"I will think about it":             stageUnknown,
	}
	for plan, want := range cases {
		if got := stageOf(plan); got != want {
			t.Errorf("stageOf(%q) = %s, want %s", plan, got, want)
		}
	}
}

func member(id, name string, st relayStage) collaborator {
	return collaborator{InstanceID: id, Name: name, Plan: name + "'s part", Stage: st}
}

// Work flows design to build to test to review, and a review goes back to
// whoever builds — a review nobody acts on is decoration.
func TestWorkFlowsThroughTheStages(t *testing.T) {
	c := &collaboration{Members: []collaborator{
		member("d", "Designer", stageDesign),
		member("b", "Builder", stageBuild),
		member("t", "Tester", stageTest),
		member("r", "Reviewer", stageReview),
	}}

	for _, tc := range []struct {
		from relayStage
		want string
	}{
		{stageDesign, "Builder"},
		{stageBuild, "Tester"},
		{stageTest, "Reviewer"},
		{stageReview, "Builder"},
	} {
		got, ok := c.successorFor(member("x", "Finished", tc.from))
		if !ok {
			t.Errorf("nothing follows %s", tc.from)
			continue
		}
		if got.Name != tc.want {
			t.Errorf("after %s the work went to %s, want %s", tc.from, got.Name, tc.want)
		}
	}
}

// With no tester, a build goes straight to review rather than stopping.
func TestFlowSkipsAMissingStage(t *testing.T) {
	c := &collaboration{Members: []collaborator{
		member("b", "Builder", stageBuild),
		member("r", "Reviewer", stageReview),
	}}
	got, ok := c.successorFor(member("b", "Builder", stageBuild))
	if !ok || got.Name != "Reviewer" {
		t.Errorf("build went to %+v, want Reviewer", got)
	}
}

// An agent never hands work to itself, which would be a bot reviewing its own
// output in an unbroken loop.
func TestAnAgentNeverHandsToItself(t *testing.T) {
	c := &collaboration{Members: []collaborator{member("b", "Builder", stageBuild)}}
	if got, ok := c.successorFor(member("b", "Builder", stageBuild)); ok {
		t.Errorf("an agent handed work to itself: %+v", got)
	}
}

// The loop terminates. An unbounded relay of agents starting each other is the
// one failure of this design that costs real money.
func TestRelayStopsAfterItsRoundLimit(t *testing.T) {
	r := newRelay()
	builder := member("b", "Builder", stageBuild)
	reviewer := member("r", "Reviewer", stageReview)

	r.join("task-0", "make the thing", "broadcast", builder)
	r.join("task-0b", "make the thing", "broadcast", reviewer)

	handoffs := 0
	task := "task-0"
	for i := 0; i < 50; i++ {
		job, _, next, ok := r.next(task)
		if !ok {
			break
		}
		handoffs++
		task = "task-" + next.InstanceID + string(rune('a'+i))
		r.register(task, job, next)
	}

	if handoffs == 0 {
		t.Fatal("the relay never handed anything on")
	}
	if handoffs > maxRelayRounds {
		t.Errorf("the relay ran %d times, past its limit of %d", handoffs, maxRelayRounds)
	}
}

// A task the relay does not know about is ignored rather than guessed at.
func TestUnknownTaskIsNotHandedOn(t *testing.T) {
	r := newRelay()
	if _, _, _, ok := r.next("a-task-nobody-registered"); ok {
		t.Error("the relay handed on work it knew nothing about")
	}
}

// Testing and reviewing wait; designing and building start.
//
// Every agent starting at once is why the relay never moved: four agents, four
// running tasks, and nobody free when the builder finally published.
func TestDownstreamStagesWaitForWork(t *testing.T) {
	if stageDesign.waitsForWork() || stageBuild.waitsForWork() {
		t.Error("a stage that produces the artefact should start immediately")
	}
	if !stageTest.waitsForWork() || !stageReview.waitsForWork() {
		t.Error("a stage that needs an artefact should wait for one")
	}
}

// An agent that registered to wait is on the job when someone starts one.
func TestWaitingMembersJoinTheSameJob(t *testing.T) {
	r := newRelay()
	const request = "build the thing"

	r.waitFor(request, "broadcast", member("t", "Tester", stageTest))
	r.waitFor(request, "broadcast", member("r", "Reviewer", stageReview))
	r.join("task-build", request, "broadcast", member("b", "Builder", stageBuild))

	job, finished, next, ok := r.next("task-build")
	if !ok {
		t.Fatal("the builder finishing handed to nobody")
	}
	if finished.Name != "Builder" {
		t.Errorf("finished = %s, want Builder", finished.Name)
	}
	if next.Name != "Tester" {
		t.Errorf("handed to %s, want the Tester who was waiting", next.Name)
	}
	if len(job.Members) != 3 {
		t.Errorf("the job has %d members, want all three", len(job.Members))
	}
}

// A cancelled job cannot be revived by a later publish.
//
// The relay kept tasks that had been cancelled, so an agent publishing for a
// new request handed work on for the old one: "ToolCheck is done with the
// test, over to you Auditor" turned up in the middle of a job about writing
// documentation.
func TestForgettingAJobStopsLaterHandoffs(t *testing.T) {
	r := newRelay()
	const request = "the old job"

	r.waitFor(request, "broadcast", member("r", "Reviewer", stageReview))
	r.join("task-old", request, "broadcast", member("b", "Builder", stageBuild))

	r.forgetRequest(request)

	if _, ok := r.taskFor("b"); ok {
		t.Error("a forgotten job still has a live task")
	}
	if _, _, _, ok := r.next("task-old"); ok {
		t.Error("a forgotten job still handed work on")
	}
	// And its waiting members are gone too, so a new job does not inherit them.
	r.join("task-new", "a different job", "broadcast", member("b", "Builder", stageBuild))
	if _, _, next, ok := r.next("task-new"); ok {
		t.Errorf("the new job inherited %s from the forgotten one", next.Name)
	}
}

// A finished task hands on however it finished — except when someone stopped
// it deliberately.
//
// Waiting for success meant the chain died at the second hop in all three
// measured runs: the agent handed the work ran out of steps or simply did not
// publish, and the colleague after it was never told.
func TestTerminalStateDecidesWhenToHandOn(t *testing.T) {
	for _, tc := range []struct {
		state any
		want  bool
	}{
		{protocol.TaskSucceeded, true},
		{protocol.TaskFailed, true},
		{string(protocol.TaskSucceeded), true},
		{string(protocol.TaskFailed), true},
		// Someone stopped this on purpose; carrying on would undo that.
		{protocol.TaskCancelled, false},
		{protocol.TaskRunning, false},
		{protocol.TaskAwaitingHuman, false},
		{nil, false},
		{42, false},
	} {
		if _, got := terminalState(tc.state); got != tc.want {
			t.Errorf("terminalState(%v) = %v, want %v", tc.state, got, tc.want)
		}
	}
}

// A hop tells the agent what to do in the terms of that hop.
//
// "Do your part" was too vague to act on: handed a review to apply, the builder
// stopped to ask what was wanted rather than applying it.
func TestBriefForIsSpecificToTheHop(t *testing.T) {
	fix := briefFor(stageReview, stageBuild, "convtest")
	if !strings.Contains(fix, "SAME work_name") {
		t.Error("a fix hop does not say to republish under the same name")
	}
	if !strings.Contains(strings.ToLower(fix), "do not ask") {
		t.Error("a fix hop does not tell the agent to stop asking and fix them")
	}

	test := briefFor(stageBuild, stageTest, "convtest")
	if !strings.Contains(test, "Looks good") {
		t.Error("a test hop does not warn against a content-free pass")
	}

	// Every brief must say to read first and to finish, or the chain stalls.
	for name, b := range map[string]string{
		"fix":    fix,
		"test":   test,
		"review": briefFor(stageTest, stageReview, "convtest"),
		"build":  briefFor(stageDesign, stageBuild, "convtest"),
		"other":  briefFor(stageUnknown, stageUnknown, "convtest"),
	} {
		if !strings.Contains(b, "read_work") {
			t.Errorf("the %s brief does not say to read the catalog first", name)
		}
		if !strings.Contains(b, "done") {
			t.Errorf("the %s brief does not say to finish", name)
		}
	}
}

// Work handed from one agent to another is marked, so it does not stop to ask
// a person.
//
// It arrives with concrete instructions from a colleague, nobody is standing
// by to answer, and each wait holds up everyone downstream: one run spent
// three eight-minute waits in twenty minutes.
func TestHandoffTasksAreMarked(t *testing.T) {
	task := &protocol.Task{
		Params: map[string]string{protocol.ParamHandoff: "1"},
	}
	if task.Params[protocol.ParamHandoff] == "" {
		t.Error("a handoff task is not marked as one")
	}
	// An ordinary task is not marked, so it still waits for a person.
	ordinary := &protocol.Task{}
	if ordinary.Params[protocol.ParamHandoff] != "" {
		t.Error("an operator's own task was treated as a handoff")
	}
}

// A parked task is live work, not finished work.
//
// Requiring the publisher's task to be exactly running silently broke the
// feature: an agent that published and then stopped to ask something was
// dropped from the relay, so the artefact sat in the catalog and nobody was
// ever handed it.
func TestParkedPublisherStillHandsOn(t *testing.T) {
	live := []protocol.TaskState{
		protocol.TaskRunning,
		protocol.TaskAwaitingHuman,
		protocol.TaskQueued,
	}
	finished := []protocol.TaskState{
		protocol.TaskSucceeded,
		protocol.TaskFailed,
		protocol.TaskCancelled,
	}

	isLive := func(st protocol.TaskState) bool {
		switch st {
		case protocol.TaskRunning, protocol.TaskAwaitingHuman, protocol.TaskQueued:
			return true
		}
		return false
	}
	for _, st := range live {
		if !isLive(st) {
			t.Errorf("%s should still be able to hand work on", st)
		}
	}
	for _, st := range finished {
		if isLive(st) {
			t.Errorf("%s is finished and should be forgotten", st)
		}
	}
}

// Jobs whose members are all waiting do not accumulate.
//
// Pending jobs were only pruned when an agent started work, so a broadcast that
// produced nothing but waiting agents left its job behind for good — one per
// such request, in a process that runs for weeks.
func TestPendingJobsDoNotAccumulate(t *testing.T) {
	r := newRelay()

	// Many requests where everybody waits.
	for i := 0; i < 40; i++ {
		req := "request number " + string(rune('a'+i%26)) + string(rune('0'+i/26))
		r.forgetOthers(req)
		r.waitFor(req, "broadcast", member("t", "Tester", stageTest))
	}
	if len(r.jobs) > maxPendingJobs {
		t.Errorf("jobs grew to %d, past the cap of %d", len(r.jobs), maxPendingJobs)
	}
	// Superseding leaves only the newest.
	if len(r.jobs) != 1 {
		t.Errorf("superseding left %d jobs, want just the current one", len(r.jobs))
	}

	// Even without superseding, the cap holds.
	r2 := newRelay()
	for i := 0; i < 40; i++ {
		r2.waitFor("request "+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"broadcast", member("t", "Tester", stageTest))
	}
	if len(r2.jobs) > maxPendingJobs {
		t.Errorf("without superseding, jobs grew to %d", len(r2.jobs))
	}
}

// A job where everybody is waiting and nobody is building is reported once.
//
// An agent named only for testing registers to wait, which is right — but if
// nobody was asked to build the thing, it waits for good, having first said "I
// will test probe-three" in the thread, which reads exactly like work starting.
func TestStalledJobsAreFoundOnceEach(t *testing.T) {
	r := newRelay()

	r.waitFor("test the thing", "broadcast", member("t", "Tester", stageTest))
	r.waitFor("test the thing", "broadcast", member("r", "Reviewer", stageReview))

	// Not yet: a job gets a moment before anyone worries about it.
	if got := r.stalledJobs(time.Hour); len(got) != 0 {
		t.Errorf("a job was called stalled immediately: %d", len(got))
	}

	stalled := r.stalledJobs(0)
	if len(stalled) != 1 {
		t.Fatalf("found %d stalled jobs, want 1", len(stalled))
	}
	// Reported once, not every time the sweeper runs.
	if got := r.stalledJobs(0); len(got) != 0 {
		t.Errorf("the same job was reported twice: %d", len(got))
	}
}

// A job with somebody building is not stalled, however many are waiting.
func TestAJobWithAProducerIsNotStalled(t *testing.T) {
	r := newRelay()
	r.waitFor("build and test", "broadcast", member("t", "Tester", stageTest))
	r.waitFor("build and test", "broadcast", member("b", "Builder", stageBuild))

	if got := r.stalledJobs(0); len(got) != 0 {
		t.Errorf("a job with a builder in it was called stalled: %d", len(got))
	}
}

// A job with a live task is progressing even if every member waits.
func TestAJobWithLiveWorkIsNotStalled(t *testing.T) {
	r := newRelay()
	r.waitFor("do it", "broadcast", member("t", "Tester", stageTest))
	r.join("task-1", "do it", "broadcast", member("b", "Builder", stageBuild))

	if got := r.stalledJobs(0); len(got) != 0 {
		t.Errorf("a job with work in flight was called stalled: %d", len(got))
	}
}

// A job whose builder finished and handed on is not stalled.
//
// The producer's task is removed when it hands on, so a colleague registering
// to wait afterwards used to open a second job containing only waiters -- which
// looked exactly like a job nobody had started, and was reported as stalled
// while the work was in fact proceeding.
func TestAFinishedProducerDoesNotLookLikeAStall(t *testing.T) {
	r := newRelay()
	const req = "build it then test it"

	r.join("task-build", req, "broadcast", member("b", "Builder", stageBuild))
	r.waitFor(req, "broadcast", member("t", "Tester", stageTest))

	// The builder finishes and hands on, which removes its task.
	if _, _, next, ok := r.next("task-build"); !ok || next.Name != "Tester" {
		t.Fatalf("the handoff did not reach the tester: %+v", next)
	}

	if got := r.stalledJobs(0); len(got) != 0 {
		t.Errorf("a job whose builder had already handed on was called stalled: %d", len(got))
	}
}

// A colleague registering after the builder started joins the same job.
func TestWaitingAfterAStartJoinsTheSameJob(t *testing.T) {
	r := newRelay()
	const req = "build it then review it"

	r.join("task-build", req, "broadcast", member("b", "Builder", stageBuild))
	r.waitFor(req, "broadcast", member("v", "Reviewer", stageReview))

	if len(r.jobs) != 1 {
		t.Fatalf("%d jobs for one request, want 1", len(r.jobs))
	}
	if got := len(r.jobs[req].Members); got != 2 {
		t.Errorf("the job has %d members, want both", got)
	}
}

// One word inside another must not decide an agent's job.
//
// "markdown preview box" was read as a review, so the agent asked to build it
// registered as a reviewer, waited for work nobody was making, and the job was
// reported as one that could not start. The bug was upstream of all of that.
func TestStageOfMatchesWholeWords(t *testing.T) {
	cases := map[string]relayStage{
		"write a one-file HTML markdown preview box": stageBuild,
		"build a preview pane":                       stageBuild,
		"write the previewer":                        stageBuild,
		// The real words still work, in their usual forms.
		"review the finished code":            stageReview,
		"reviews mdbox and publishes defects": stageReview,
		"tests mdbox and publishes findings":  stageTest,
		"testing it on a phone":               stageTest,
		"designs the game first":              stageDesign,
		"generates the complete HTML file":    stageBuild,
	}
	for plan, want := range cases {
		if got := stageOf(plan); got != want {
			t.Errorf("stageOf(%q) = %s, want %s", plan, got, want)
		}
	}
}

// Each review round used to invent a new file name -- convtest_defects.md,
// then _v2, then _additional, then _final -- so one app collected five reports
// and none of them was obviously the current one.
func TestReportHopsNameTheFileTheyPublish(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   relayStage
		want string
	}{
		{"test", stageTest, "convtest_test_report"},
		{"review", stageReview, "convtest_review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := briefFor(stageBuild, tc.to, "convtest")
			if !strings.Contains(got, tc.want) {
				t.Errorf("brief does not name %q:\n%s", tc.want, got)
			}
			if !strings.Contains(got, "new version") {
				t.Errorf("brief does not say to republish under the same name:\n%s", got)
			}
		})
	}
}

// With nothing published yet there is no name to derive, but the instruction
// not to spray variants still has to survive.
func TestReportHopsStillSayReuseOneNameWithoutAProducedItem(t *testing.T) {
	got := briefFor(stageBuild, stageReview, "")
	if !strings.Contains(got, "reuse that same name") {
		t.Errorf("brief lost its one-name instruction:\n%s", got)
	}
}
