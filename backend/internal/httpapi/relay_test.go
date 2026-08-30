package httpapi

import (
	"strings"
	"testing"

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
	fix := briefFor(stageReview, stageBuild)
	if !strings.Contains(fix, "SAME work_name") {
		t.Error("a fix hop does not say to republish under the same name")
	}
	if !strings.Contains(strings.ToLower(fix), "do not ask") {
		t.Error("a fix hop does not tell the agent to stop asking and fix them")
	}

	test := briefFor(stageBuild, stageTest)
	if !strings.Contains(test, "Looks good") {
		t.Error("a test hop does not warn against a content-free pass")
	}

	// Every brief must say to read first and to finish, or the chain stalls.
	for name, b := range map[string]string{
		"fix":    fix,
		"test":   test,
		"review": briefFor(stageTest, stageReview),
		"build":  briefFor(stageDesign, stageBuild),
		"other":  briefFor(stageUnknown, stageUnknown),
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
