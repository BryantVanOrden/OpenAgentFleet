package httpapi

import (
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func fleetOf(names ...string) []protocol.Instance {
	out := make([]protocol.Instance, 0, len(names))
	for i, n := range names {
		out = append(out, protocol.Instance{ID: "i" + string(rune('1'+i)), Name: n, State: protocol.InstanceRunning})
	}
	return out
}

// The ASK line is the one thing a chat reply may DO. It has to resolve the
// colleague the way fleet comms does — tolerant of casing and a typo — and
// refuse to guess when nobody matches.
func TestPeerAsksResolveLooselyAndRefuseToGuess(t *testing.T) {
	insts := fleetOf("Builder", "ToolCheck", "Auditor")
	body := "I can do the layout.\n\nASK Builder: send me the API schema you settled on\n" +
		"ask tool check: is the test harness green on main?\n" +
		"ASK Nobody: hello?\n" +
		"ASK builder:    \n" // empty request is dropped

	asks := peerAsksFrom(body, insts, "i3")
	if len(asks) != 2 {
		t.Fatalf("got %d asks, want 2: %+v", len(asks), asks)
	}
	if asks[0].PeerName != "Builder" || asks[0].Text != "send me the API schema you settled on" {
		t.Errorf("first ask wrong: %+v", asks[0])
	}
	if asks[1].PeerName != "ToolCheck" || asks[1].Text != "is the test harness green on main?" {
		t.Errorf("loose name match failed: %+v", asks[1])
	}
}

func TestPeerAsksNeverTargetSelfOrStoppedBots(t *testing.T) {
	insts := fleetOf("Builder", "Auditor")
	insts[1].State = protocol.InstanceStopped
	asks := peerAsksFrom("ASK Builder: ping\nASK Auditor: ping", insts, "i1")
	if len(asks) != 0 {
		t.Errorf("asking yourself or a stopped bot must be dropped, got %+v", asks)
	}
}

func TestChatRosterIsEmptyWhenAlone(t *testing.T) {
	s := &Server{}
	if got := s.chatRoster(fleetOf("Solo"), "i1"); got != "" {
		t.Errorf("a bot with no colleagues must not be told to consult them: %q", got)
	}
	roster := s.chatRoster(fleetOf("Solo", "Builder"), "i1")
	if roster == "" || !containsAll(roster, "Builder", "ASK <name>") {
		t.Errorf("roster should name the colleague and the ASK form: %q", roster)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !contains(s, p) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
