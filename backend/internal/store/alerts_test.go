package store

import (
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A notification is resolved when it is filed; a question stays open.
func TestNotificationsAreNotOpenAlerts(t *testing.T) {
	s, ctx := testStore(t)
	note := &protocol.Alert{Kind: protocol.AlertCompleted, Severity: "info", Title: "Task complete — test"}
	ask := &protocol.Alert{Kind: protocol.AlertStalled, Severity: "warn", Title: "Needs a person — test", NeedsReply: true}
	for _, a := range []*protocol.Alert{note, ask} {
		if err := s.CreateAlert(ctx, a); err != nil {
			t.Fatal(err)
		}
		id := a.ID
		t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM alerts WHERE id=$1`, id) })
	}
	open, err := s.ListAlerts(ctx, true, 500)
	if err != nil {
		t.Fatal(err)
	}
	var sawNote, sawAsk bool
	for _, a := range open {
		sawNote = sawNote || a.ID == note.ID
		sawAsk = sawAsk || a.ID == ask.ID
	}
	if sawNote || !sawAsk {
		t.Fatalf("open alerts hold the question and not the notification: note=%v ask=%v", sawNote, sawAsk)
	}
	all, _ := s.ListAlerts(ctx, false, 500)
	for _, a := range all {
		if a.ID == note.ID && a.ResolvedAt == nil {
			t.Fatal("the notification is in the history, resolved")
		}
	}
}
