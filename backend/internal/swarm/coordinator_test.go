package swarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// A coordinator with the two things a swarm now needs to do anything.
//
// These tests changed shape rather than being added to. They previously
// asserted that CreateSwarm succeeded with no task runner and with nil members —
// which was true, and was the bug: a swarm was a row and a kickoff message, no
// agent was ever told about it, and passing no members made the API invent
// three bots that exist on no fleet. The behaviour they pinned is the behaviour
// the README listed as unfinished, so the assertions had to be inverted.

type fakeFleet struct {
	mu      sync.Mutex
	started []startedTask
	// failFor makes one instance refuse to start, as a busy bot would.
	failFor string
	// unknown is an instance the lookup does not recognise.
	unknown string
}

type startedTask struct {
	instanceID string
	goal       string
	source     string
}

func (f *fakeFleet) start(_ context.Context, instanceID, goal, source string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if instanceID == f.failFor {
		return "", errors.New("that bot is already running a task")
	}
	f.started = append(f.started, startedTask{instanceID, goal, source})
	return fmt.Sprintf("task-%d", len(f.started)), nil
}

func (f *fakeFleet) lookup(_ context.Context, instanceID string) (string, string, error) {
	if instanceID == f.unknown {
		return "", "", errors.New("instance not found")
	}
	switch instanceID {
	case "i-1":
		return "arch", "fullstack_dev", nil
	case "i-2":
		return "qa", "qa_ui_ux", nil
	case "i-3":
		return "sec", "cyber_ops", nil
	}
	return "bot-" + instanceID, "generalist", nil
}

// goalFor returns the most recent goal given to an instance.
//
// Most recent, not first: a member gets a mission goal at create and a review
// goal when a colleague publishes, and taking the first would always read back
// the mission.
func (f *fakeFleet) goalFor(instanceID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := ""
	for _, t := range f.started {
		if t.instanceID == instanceID {
			out = t.goal
		}
	}
	return out
}

func (f *fakeFleet) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func wired(f *fakeFleet) *Coordinator {
	c := NewCoordinator()
	c.Wire(f.start, f.lookup, nil)
	return c
}

func members() []protocol.SwarmMember {
	return []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "Lead Architect"},
		{InstanceID: "i-2", Role: "QA Auditor"},
	}
}

// ------------------------------------------------------------------- create ---

func TestCreateSwarmStartsRealWork(t *testing.T) {
	ctx := context.Background()
	fleet := &fakeFleet{}
	c := wired(fleet)

	sw, err := c.CreateSwarm(ctx, "Launch", "ship the release", members(), false)
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}

	// The whole point. Creating a swarm used to start nothing at all.
	if fleet.count() != 2 {
		t.Fatalf("started %d tasks, want one per member", fleet.count())
	}
	if sw.Status != protocol.SwarmStatusRunning {
		t.Errorf("Status = %q, want %q", sw.Status, protocol.SwarmStatusRunning)
	}
	if sw.ID == "" || sw.CreatedAt.IsZero() || sw.UpdatedAt.IsZero() {
		t.Error("id or timestamps not set")
	}
	if sw.Artifacts == nil {
		t.Error("Artifacts is nil; it should be [] not null")
	}

	// Each member's goal carries the mission, its own role, and who else is on
	// the team -- an agent that does not know it has colleagues will not
	// coordinate with them.
	goal := fleet.goalFor("i-1")
	for _, want := range []string{"ship the release", "Lead Architect", "qa", "QA Auditor"} {
		if !strings.Contains(goal, want) {
			t.Errorf("the lead's goal is missing %q:\n%s", want, goal)
		}
	}
	if !strings.Contains(goal, "message_peer") {
		t.Error("the goal does not tell the agent how to reach its teammates")
	}
}

func TestCreateSwarmRefusesWithNoMembers(t *testing.T) {
	fleet := &fakeFleet{}
	c := wired(fleet)

	// This used to succeed and fabricate "inst-lead", "inst-qa" and "inst-sec",
	// so the screen showed a running mission staffed entirely by bots that
	// exist on no fleet.
	_, err := c.CreateSwarm(context.Background(), "s", "m", nil, false)
	if err == nil {
		t.Fatal("a swarm with no members was accepted")
	}
	if !strings.Contains(err.Error(), "at least one member") {
		t.Errorf("error %q does not explain what is missing", err)
	}
	if fleet.count() != 0 {
		t.Error("tasks were started for a swarm that was rejected")
	}
}

func TestCreateSwarmRejectsAMemberThatIsNotOnTheFleet(t *testing.T) {
	fleet := &fakeFleet{unknown: "i-ghost"}
	c := wired(fleet)

	_, err := c.CreateSwarm(context.Background(), "s", "m", []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "Lead"},
		{InstanceID: "i-ghost", Role: "Ghost"},
	}, false)
	if err == nil {
		t.Fatal("a member that is not a real instance was accepted")
	}
	// Validated before anything is stored or started, so a swarm never exists
	// with a member that does not.
	if len(c.ListSwarms(context.Background())) != 0 {
		t.Error("the swarm was stored despite an invalid member")
	}
	if fleet.count() != 0 {
		t.Error("tasks were started for a swarm that was rejected")
	}
}

func TestCreateSwarmRejectsDuplicateMembers(t *testing.T) {
	c := wired(&fakeFleet{})
	_, err := c.CreateSwarm(context.Background(), "s", "m", []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "Lead"},
		{InstanceID: "i-1", Role: "Also lead"},
	}, false)
	if err == nil {
		t.Fatal("the same instance was accepted twice on one mission")
	}
}

func TestCreateSwarmRefusesWithNothingWiredUp(t *testing.T) {
	// Refusing beats recording a mission that will never run, which is what it
	// used to do.
	_, err := NewCoordinator().CreateSwarm(context.Background(), "s", "m", members(), false)
	if !errors.Is(err, ErrNoRunner) {
		t.Errorf("err = %v, want ErrNoRunner", err)
	}
}

func TestMemberNamesComeFromTheFleetNotTheCaller(t *testing.T) {
	c := wired(&fakeFleet{})
	sw, err := c.CreateSwarm(context.Background(), "s", "m", []protocol.SwarmMember{
		// The caller's name for the bot is wrong; the fleet's wins.
		{InstanceID: "i-1", InstanceName: "whatever-i-typed", Role: "Lead"},
	}, false)
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	if sw.Members[0].InstanceName != "arch" {
		t.Errorf("InstanceName = %q, want the fleet's own name", sw.Members[0].InstanceName)
	}
	// And the archetype is filled in from the instance rather than left blank.
	if sw.Members[0].ArchetypeID != "fullstack_dev" {
		t.Errorf("ArchetypeID = %q, want it resolved from the instance", sw.Members[0].ArchetypeID)
	}
}

func TestABusyMemberDoesNotCancelTheMission(t *testing.T) {
	fleet := &fakeFleet{failFor: "i-2"}
	c := wired(fleet)

	sw, err := c.CreateSwarm(context.Background(), "s", "m", members(), false)
	if err != nil {
		t.Fatalf("one busy bot should not fail the whole swarm: %v", err)
	}
	if sw.Status != protocol.SwarmStatusRunning {
		t.Errorf("Status = %q, want running: one member did start", sw.Status)
	}
	// Recorded on the blackboard rather than swallowed, and the member marked.
	var sawFailure bool
	for _, m := range sw.Messages {
		if strings.Contains(m.Content, "Could not start") {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Error("the failure to start a member is not on the blackboard")
	}
	for _, m := range sw.Members {
		if m.InstanceID == "i-2" && m.Status != "error" {
			t.Errorf("member i-2 status = %q, want error", m.Status)
		}
	}
}

func TestASwarmWhereNothingStartsIsNotReportedAsRunning(t *testing.T) {
	fleet := &fakeFleet{failFor: "i-1"}
	c := wired(fleet)

	sw, err := c.CreateSwarm(context.Background(), "s", "m",
		[]protocol.SwarmMember{{InstanceID: "i-1", Role: "Lead"}}, false)
	if err == nil {
		t.Error("a swarm where no member could start should report an error")
	}
	if sw != nil && sw.Status == protocol.SwarmStatusRunning {
		t.Error("a swarm with nothing working on it is reported as running")
	}
}

func TestCreateSwarmIssuesDistinctIDs(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})

	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		sw, err := c.CreateSwarm(ctx, "s", "m", members(), false)
		if err != nil {
			t.Fatalf("CreateSwarm: %v", err)
		}
		if seen[sw.ID] {
			t.Fatalf("duplicate swarm id %q — the second would overwrite the first", sw.ID)
		}
		seen[sw.ID] = true
	}
	if got := len(c.ListSwarms(ctx)); got != 5 {
		t.Errorf("ListSwarms returned %d swarms, want 5", got)
	}
}

// ------------------------------------------------------------------ messages ---

func TestPostMessage(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, err := c.CreateSwarm(ctx, "Launch", "ship it", members(), false)
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	before := len(sw.Messages)

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
	if len(got.Messages) != before+1 {
		t.Fatalf("Messages = %d, want %d", len(got.Messages), before+1)
	}
	if got.Messages[len(got.Messages)-1].ID != msg.ID {
		t.Error("the posted message is not on the blackboard")
	}
}

func TestMessageIDsAreDistinctWithinATick(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	// The start loop appends several messages in quick succession, and a
	// nanosecond timestamp alone is not an identity.
	sw, err := c.CreateSwarm(ctx, "s", "m", []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "a"}, {InstanceID: "i-2", Role: "b"}, {InstanceID: "i-3", Role: "c"},
	}, false)
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range sw.Messages {
		if seen[m.ID] {
			t.Fatalf("duplicate message id %q", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestEmptyMessagesAreRejected(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", members(), false)
	if _, err := c.PostMessage(ctx, sw.ID, "a", "b", "p", "   ", nil); err == nil {
		t.Error("an empty message was accepted onto the blackboard")
	}
}

// ----------------------------------------------------------------- artifacts ---

func TestPublishArtifactSendsItForPeerReview(t *testing.T) {
	ctx := context.Background()
	fleet := &fakeFleet{}
	c := wired(fleet)
	sw, err := c.CreateSwarm(ctx, "Launch", "ship it", members(), false)
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	startedByCreate := fleet.count()

	art, err := c.PublishArtifact(ctx, sw.ID, "Audit", "qa", "security_audit", "no findings")
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if art.SwarmID != sw.ID || art.Title != "Audit" || art.Author != "qa" {
		t.Errorf("got %+v", art)
	}

	// This is the part that did not exist. Publishing appended to a list and
	// stopped; ApprovedBy could never be filled in, so "verified deliverable"
	// meant nothing had verified it.
	if fleet.count() != startedByCreate+1 {
		t.Fatalf("started %d review tasks, want 1 (every member except the author)",
			fleet.count()-startedByCreate)
	}
	// The author does not review its own work.
	if fleet.goalFor("i-2") != "" && strings.Contains(fleet.goalFor("i-2"), "Peer review") {
		t.Error("the author was asked to review its own artifact")
	}
	reviewGoal := fleet.goalFor("i-1")
	for _, want := range []string{"Peer review", "Audit", "no findings"} {
		if !strings.Contains(reviewGoal, want) {
			t.Errorf("the review goal is missing %q:\n%s", want, reviewGoal)
		}
	}
	// The artifact is attacker-influenced content going into a prompt.
	if !strings.Contains(reviewGoal, "untrusted content") {
		t.Error("the review goal does not label the artifact as untrusted")
	}

	got, _ := c.GetSwarm(ctx, sw.ID)
	if len(got.Artifacts) != 1 || got.Artifacts[0].ID != art.ID {
		t.Errorf("artifact was not attached to the swarm: %+v", got.Artifacts)
	}
	if got.Status != protocol.SwarmStatusReviewing {
		t.Errorf("Status = %q, want reviewing", got.Status)
	}
}

func TestReviewRecordsAVerdict(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "Launch", "ship it", members(), false)
	art, err := c.PublishArtifact(ctx, sw.ID, "Audit", "qa", "security_audit", "no findings")
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}

	reviewed, err := c.ReviewArtifact(ctx, sw.ID, art.ID, "arch", true, "checked the scan output")
	if err != nil {
		t.Fatalf("ReviewArtifact: %v", err)
	}
	if len(reviewed.ApprovedBy) != 1 || reviewed.ApprovedBy[0] != "arch" {
		t.Errorf("ApprovedBy = %v, want [arch]", reviewed.ApprovedBy)
	}

	// Every non-author member has approved, so the mission is complete.
	got, _ := c.GetSwarm(ctx, sw.ID)
	if got.Status != protocol.SwarmStatusCompleted {
		t.Errorf("Status = %q, want completed once every reviewer approved", got.Status)
	}
	// The verdict is on the blackboard, with the notes.
	var sawVerdict bool
	for _, m := range got.Messages {
		if strings.Contains(m.Content, "APPROVED") && strings.Contains(m.Content, "scan output") {
			sawVerdict = true
		}
	}
	if !sawVerdict {
		t.Error("the verdict is not on the blackboard")
	}
}

func TestARejectionDoesNotCompleteTheMission(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "Lead"},
		{InstanceID: "i-2", Role: "QA"},
		{InstanceID: "i-3", Role: "Security"},
	}, false)
	art, _ := c.PublishArtifact(ctx, sw.ID, "Patch", "arch", "code_patch", "the diff")

	if _, err := c.ReviewArtifact(ctx, sw.ID, art.ID, "qa", true, "looks fine"); err != nil {
		t.Fatalf("ReviewArtifact: %v", err)
	}
	if _, err := c.ReviewArtifact(ctx, sw.ID, art.ID, "sec", false, "leaks a token"); err != nil {
		t.Fatalf("ReviewArtifact: %v", err)
	}

	got, _ := c.GetSwarm(ctx, sw.ID)
	if got.Status == protocol.SwarmStatusCompleted {
		t.Error("the mission completed with an outstanding rejection")
	}
	if contains(got.Artifacts[0].ApprovedBy, "sec") {
		t.Error("a rejecting reviewer is counted as an approver")
	}
}

func TestAReviewerCannotApproveTwice(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", []protocol.SwarmMember{
		{InstanceID: "i-1", Role: "Lead"},
		{InstanceID: "i-2", Role: "QA"},
		{InstanceID: "i-3", Role: "Security"},
	}, false)
	art, _ := c.PublishArtifact(ctx, sw.ID, "Patch", "arch", "code_patch", "the diff")

	// Two approvals from one reviewer must not stand in for two reviewers.
	for i := 0; i < 3; i++ {
		if _, err := c.ReviewArtifact(ctx, sw.ID, art.ID, "qa", true, "again"); err != nil {
			t.Fatalf("ReviewArtifact: %v", err)
		}
	}
	got, _ := c.GetSwarm(ctx, sw.ID)
	if n := len(got.Artifacts[0].ApprovedBy); n != 1 {
		t.Errorf("ApprovedBy has %d entries, want 1", n)
	}
	if got.Status == protocol.SwarmStatusCompleted {
		t.Error("one reviewer approving repeatedly completed a two-reviewer mission")
	}
}

func TestARejectionRetractsAnEarlierApproval(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", members(), false)
	art, _ := c.PublishArtifact(ctx, sw.ID, "Patch", "qa", "code_patch", "the diff")

	if _, err := c.ReviewArtifact(ctx, sw.ID, art.ID, "arch", true, "fine"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// A reviewer who spots something after approving must be able to say so.
	reviewed, err := c.ReviewArtifact(ctx, sw.ID, art.ID, "arch", false, "actually it breaks the build")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if contains(reviewed.ApprovedBy, "arch") {
		t.Error("the retracted approval is still counted")
	}
}

func TestReviewOfAnUnknownArtifactIsAnError(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", members(), false)
	if _, err := c.ReviewArtifact(ctx, sw.ID, "art-nope", "arch", true, ""); err == nil {
		t.Error("reviewing an artifact that does not exist succeeded")
	}
}

func TestAOneBotSwarmSaysThereIsNobodyToReview(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", []protocol.SwarmMember{{InstanceID: "i-1", Role: "Solo"}}, false)

	if _, err := c.PublishArtifact(ctx, sw.ID, "Note", "arch", "note", "body"); err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	got, _ := c.GetSwarm(ctx, sw.ID)
	// Reported, rather than silently treated as approved.
	var said bool
	for _, m := range got.Messages {
		if strings.Contains(m.Content, "No other member is available to review") {
			said = true
		}
	}
	if !said {
		t.Error("a swarm with nobody to review did not say so")
	}
	if got.Status == protocol.SwarmStatusCompleted {
		t.Error("an unreviewed artifact completed the mission")
	}
}

func TestEmptyArtifactsAreRejected(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", members(), false)
	if _, err := c.PublishArtifact(ctx, sw.ID, "", "qa", "note", "body"); err == nil {
		t.Error("an artifact with no title was accepted")
	}
	if _, err := c.PublishArtifact(ctx, sw.ID, "t", "qa", "note", ""); err == nil {
		t.Error("an artifact with no content was accepted")
	}
}

// ------------------------------------------------------------------- general ---

func TestUnknownSwarmIsAnError(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})

	if _, err := c.GetSwarm(ctx, "nope"); err == nil {
		t.Error("GetSwarm on an unknown id succeeded")
	}
	if _, err := c.PostMessage(ctx, "nope", "a", "b", "p", "c", nil); err == nil {
		t.Error("PostMessage on an unknown swarm succeeded")
	}
	if _, err := c.PublishArtifact(ctx, "nope", "t", "a", "c", "body"); err == nil {
		t.Error("PublishArtifact on an unknown swarm succeeded")
	}
	if _, err := c.ReviewArtifact(ctx, "nope", "art", "a", true, ""); err == nil {
		t.Error("ReviewArtifact on an unknown swarm succeeded")
	}
}

func TestWritesBumpUpdatedAt(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "Launch", "ship it", members(), false)
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
		t.Error("UpdatedAt went backwards after PublishArtifact")
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

func TestSnapshotsDoNotShareSlicesWithTheCoordinator(t *testing.T) {
	ctx := context.Background()
	c := wired(&fakeFleet{})
	sw, _ := c.CreateSwarm(ctx, "s", "m", members(), false)

	// The API serialises what GetSwarm returns while reviews append to the
	// coordinator's own copy. Handing out the live slices is a data race.
	snap, _ := c.GetSwarm(ctx, sw.ID)
	msgsBefore := len(snap.Messages)
	if _, err := c.PostMessage(ctx, sw.ID, "a", "all", "execution", "new", nil); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if len(snap.Messages) != msgsBefore {
		t.Error("a snapshot grew when the coordinator was written to")
	}
}

// ---------------------------------------------------------------- persistence ---

type memSwarmStore struct {
	mu   sync.Mutex
	rows map[string]protocol.SwarmTeam
}

func (m *memSwarmStore) UpsertSwarm(_ context.Context, s protocol.SwarmTeam) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[string]protocol.SwarmTeam{}
	}
	m.rows[s.ID] = s
	return nil
}

func (m *memSwarmStore) ListSwarms(_ context.Context) ([]protocol.SwarmTeam, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.SwarmTeam, 0, len(m.rows))
	for _, s := range m.rows {
		out = append(out, s)
	}
	return out, nil
}

func (m *memSwarmStore) DeleteSwarm(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}

func TestSwarmsSurviveARestart(t *testing.T) {
	ctx := context.Background()
	st := &memSwarmStore{}

	first := wired(&fakeFleet{})
	if err := first.AttachStore(ctx, st); err != nil {
		t.Fatalf("attach: %v", err)
	}
	sw, err := first.CreateSwarm(ctx, "Launch", "ship it", members(), false)
	if err != nil {
		t.Fatalf("CreateSwarm: %v", err)
	}
	art, _ := first.PublishArtifact(ctx, sw.ID, "Audit", "qa", "security_audit", "clean")
	if _, err := first.ReviewArtifact(ctx, sw.ID, art.ID, "arch", true, "verified"); err != nil {
		t.Fatalf("ReviewArtifact: %v", err)
	}

	// A peer review that vanishes on the next deploy is not a review.
	second := wired(&fakeFleet{})
	if err := second.AttachStore(ctx, st); err != nil {
		t.Fatalf("attach after restart: %v", err)
	}
	got, err := second.GetSwarm(ctx, sw.ID)
	if err != nil {
		t.Fatalf("the swarm did not survive the restart: %v", err)
	}
	if got.Mission != "ship it" {
		t.Errorf("Mission = %q", got.Mission)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("Artifacts = %d, want 1", len(got.Artifacts))
	}
	if !contains(got.Artifacts[0].ApprovedBy, "arch") {
		t.Errorf("the approval did not survive: %v", got.Artifacts[0].ApprovedBy)
	}
	if len(got.Messages) == 0 {
		t.Error("the blackboard did not survive")
	}
}

func TestDeleteSwarmRemovesTheRow(t *testing.T) {
	ctx := context.Background()
	st := &memSwarmStore{}
	c := wired(&fakeFleet{})
	if err := c.AttachStore(ctx, st); err != nil {
		t.Fatalf("attach: %v", err)
	}
	sw, _ := c.CreateSwarm(ctx, "s", "m", members(), false)

	c.DeleteSwarm(ctx, sw.ID)
	if _, err := c.GetSwarm(ctx, sw.ID); err == nil {
		t.Error("the swarm is still present after deletion")
	}
	if _, ok := st.rows[sw.ID]; ok {
		t.Error("the row is still in the store after deletion")
	}
}
