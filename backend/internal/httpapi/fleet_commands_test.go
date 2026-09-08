package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The parts of the command layer that need no database: the parser, the
// catalogue's integrity, help, and the bot resolvers.

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in, name, args string
	}{
		{"/task @bob do the thing", "task", "@bob do the thing"},
		{"  /HELP  ", "help", ""},
		{"/mission   build a game  ", "mission", "build a game"},
		{"plain text", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		name, args := parseCommand(c.in)
		if name != c.name || args != c.args {
			t.Errorf("parseCommand(%q) = (%q, %q), want (%q, %q)", c.in, name, args, c.name, c.args)
		}
	}
}

func TestEveryCommandIsDispatchedAndDocumented(t *testing.T) {
	// A verb in the catalogue with no case in the switch answers "unknown
	// command" to the operator while advertising itself under /help.
	s := &Server{}
	for _, c := range fleetCommands {
		if c.Usage == "" || c.Description == "" || !strings.HasPrefix(c.Usage, "/"+c.Name) {
			t.Errorf("catalogue entry %q is malformed: %+v", c.Name, c)
		}
	}
	help := helpResult()
	for _, c := range fleetCommands {
		if !strings.Contains(help.Body, "`"+c.Usage+"`") {
			t.Errorf("/help does not list %s", c.Usage)
		}
	}
	// Unknown verbs are told so, with a pointer.
	res := s.runFleetCommand(newReq(), "frobnicate", "")
	if res.OK || !strings.Contains(res.Body, "/help") {
		t.Errorf("unknown command should fail with a /help hint: %+v", res)
	}
}

func newReq() *http.Request { return httptest.NewRequest(http.MethodPost, "/api/fleet/command", nil) }

func TestSplitBotRef(t *testing.T) {
	bot, rest := splitBotRef("@Builder ship the thing")
	if bot != "Builder" || rest != "ship the thing" {
		t.Errorf("got (%q, %q)", bot, rest)
	}
	bot, rest = splitBotRef("Builder")
	if bot != "Builder" || rest != "" {
		t.Errorf("bare name: got (%q, %q)", bot, rest)
	}
	if bot, _ := splitBotRef("   "); bot != "" {
		t.Error("blank args resolve to no bot")
	}
}

func TestResolveBotAnyStateIncludesPausedBots(t *testing.T) {
	insts := fleetOf("Builder", "Sleeper")
	insts[1].State = protocol.InstancePaused
	if _, ok := resolveBotByName(insts, "sleeper", ""); ok {
		t.Error("the running-only resolver must skip a paused bot")
	}
	got, ok := resolveBotAnyState(insts, "@sleeper")
	if !ok || got.Name != "Sleeper" {
		t.Errorf("/resume needs to find a paused bot: %+v %v", got, ok)
	}
}
