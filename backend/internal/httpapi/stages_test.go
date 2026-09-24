package httpapi

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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

// A directly started task and a handoff must give the same account of how to
// reach shared work. They did not, and the agent that had not been handed
// anything went looking for the app in a desktop launcher.
func TestCatalogGuidanceIsSaidOnBothPaths(t *testing.T) {
	if !strings.Contains(partDescription(protocol.Instance{ID: "0123456789abcdef", Name: "Checker"}, "test it"), "already be on screen") {
		t.Error("a filed part lost the catalog guidance")
	}
	for _, want := range []string{"read_work", "application launcher", "already be on screen"} {
		if !strings.Contains(catalogGuidance, want) {
			t.Errorf("catalogGuidance no longer mentions %q", want)
		}
	}
}

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
