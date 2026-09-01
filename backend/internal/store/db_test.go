package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// These exercise the SQL itself, which is the half of persistence a fake store
// cannot check: a column that does not exist, a placeholder off by one or a
// guard that never matches all look fine in Go and fail on the first real boot.
//
// Skipped unless AGENTFLEET_TEST_DSN points at a throwaway database, so
// `go test ./...` stays runnable with nothing installed:
//
//	AGENTFLEET_TEST_DSN=postgres://postgres@127.0.0.1:5432/agentfleet_test go test ./internal/store
func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("AGENTFLEET_TEST_DSN")
	if dsn == "" {
		t.Skip("set AGENTFLEET_TEST_DSN to run the store integration tests")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s, ctx
}

func TestWebhookRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	wh := &Webhook{
		Token:            "wh-round-trip",
		Name:             "CI",
		TargetInstanceID: "inst-1",
		TargetArchetype:  "fullstack_dev",
		GoalTemplate:     "run the tests for {{branch}}",
		Secret:           "s3cret",
		Active:           true,
	}
	if err := s.UpsertWebhook(ctx, wh); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteWebhook(ctx, wh.ID) })

	got := findWebhook(t, s, ctx, wh.ID)
	if got.Token != wh.Token || got.GoalTemplate != wh.GoalTemplate ||
		got.TargetInstanceID != "inst-1" || got.Secret != "s3cret" || !got.Active {
		t.Fatalf("row came back changed: %+v", got)
	}
	if got.LastTriggeredAt != nil {
		t.Errorf("a new webhook should have no last_triggered_at, got %v", got.LastTriggeredAt)
	}

	// Update in place rather than duplicating.
	wh.Name = "CI (renamed)"
	wh.Active = false
	if err := s.UpsertWebhook(ctx, wh); err != nil {
		t.Fatalf("update: %v", err)
	}
	got = findWebhook(t, s, ctx, wh.ID)
	if got.Name != "CI (renamed)" || got.Active {
		t.Fatalf("update did not land: %+v", got)
	}

	fired := time.Now().UTC().Truncate(time.Second)
	if err := s.TouchWebhook(ctx, wh.ID, fired); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got = findWebhook(t, s, ctx, wh.ID)
	if got.LastTriggeredAt == nil || !got.LastTriggeredAt.Equal(fired) {
		t.Fatalf("last_triggered_at = %v, want %v", got.LastTriggeredAt, fired)
	}

	if err := s.DeleteWebhook(ctx, wh.Token); err != nil {
		t.Fatalf("delete by token: %v", err)
	}
	if list, _ := s.ListWebhooks(ctx); containsWebhook(list, wh.ID) {
		t.Error("delete by token left the row behind")
	}
}

func findWebhook(t *testing.T, s *Store, ctx context.Context, id string) Webhook {
	t.Helper()
	list, err := s.ListWebhooks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, wh := range list {
		if wh.ID == id {
			return wh
		}
	}
	t.Fatalf("webhook %s not in the listing", id)
	return Webhook{}
}

func containsWebhook(list []Webhook, id string) bool {
	for _, wh := range list {
		if wh.ID == id {
			return true
		}
	}
	return false
}

// The claim is what stops a trigger firing twice in one minute, so it is tested
// against real SQL rather than trusted.
func TestClaimCronRunIsExactlyOncePerMinute(t *testing.T) {
	s, ctx := testStore(t)

	cr := &CronTrigger{
		Name: "nightly", ScheduleCron: "0 2 * * *",
		TargetArchetype: "cyber_ops", GoalTemplate: "scan", Active: true,
	}
	if err := s.UpsertCronTrigger(ctx, cr); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteCronTrigger(ctx, cr.ID) })

	minute := time.Date(2026, time.March, 4, 2, 0, 0, 0, time.UTC)
	won, err := s.ClaimCronRun(ctx, cr.ID, minute)
	if err != nil || !won {
		t.Fatalf("first claim: won=%v err=%v", won, err)
	}
	// A second orchestrator, or the same one after a restart inside the minute.
	won, err = s.ClaimCronRun(ctx, cr.ID, minute)
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Fatal("the same minute was claimed twice; the trigger would fire twice")
	}
	// The next occurrence is claimable again.
	won, err = s.ClaimCronRun(ctx, cr.ID, minute.AddDate(0, 0, 1))
	if err != nil || !won {
		t.Fatalf("next day's claim: won=%v err=%v", won, err)
	}

	var stored CronTrigger
	list, err := s.ListCronTriggers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ID == cr.ID {
			stored = c
		}
	}
	if stored.LastRunAt == nil || !stored.LastRunAt.Equal(minute.AddDate(0, 0, 1)) {
		t.Fatalf("last_run_at = %v, want the latest claim", stored.LastRunAt)
	}
	if stored.TargetArchetype != "cyber_ops" || stored.ScheduleCron != "0 2 * * *" {
		t.Errorf("row came back changed: %+v", stored)
	}
}

func TestPeerMessageRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	base := time.Now().UTC().Truncate(time.Millisecond)
	msgs := []protocol.PeerMessage{
		{ID: "pm-1", FromInstanceID: "a", FromInstanceName: "Alpha", ToInstanceID: "broadcast",
			Kind: "report", Content: "scan done", Data: map[string]any{"cves": float64(3)}, CreatedAt: base},
		{ID: "pm-2", FromInstanceID: "a", FromInstanceName: "Alpha", ToInstanceID: "b",
			Kind: "question", Content: "review it?", CreatedAt: base.Add(time.Second)},
		{ID: "pm-3", FromInstanceID: "c", FromInstanceName: "Gamma", ToInstanceID: "d",
			Kind: "message", Content: "unrelated", CreatedAt: base.Add(2 * time.Second)},
	}
	for _, m := range msgs {
		if err := s.InsertPeerMessage(ctx, m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
		t.Cleanup(func() {
			_, _ = s.pool.Exec(context.Background(), `DELETE FROM peer_messages WHERE id=$1`, m.ID)
		})
	}
	// Re-inserting the same id is a no-op, not a constraint violation: the bus
	// may replay a message it already wrote.
	if err := s.InsertPeerMessage(ctx, msgs[0]); err != nil {
		t.Fatalf("re-insert should be a no-op: %v", err)
	}

	got, err := s.ListPeerMessages(ctx, "b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("instance b sees %d messages, want the broadcast and its own direct one: %+v", len(got), got)
	}
	// Oldest first, which is the order the bus replays them in.
	if got[0].ID != "pm-1" || got[1].ID != "pm-2" {
		t.Fatalf("wrong order: %s, %s", got[0].ID, got[1].ID)
	}
	if got[0].Data["cves"] != float64(3) {
		t.Errorf("data_json lost: %+v", got[0].Data)
	}
	if got[0].FromInstanceName != "Alpha" || got[0].Kind != "report" {
		t.Errorf("fields lost: %+v", got[0])
	}

	// The limit keeps the newest, not the oldest.
	got, err = s.ListPeerMessages(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "pm-3" {
		t.Fatalf("limit 1 returned %+v, want the newest message", got)
	}
}

func TestMemoryRoundTrip(t *testing.T) {
	s, ctx := testStore(t)

	m := protocol.MemoryRecord{
		ID:               "mem-db-test",
		Namespace:        "fleet",
		Title:            "Godot shader stutter",
		Content:          "Enable shader caching.",
		Tags:             []string{"godot", "shader"},
		Embedding:        []float32{0.5, 0.25, 0},
		SourceTaskID:     "task-1",
		SourceInstanceID: "inst-1",
		CreatedAt:        time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.UpsertMemory(ctx, m); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteMemory(ctx, m.ID) })

	list, err := s.ListMemories(ctx, "fleet", 10)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.MemoryRecord
	for _, r := range list {
		if r.ID == m.ID {
			got = r
		}
	}
	if got.ID == "" {
		t.Fatal("memory not returned for its namespace")
	}
	if len(got.Tags) != 2 || got.Tags[0] != "godot" {
		t.Errorf("tags lost: %+v", got.Tags)
	}
	if len(got.Embedding) != 3 || got.Embedding[0] != 0.5 {
		t.Errorf("embedding lost: %+v", got.Embedding)
	}
	if got.SourceTaskID != "task-1" || got.SourceInstanceID != "inst-1" {
		t.Errorf("provenance lost: %+v", got)
	}

	// Re-storing the same id updates rather than erroring: the engine
	// re-embeds and writes through on every store.
	m.Content = "Updated: compile asynchronously too."
	if err := s.UpsertMemory(ctx, m); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	list, _ = s.ListMemories(ctx, "", 50)
	for _, r := range list {
		if r.ID == m.ID && r.Content != m.Content {
			t.Errorf("update did not land: %q", r.Content)
		}
	}
}
