package swarm

import (
	"context"
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

func members() []protocol.SwarmMember {
	return []protocol.SwarmMember{
		{InstanceID: "i-1", InstanceName: "arch", Role: "Lead Architect", ArchetypeID: "fullstack_dev", Status: "idle"},
		{InstanceID: "i-2", InstanceName: "qa", Role: "QA Auditor", ArchetypeID: "qa_ui_ux", Status: "idle"},
	}
}

func TestCreateSwarm(t *testing.T) {
	ctx := context.Background()
	c := NewCoordinator()

	sw, err := c.CreateSwarm(ctx, "Launch", "ship the release", members())
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	if sw.ID == "" {
		t.Error("swarm has no id")
	}
	if sw.Name != "Launch" || sw.Mission != "ship the release" {
		t.Errorf("got name=%q mission=%q", sw.Name, sw.Mission)
	}
	// Status must land on running, not on the transient initializing value.
	if sw.Status != protocol.SwarmStatusRunning {
		t.Errorf("Status = %q, want %q", sw.Status, protocol.SwarmStatusRunning)
	}
	if len(sw.Members) != 2 {
		t.Errorf("Members = %d, want 2", len(sw.Members))
	}
	if sw.Artifacts == nil {
		t.Error("Artifacts is nil; it should be an empty slice so it marshals as [] not null")
	}
	if sw.CreatedAt.IsZero() || sw.UpdatedAt.IsZero() {
		t.Error("timestamps were not set")
	}

	// A kickoff message is posted to the blackboard so the members have context.
	if len(sw.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1 kickoff message", len(sw.Messages))
	}
	kickoff := sw.Messages[0]
	if kickoff.SwarmID != sw.ID {
		t.Errorf("kickoff SwarmID = %q, want %q", kickoff.SwarmID, sw.ID)
	}
	if kickoff.ToBot != "all" {
		t.Errorf("kickoff ToBot = %q, want all", kickoff.ToBot)
	}
	if kickoff.Phase != "planning" {
		t.Errorf("kickoff Phase = %q, want planning", kickoff.Phase)
	}
	if !strings.Contains(kickoff.Content, "ship the release") {
		t.Errorf("kickoff does not mention the mission: %q", kickoff.Content)
	}
}

func TestCreateSwarmIssuesDistinctIDs(t *testing.T) {
	ctx := context.Background()
	c := NewCoordinator()

	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		sw, err := c.CreateSwarm(ctx, "s", "m", nil)
		if err != nil {
			t.Fatalf("CreateSwarm: %v", err)
		}
		if seen[sw.ID] {
			t.Fatalf("duplicate swarm id %q — the second swarm would overwrite the first", sw.ID)
		}
		seen[sw.ID] = true
	}
	if got := len(c.ListSwarms(ctx)); got != 5 {
		t.Errorf("ListSwarms returned %d swarms, want 5", got)
	}
}

func TestPostMessage(t *testing.T) {
	ctx := context.Background()
	c := NewCoordinator()
	sw, _ := c.CreateSwarm(ctx, "Launch", "ship it", members())

	msg, err := c.PostMessage(ctx, sw.ID, "arch", "qa", "handoff", "branch is ready", []string{"art-1"})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if msg.FromBot != "arch" || msg.ToBot != "qa" || msg.Phase != "handoff" {
		t.Errorf("got %+v", msg)
	}
	if msg.Content != "branch is ready" {
		t.Errorf("Content = %q", msg.Content)
	}
	if len(msg.Artifacts) != 1 || msg.Artifacts[0] != "art-1" {
		t.Errorf("Artifacts = %v", msg.Artifacts)
	}
	if msg.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}

	got, err := c.GetSwarm(ctx, sw.ID)
	if err != nil {
		t.Fatalf("GetSwarm: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("Messages = %d, want kickoff + 1", len(got.Messages))
	}
	if got.Messages[1].ID != msg.ID {
		t.Errorf("the posted message is not on the blackboard")
	}
}

func TestPublishArtifact(t *testing.T) {
	ctx := context.Background()
	c := NewCoordinator()
	sw, _ := c.CreateSwarm(ctx, "Launch", "ship it", members())

	art, err := c.PublishArtifact(ctx, sw.ID, "Audit", "qa", "security_audit", "no findings")
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if art.SwarmID != sw.ID || art.Title != "Audit" || art.Author != "qa" {
		t.Errorf("got %+v", art)
	}
	if art.Category != "security_audit" || art.Content != "no findings" {
		t.Errorf("got %+v", art)
	}

	got, _ := c.GetSwarm(ctx, sw.ID)
	if len(got.Artifacts) != 1 || got.Artifacts[0].ID != art.ID {
		t.Errorf("artifact was not attached to the swarm: %+v", got.Artifacts)
	}
}

func TestUnknownSwarmIsAnError(t *testing.T) {
	ctx := context.Background()
	c := NewCoordinator()

	if _, err := c.GetSwarm(ctx, "nope"); err == nil {
		t.Error("GetSwarm on an unknown id succeeded")
	}
	if _, err := c.PostMessage(ctx, "nope", "a", "b", "p", "c", nil); err == nil {
		t.Error("PostMessage on an unknown swarm succeeded")
	}
	if _, err := c.PublishArtifact(ctx, "nope", "t", "a", "c", "body"); err == nil {
		t.Error("PublishArtifact on an unknown swarm succeeded")
	}
}

func TestWritesBumpUpdatedAt(t *testing.T) {
	ctx := context.Background()
	c := NewCoordinator()
	sw, _ := c.CreateSwarm(ctx, "Launch", "ship it", members())
	created := sw.UpdatedAt

	if _, err := c.PostMessage(ctx, sw.ID, "a", "all", "execution", "working", nil); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	afterMsg, _ := c.GetSwarm(ctx, sw.ID)
	if afterMsg.UpdatedAt.Before(created) {
		t.Errorf("UpdatedAt went backwards after PostMessage: %v -> %v", created, afterMsg.UpdatedAt)
	}

	if _, err := c.PublishArtifact(ctx, sw.ID, "t", "a", "c", "body"); err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	afterArt, _ := c.GetSwarm(ctx, sw.ID)
	if afterArt.UpdatedAt.Before(afterMsg.UpdatedAt) {
		t.Errorf("UpdatedAt went backwards after PublishArtifact")
	}
}

func TestListSwarmsOnAnEmptyCoordinator(t *testing.T) {
	got := NewCoordinator().ListSwarms(context.Background())
	if got == nil {
		t.Error("ListSwarms returned nil; it should be an empty slice so it marshals as []")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
