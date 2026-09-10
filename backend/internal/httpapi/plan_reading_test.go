package httpapi

import (
	"strings"
	"testing"
)

// Builder's actual reply to the fourth brief on 2026-09-09. It opens with a
// status report and commits in the second sentence; read as a status report,
// the builder was parked waiting to be handed its own work.
const builderStatusThenPlan = "I'm idle and available — nothing has actually run yet. I'll take the code: " +
	"read the existing index.html, styles.css, and app.js in /home/agent/fleet-notes, then rebuild " +
	"the notes app step by step (one file per step)."

func TestPlanFromReadsACommitmentAfterAStatusSentence(t *testing.T) {
	plan, ok := planFrom(builderStatusThenPlan)
	if !ok {
		t.Fatal("the second sentence commits to the code; that is a plan")
	}
	if !strings.Contains(plan, "take the code") {
		t.Fatalf("plan lost its content: %q", plan)
	}
}

func TestPlanFromFindsTheMarkerAfterAStatusSentence(t *testing.T) {
	plan, ok := planFrom("I have nothing running right now. PLAN: I'll write the review once Builder publishes.")
	if !ok || !strings.HasPrefix(plan, "I'll write the review") {
		t.Fatalf("got ok=%v plan=%q", ok, plan)
	}
	// The word inside a sentence is not the marker.
	if _, ok := planFrom("I have no plan: nothing was asked of me and I am idle."); ok {
		t.Fatal("'no plan:' mid-sentence is not a commitment")
	}
}

func TestPureStatusReportsStillDoNotStartWork(t *testing.T) {
	for _, body := range []string{
		"I'm standing by, idle and available — no work assigned to me this round.",
		"Nothing to report. My desktop shows an empty XFCE session.",
		"I am ready for whatever comes next and awaiting instructions.",
	} {
		if _, ok := planFrom(body); ok {
			t.Errorf("status report read as a plan: %q", body)
		}
	}
}
