package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Oaf answers with one JSON object per turn. The parser has to find it inside
// whatever prose a model wraps around it, and tell a tool call from a reply.
func TestParseOafStepFindsTheObjectAndTellsToolFromReply(t *testing.T) {
	st, err := parseOafStep("Sure. {\"tool\":\"shell\",\"args\":{\"command\":\"ls -la\"}} done")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Tool != "shell" || st.Args["command"] != "ls -la" {
		t.Errorf("step = %+v", st)
	}

	st, err = parseOafStep("```json\n{\"reply\":\"All set — the tests pass.\",\"done\":true}\n```")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.Tool != "" || st.Reply == "" || !st.Done {
		t.Errorf("step = %+v", st)
	}

	if _, err := parseOafStep("I just talked, no JSON here."); err == nil {
		t.Error("prose without JSON must not parse as a step")
	}
	if _, err := parseOafStep(`{"unrelated":1}`); err == nil {
		t.Error("an object with neither tool nor reply is not a step")
	}
}

// Braces inside strings must not confuse the balancer: a shell command with a
// brace expansion is exactly the kind of thing Oaf asks for.
func TestFirstJSONObjectHonoursStrings(t *testing.T) {
	in := `{"tool":"shell","args":{"command":"echo {a,b} \"}\""}} trailing {`
	got := firstJSONObject(in)
	want := `{"tool":"shell","args":{"command":"echo {a,b} \"}\""}}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if firstJSONObject("no braces") != "" {
		t.Error("expected empty for no object")
	}
}

// A session's folder has to be one the device exposes. Windows and POSIX
// spellings of the same folder must agree, and a sibling that merely shares a
// prefix ("/srv/app2" under "/srv/app") must not pass.
func TestUnderRoots(t *testing.T) {
	roots := []string{`C:\Users\me\Code`, "/srv/app"}
	cases := map[string]bool{
		`C:\Users\me\Code`:            true,
		`C:\Users\me\Code\proj\sub`:   true,
		`c:/users/me/code/proj`:       true,
		`C:\Users\me\Codex`:           false,
		`C:\Users\me`:                 false,
		"/srv/app":                    true,
		"/srv/app/x":                  true,
		"/srv/app2":                   false,
		"/srv/app/../../etc":          false,
		"":                            false,
	}
	for dir, want := range cases {
		if got := underRoots(dir, roots); got != want {
			t.Errorf("underRoots(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestParseEvery(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"30s": 30 * time.Second, "5m": 5 * time.Minute, "2h": 2 * time.Hour, "1d": 24 * time.Hour,
	} {
		got, err := parseEvery(in)
		if err != nil || got != want {
			t.Errorf("parseEvery(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "5", "10s", "soon", "-5m"} {
		if _, err := parseEvery(bad); err == nil {
			t.Errorf("parseEvery(%q) should fail", bad)
		}
	}
}

// The session-scoped commands refuse to run outside a session with a pointer
// to where they belong, rather than silently creating work nobody can find.
func TestGoalAndLoopNeedASession(t *testing.T) {
	s := &Server{}
	r := newReq()
	if res := s.cmdGoal(r, "ship it"); res.OK {
		t.Error("/goal outside a session must not start a job")
	}
	if res := s.cmdLoop(r, "5m check"); res.OK {
		t.Error("/loop outside a session must not start a job")
	}
}

// The model's view of a thread: operator lines as the user, Oaf's replies and
// its tool calls as the assistant, neighbours of the same role merged (every
// provider rejects two in a row), and the new line last.
func TestOafMessagesBuildsAlternatingRoles(t *testing.T) {
	sess := &protocol.OafSession{ID: "sess-test-" + time.Now().Format("150405.000000"), Name: "t"}
	conv := protocol.OafConversationID(sess.ID)
	ctx := context.Background()
	bus := vault.GlobalBus
	// The handler registers the thread on session create; do the same, or the
	// bus reroutes every message to broadcast for want of a conversation.
	bus.EnsureConversation(ctx, conv, sess.Name,
		[]string{protocol.OperatorMemberID, protocol.OafMemberID}, protocol.ConversationOaf)
	bus.SendMessageAs(ctx, conv, "", "alex", "u1", protocol.OafMemberID, "message", "list the files", nil)
	bus.SendMessageAs(ctx, conv, protocol.OafMemberID, oafName, "", protocol.OperatorMemberID, oafToolKind,
		"list_dir {\"path\":\".\"}", map[string]any{"tool": "list_dir", "result": "README.md\nhello.py"})
	bus.SendMessageAs(ctx, conv, protocol.OafMemberID, oafName, "", protocol.OperatorMemberID, "message", "Two files: README.md and hello.py.", nil)
	bus.SendMessageAs(ctx, conv, "", "alex", "u1", protocol.OafMemberID, "message", "run hello.py", nil)

	s := &Server{}
	msgs, err := s.oafMessages(ctx, newReq(), sess, "run hello.py")
	if err != nil {
		t.Fatalf("oafMessages: %v", err)
	}
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	// The tool call replays in the live loop's own shape -- the call as the
	// assistant's JSON, the result as a user turn -- so the model sees one
	// protocol throughout and never learns to write tool notation as prose.
	want := []string{connectors.RoleUser, connectors.RoleAssistant, connectors.RoleUser, connectors.RoleAssistant, connectors.RoleUser}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}
	if got := msgs[1].Text; !contains(got, `"tool":"list_dir"`) {
		t.Errorf("assistant turn is not the tool call: %q", got)
	}
	if got := msgs[2].Text; !contains(got, "TOOL RESULT (list_dir)") || !contains(got, "hello.py") {
		t.Errorf("tool result turn lost the result: %q", got)
	}
	if got := msgs[3].Text; !contains(got, "Two files") {
		t.Errorf("reply turn = %q", got)
	}
	if msgs[4].Text != "run hello.py" {
		t.Errorf("last turn = %q, want the new line", msgs[4].Text)
	}
}

