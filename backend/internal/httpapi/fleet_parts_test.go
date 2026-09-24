package httpapi

import (
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func tk(id, stage string, kind protocol.TicketKind, st protocol.TicketStatus) protocol.Ticket {
	return protocol.Ticket{ID: id, Stage: stage, Kind: kind, Status: st}
}

func TestATesterReviewsTheBuilds(t *testing.T) {
	build := tk("b", "build", protocol.TicketWork, protocol.TicketInProgress)
	p := planPart([]protocol.Ticket{build}, stageTest, false, false)
	if len(p.reviewFor) != 1 || p.reviewFor[0] != "b" || p.alsoWork {
		t.Fatalf("a tester becomes the build's reviewer, with no ticket of its own: %+v", p)
	}
}

func TestATesterWhoAnswersFirstWaitsInAPlaceholder(t *testing.T) {
	p := planPart(nil, stageTest, false, false)
	if p.kind != protocol.TicketReview || p.status != protocol.TicketBacklog {
		t.Fatalf("with nothing built yet the tester is parked: %+v", p)
	}
	// ...and the build that arrives later adopts it as its reviewer.
	placeholder := tk("ph", "test", protocol.TicketReview, protocol.TicketBacklog)
	b := planPart([]protocol.Ticket{placeholder}, stageBuild, false, false)
	if len(b.adopt) != 1 || b.adopt[0].ID != "ph" || b.status != protocol.TicketTodo {
		t.Fatalf("the build adopts the waiting tester: %+v", b)
	}
}

func TestATesterNamedFirstStartsAtOnce(t *testing.T) {
	p := planPart(nil, stageTest, true, false)
	if p.kind != protocol.TicketWork || p.status != protocol.TicketTodo || len(p.blockedBy) != 0 {
		t.Fatalf("named first, a tester tests what already exists: %+v", p)
	}
}

func TestDesignComesBeforeBuild(t *testing.T) {
	build := tk("b", "build", protocol.TicketWork, protocol.TicketTodo)
	d := planPart([]protocol.Ticket{build}, stageDesign, false, false)
	if len(d.blockExisting) != 1 || d.blockExisting[0] != "b" {
		t.Fatalf("a design makes the unstarted build wait for it: %+v", d)
	}
	started := build
	started.Status, started.TaskID = protocol.TicketInProgress, "run"
	if d := planPart([]protocol.Ticket{started}, stageDesign, false, false); len(d.blockExisting) != 0 {
		t.Fatal("a build already running is not held back")
	}
	design := tk("d", "design", protocol.TicketWork, protocol.TicketInProgress)
	if b := planPart([]protocol.Ticket{design}, stageBuild, false, false); len(b.blockedBy) != 1 || b.blockedBy[0] != "d" {
		t.Fatalf("a build filed after the design waits for it: %+v", b)
	}
}

func TestAReviewerVerifiesTheWholeRequest(t *testing.T) {
	if p := planPart(nil, stageReview, false, false); !p.verifier {
		t.Fatalf("the review stage verifies the request: %+v", p)
	}
	build := tk("b", "build", protocol.TicketWork, protocol.TicketTodo)
	if p := planPart([]protocol.Ticket{build}, stageReview, false, true); len(p.reviewFor) != 1 {
		t.Fatalf("with a verifier already, a second reviewer reviews the builds: %+v", p)
	}
}

func TestASecondTesterTestsAfterTheBuilds(t *testing.T) {
	build := tk("b", "build", protocol.TicketWork, protocol.TicketInProgress)
	build.ReviewerID = "checker"
	p := planPart([]protocol.Ticket{build}, stageTest, false, false)
	if !p.alsoWork || len(p.blockedBy) != 1 || p.blockedBy[0] != "b" {
		t.Fatalf("a second tester gets a ticket after the build: %+v", p)
	}
}

func TestFirstSentence(t *testing.T) {
	cases := map[string]string{
		"Build Fleet Tasks. Then test it.":       "Build Fleet Tasks",
		"Round two on Fleet Tasks!\nBuilder: go": "Round two on Fleet Tasks!",
		"just one":                               "just one",
	}
	for in, want := range cases {
		if got := firstSentence(in); got != want {
			t.Errorf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}
