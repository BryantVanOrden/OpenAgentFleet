package agent

import (
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// An agent reads only what its departments may see.
//
// The bug: read_work listed the whole catalog and matched on name, so a bot in
// one department could read another's published work by guessing what it was
// called. The HTTP layer filtered; the agent path did not.
func TestReadWorkIsScopedToTheBotsDepartments(t *testing.T) {
	support := &protocol.Instance{ID: "bot-s", OrgIDs: []string{"support"}}

	catalog := []protocol.WorkItem{
		{Name: "support notes", OrgID: "support", CreatedBy: "bot-s"},
		{Name: "engineering plan", OrgID: "engineering", CreatedBy: "bot-e"},
		{Name: "operator only", CreatedBy: ""},
		// Filed nowhere because its author is in several departments.
		{Name: "shared triage", CreatedBy: "bot-shared"},
		{Name: "other shared", CreatedBy: "bot-far"},
	}
	authors := map[string][]string{
		"bot-s":      {"support"},
		"bot-e":      {"engineering"},
		"bot-shared": {"support", "engineering"},
		"bot-far":    {"sales"},
	}

	got := map[string]bool{}
	for _, w := range readableWork(catalog, support, authors) {
		got[w.Name] = true
	}

	if !got["support notes"] {
		t.Error("a bot cannot read its own department's work")
	}
	if !got["shared triage"] {
		t.Error("a bot cannot read work from a colleague it shares a department with")
	}
	if got["engineering plan"] {
		t.Error("a support bot read engineering's work")
	}
	if got["other shared"] {
		t.Error("a support bot read work from a bot in no shared department")
	}
	if got["operator only"] {
		t.Error("a bot read work the operator filed nowhere, which is admin-only")
	}
}

// Its own work is always readable, even filed nowhere.
func TestABotCanAlwaysReadItsOwnWork(t *testing.T) {
	shared := &protocol.Instance{ID: "bot-x", OrgIDs: []string{"a", "b"}}
	catalog := []protocol.WorkItem{{Name: "mine", CreatedBy: "bot-x"}}

	if len(readableWork(catalog, shared, nil)) != 1 {
		t.Error("a bot in several departments cannot read back what it published")
	}
}
