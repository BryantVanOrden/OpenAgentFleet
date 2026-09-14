package httpapi

import (
	"fmt"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The brief that misfiled Builder as a tester on the live Fleet Notes run.
const builderBrief = "you own the code. In /home/agent/fleet-notes create a single-page notes app " +
	"in plain HTML, CSS and JavaScript (no build step): add, edit, delete and search notes, persist " +
	"them in localStorage. Serve it with python3 -m http.server 8000 from that folder and check every " +
	"feature yourself in Firefox on your desktop before you report. Write a README.md and a tests.html " +
	"page that exercises the note store in the browser and shows PASS or FAIL per case. When a version " +
	"is ready, tell Checker exactly what to test and how to reach it."

const checkerBrief = "you own quality. Whenever Builder reports a version, test it the way a real user " +
	"would, look for bugs, missing states and accessibility problems. Send Builder a numbered list of " +
	"problems with clear repro steps. Do not accept vague answers: verify each fix."

func TestStageOfScoresABuilderWhoMentionsTests(t *testing.T) {
	cases := map[string]relayStage{
		builderBrief: stageBuild,
		checkerBrief: stageTest,
		// Builder's own plan, which also promised to verify its work itself.
		"I'll build the Fleet Notes app in /home/agent/fleet-notes — index.html, styles.css, app.js, " +
			"README.md, and tests.html — then serve it with python3 -m http.server 8000 and verify " +
			"every feature myself in Firefox before I report": stageBuild,
		// File names are things, not roles.
		"write tests.html and test_store.py": stageBuild,
		// A tie still goes to the more specific reading.
		"I will write the test plan": stageTest,
		// Run 17: Checker's clause had no "test" in it and scored as a builder
		// on "write"; it started building instead of waiting for Builder.
		"you own quality. When Builder's part reaches you, open the addresses with the open_url " +
			"action, run through the app as a real user: add three tasks, edit one, complete one, " +
			"delete one. Write a numbered findings list ordered by severity with repro steps, " +
			"publish it with publish_work as fleet-tasks/findings.md": stageTest,
	}
	for plan, want := range cases {
		if got := stageOf(plan); got != want {
			t.Errorf("stageOf(%.60q...) = %s, want %s", plan, got, want)
		}
	}
}

func TestAPartThatWaitsOnAColleagueIsDownstream(t *testing.T) {
	yes := []string{
		"When Builder's part reaches you, open the addresses",
		"Whenever Builder reports a version, test it",
		"I will wait for Builder to confirm the server is up and send me the URLs",
		"once Designer publishes the spec, build it",
	}
	no := []string{
		"In /home/agent/fleet-tasks create a single-page task tracker",
		"I will build the app and tell Checker when it is ready",
	}
	for _, s := range yes {
		if !defersToColleague(s) {
			t.Errorf("%q waits on a colleague", s)
		}
	}
	for _, s := range no {
		if defersToColleague(s) {
			t.Errorf("%q does not wait on anyone", s)
		}
	}
}

func TestSystemFailuresAreTheMachinerysNotTheModels(t *testing.T) {
	yes := []string{
		"model would not produce a valid action: no JSON object in model reply: <tool_call>",
		"every model provider failed: dial tcp: connection refused",
		"could not observe the desktop: agentd 502",
	}
	no := []string{
		"agent gave up",
		"the site requires a login I do not have",
		"ran out of steps",
	}
	for _, e := range yes {
		if !systemFailure(e) {
			t.Errorf("%q should be a system failure", e)
		}
	}
	for _, e := range no {
		if systemFailure(e) {
			t.Errorf("%q is the model's verdict, not a system failure", e)
		}
	}
}

func TestRelayRetriesAPartTwiceThenHandsOn(t *testing.T) {
	r := newRelay()
	builder := member("b", "Builder", stageBuild)
	tester := member("t", "Checker", stageTest)
	r.join("task-1", "build it", "broadcast", builder)
	r.waitFor("build it", "broadcast", tester)

	job, who, ok := r.retry("task-1", "task-2")
	if !ok || who.InstanceID != "b" || job.Retries["b"] != 1 {
		t.Fatalf("first retry: ok=%v who=%s retries=%v", ok, who.Name, job.Retries)
	}
	if _, present := r.byTask["task-1"]; present {
		t.Fatal("the dead task should no longer own a place on the job")
	}
	if _, _, ok := r.retry("task-2", "task-3"); !ok {
		t.Fatal("second retry should be allowed")
	}
	if _, _, ok := r.retry("task-3", "task-4"); ok {
		t.Fatal("a third retry should be refused so the job can move on")
	}
	// The refused retry leaves the task in place, so the failure hands on.
	c, finished, successor, ok := r.next("task-3")
	if !ok || finished.InstanceID != "b" || successor.InstanceID != "t" {
		t.Fatalf("hand on after retries exhausted: ok=%v finished=%s successor=%s", ok, finished.Name, successor.Name)
	}
	if c.Round != 1 {
		t.Fatalf("round = %d, want 1", c.Round)
	}
}

// A marathon window continuing into a new task keeps its place on the job.
func TestRelayFollowsAContinuedWindow(t *testing.T) {
	r := newRelay()
	r.join("w1", "build it", "broadcast", member("b", "Builder", stageBuild))
	r.waitFor("build it", "broadcast", member("t", "Checker", stageTest))
	r.rebind("w1", "w2")
	r.rebind("w2", "w3")
	if _, present := r.byTask["w1"]; present {
		t.Fatal("the closed window should no longer own the job")
	}
	c, finished, successor, ok := r.next("w3")
	if !ok || finished.Name != "Builder" || successor.Name != "Checker" || c.Request != "build it" {
		t.Fatalf("the third window should hand on like the first: ok=%v finished=%s successor=%s", ok, finished.Name, successor.Name)
	}
}

func TestHandoffLineDoesNotCallAStallDone(t *testing.T) {
	b, c := member("b", "Builder", stageBuild), member("t", "Checker", stageTest)
	if got := handoffLine(b, c, "published index.html"); !strings.Contains(got, "Builder is done with the build") {
		t.Fatalf("a finished part is done: %q", got)
	}
	got := handoffLine(b, c, "They did not finish: stalled 3 times without making progress. Whatever they left in the catalog is what there is.")
	if strings.Contains(got, "is done") || !strings.Contains(got, "stopped short") || !strings.Contains(got, "stalled") {
		t.Fatalf("a stalled part is not done: %q", got)
	}
}

func TestMachinesNoteNamesTheOtherBotsHost(t *testing.T) {
	b := collaborator{InstanceID: "2f74f6ae-0bdb-4c37-b55c-b1e315810ecf", Name: "Builder", Stage: stageBuild}
	note := machinesNote(b)
	for _, want := range []string{"http://af-2f74f6ae-0bd:8000", "not on your disk", "read_work", "message_peer"} {
		if !strings.Contains(note, want) {
			t.Errorf("note should mention %q: %s", want, note)
		}
	}
	if sandboxHost("short") != "af-short" {
		t.Fatal("a short id is used whole")
	}
}

// A colleague's request inside a live job can start work, bounded by rounds.
func TestPeerRequestsSpendRoundsAndStopAtTheLimit(t *testing.T) {
	r := newRelay()
	r.join("build it", "build it", "broadcast", member("t", "Checker", stageTest))
	job, asker, ok := r.jobOfInstance("t")
	if !ok || asker.Name != "Checker" {
		t.Fatal("Checker is mid-task on the job")
	}
	if _, _, ok := r.jobOfInstance("nobody"); ok {
		t.Fatal("an idle bot has no job")
	}
	b := member("b", "Builder", stageBuild)
	for i := 0; i < maxRelayRounds; i++ {
		if !r.claimRound(job, fmt.Sprintf("peer-%d", i), b, "t") {
			t.Fatalf("round %d should be allowed", i)
		}
	}
	if r.claimRound(job, "peer-too-many", b, "t") {
		t.Fatal("the round limit should stop the ping-pong")
	}
	if len(job.Members) != 2 {
		t.Fatalf("Builder should be a member once, got %d members", len(job.Members))
	}
}

// The tenth brief: Checker is mentioned inside Builder's instructions before
// its own "Checker:" clause. Its assignment is the clause, not the mention.
func TestAssignmentPrefersTheColonClauseOverAnEarlierMention(t *testing.T) {
	brief := "Builder: read each file once, make sure the server is up, then publish the files and tell Checker " +
		"the URL of your machine. Team project: build and ship a small web app. Checker: you own quality. " +
		"Test it the way a real user would and verify each fix. Both of you: talk to each other."
	all := []protocol.Instance{{ID: "b", Name: "Builder"}, {ID: "t", Name: "Checker"}, {ID: "g", Name: "gui-scout"}}
	part := assignmentFor(brief, "Checker", all)
	if !strings.HasPrefix(part, "you own quality") {
		t.Fatalf("Checker's part should start at its clause, got %q", part)
	}
	if got := stageOf(part); got != stageTest {
		t.Fatalf("Checker is a tester, got %s from %q", got, part)
	}
	b := assignmentFor(brief, "Builder", all)
	if !strings.HasPrefix(b, "read each file once") || strings.Contains(b, "you own quality") {
		t.Fatalf("Builder's part should run up to Checker's clause, got %q", b)
	}
	// A brief without colons still works by first mention.
	if p := assignmentFor("Builder writes the game. Checker tests it.", "Checker", all); !strings.HasPrefix(p, "tests it") {
		t.Fatalf("plain mention fallback broke: %q", p)
	}
}

func TestPeerRequestsOnlyStartWorkForBotsOnTheJob(t *testing.T) {
	job := &collaboration{Request: "Builder builds it, Checker tests it.", Members: []collaborator{member("t", "Checker", stageTest)}}
	if !onJob(job, protocol.Instance{ID: "t", Name: "Checker"}) {
		t.Fatal("a member is on the job")
	}
	if !onJob(job, protocol.Instance{ID: "b", Name: "Builder"}) {
		t.Fatal("a bot named in the request is on the job")
	}
	if onJob(job, protocol.Instance{ID: "g", Name: "gui-scout"}) {
		t.Fatal("a bystander is not on the job")
	}
}
