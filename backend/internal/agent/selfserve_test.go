package agent

import (
	"strings"
	"testing"
)

// The second refusal is blunter, because the first is read as a suggestion.
//
// In a measured run every task stopped to ask twice, each stop costing another
// wait, despite the first reply asking it not to.
func TestSelfServeReplyHardensOnRepeat(t *testing.T) {
	first := selfServeReply(1)
	again := selfServeReply(2)

	if first == again {
		t.Fatal("asking twice gets the same answer as asking once")
	}
	if !strings.Contains(strings.ToLower(again), "already asked") {
		t.Error("the repeat reply does not say the question was already asked")
	}
	if !strings.Contains(strings.ToLower(again), "not be answered") {
		t.Error("the repeat reply does not say another ask is pointless")
	}
	// Both must leave a way out that is not silence.
	for name, msg := range map[string]string{"first": first, "repeat": again} {
		if !strings.Contains(msg, "fail action") {
			t.Errorf("the %s reply offers no way to give up cleanly", name)
		}
		if !strings.Contains(strings.ToLower(msg), "cannot undo") {
			t.Errorf("the %s reply does not warn against irreversible actions", name)
		}
	}
}
