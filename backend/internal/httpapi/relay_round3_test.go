package httpapi

import "testing"

// Round three of the Fleet Tasks soak, as the relay saw it: Builder started
// on the brief, Checker parked as the tester, Builder's message to Checker
// started Checker's part through a peer request, then Builder finished. The
// hand-off from Builder's finish has to find Checker as its successor.
func TestBuilderFinishingAfterAPeerRequestStillHandsToChecker(t *testing.T) {
	r := newRelay()
	builder := member("b", "Builder", stageBuild)
	tester := member("t", "Checker", stageTest)
	r.join("task-1", "round three", "broadcast", builder)
	r.waitFor("round three", "broadcast", tester)

	job, asker, ok := r.jobOfInstance("b")
	if !ok || asker.Name != "Builder" {
		t.Fatalf("Builder should be on the job: ok=%v asker=%s", ok, asker.Name)
	}
	if !r.claimRound(job, "checker-task", tester) {
		t.Fatal("Checker should be able to claim a round")
	}
	if own, ok := r.liveTaskOnJob(job, "t"); !ok || own != "checker-task" {
		t.Fatalf("Checker's peer-requested task should be live on the job, got %q %v", own, ok)
	}

	c, finished, successor, ok := r.next("task-1")
	if !ok {
		t.Fatal("Builder's finish should hand on")
	}
	if c != job || finished.Name != "Builder" || successor.Name != "Checker" {
		t.Fatalf("hand-off: job same=%v finished=%s successor=%s", c == job, finished.Name, successor.Name)
	}
}

// Builder's actual round-three clause: "run one curl against
// http://localhost:8001/tests.html to confirm the server is still up, send
// Checker one message with the addresses, then reply done". No verb the scorer
// knew, so the stage was unknown -- and an unknown finisher had no successor.
func TestAProducerWithNoKnownVerbStillHandsToTheTester(t *testing.T) {
	r := newRelay()
	builder := member("b", "Builder", stageUnknown)
	tester := member("t", "Checker", stageTest)
	r.join("task-1", "round three", "broadcast", builder)
	r.waitFor("round three", "broadcast", tester)
	_, _, successor, ok := r.next("task-1")
	if !ok || successor.Name != "Checker" {
		t.Fatalf("an unknown-stage producer should hand to the tester, got ok=%v successor=%q", ok, successor.Name)
	}
	if got := stageOf("Fleet Tasks v1 is served on port 8001. Fix every real defect Checker reports, republish the changed files, tell Checker what changed."); got != stageBuild {
		t.Errorf("a fix-and-republish clause is a builder's, got %s", got)
	}
}
