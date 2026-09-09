package connectors

import (
	"time"
	"io"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

func combo(id string, roles map[string]string) protocol.ModelCombo {
	return protocol.ModelCombo{ID: id, Name: id, Roles: roles}
}

// The point of the feature: the same chain sends different roles to different
// models.
func TestChainRoutesRolesToDifferentModels(t *testing.T) {
	c := combo("combo-1", map[string]string{
		protocol.RoleVision:    "fast-eyes",
		protocol.RoleReasoning: "deep-brain",
	})
	entries := []string{"combo-1"}

	if got := ExpandChainForRole(entries, []protocol.ModelCombo{c}, protocol.RoleVision); !reflect.DeepEqual(got, []string{"fast-eyes"}) {
		t.Errorf("vision resolved to %v", got)
	}
	if got := ExpandChainForRole(entries, []protocol.ModelCombo{c}, protocol.RoleReasoning); !reflect.DeepEqual(got, []string{"deep-brain"}) {
		t.Errorf("reasoning resolved to %v", got)
	}
}

// A brain-and-hands pair must still answer roles it does not name, or picking
// two models would quietly stop the fleet summarising anything.
func TestSimpleComboFallsBackWithinItself(t *testing.T) {
	c := combo("combo-1", map[string]string{
		protocol.RoleVision:    "eyes",
		protocol.RoleReasoning: "brain",
	})
	combos := []protocol.ModelCombo{c}

	// Summarise and refine are analysis, so they land on the brain.
	for _, role := range []string{protocol.RoleSummarize, protocol.RoleRefine} {
		if got := ProviderForRole(c, role); got != "brain" {
			t.Errorf("%s resolved to %q, want brain", role, got)
		}
	}
	// Chat carries a screenshot, so it prefers the model that can see.
	if got := ProviderForRole(c, protocol.RoleChat); got != "eyes" {
		t.Errorf("chat resolved to %q, want eyes", got)
	}
	if got := ExpandChainForRole([]string{"combo-1"}, combos, protocol.RoleSummarize); len(got) != 1 {
		t.Errorf("summarise chain = %v", got)
	}
}

// Vision must never fall back to a model chosen for text. The registry drops
// vision-blind connectors when a screenshot is present, so a fallback here
// would produce an agent that is skipped every turn.
func TestVisionNeverFallsBackToATextRole(t *testing.T) {
	c := combo("combo-1", map[string]string{protocol.RoleReasoning: "text-only"})
	if got := ProviderForRole(c, protocol.RoleVision); got != "" {
		t.Errorf("vision fell back to %q; it must resolve to nothing", got)
	}
	// And the chain simply moves on rather than routing eyes to a text model.
	got := ExpandChainForRole([]string{"combo-1", "plain-vision"},
		[]protocol.ModelCombo{c}, protocol.RoleVision)
	if !reflect.DeepEqual(got, []string{"plain-vision"}) {
		t.Errorf("chain = %v, want the plain provider only", got)
	}
}

// A plain provider answers every role — that is the old single-model behaviour
// and it has to keep working.
func TestPlainProviderServesEveryRole(t *testing.T) {
	for _, role := range protocol.ModelRoles {
		got := ExpandChainForRole([]string{"solo"}, nil, role)
		if !reflect.DeepEqual(got, []string{"solo"}) {
			t.Errorf("%s resolved to %v", role, got)
		}
	}
}

// Combinations and providers mix in one chain, in the order given.
func TestComboAndProviderMixInOrder(t *testing.T) {
	c := combo("combo-1", map[string]string{protocol.RoleVision: "eyes"})
	got := ExpandChainForRole([]string{"combo-1", "backup"},
		[]protocol.ModelCombo{c}, protocol.RoleVision)
	if !reflect.DeepEqual(got, []string{"eyes", "backup"}) {
		t.Errorf("chain = %v, want [eyes backup]", got)
	}
}

// Two entries resolving to the same provider must not retry it twice: the
// second attempt is a duplicate of the failure that caused the fallback.

func TestResolvedChainDropsDuplicates(t *testing.T) {
	a := combo("a", map[string]string{protocol.RoleVision: "shared"})
	b := combo("b", map[string]string{protocol.RoleVision: "shared"})
	got := ExpandChainForRole([]string{"a", "b", "shared"},
		[]protocol.ModelCombo{a, b}, protocol.RoleVision)
	if !reflect.DeepEqual(got, []string{"shared"}) {
		t.Errorf("chain = %v, want one entry", got)
	}
}

// Deleting a provider or a combination must not break bots that referenced it.
func TestUnknownEntriesAreSkipped(t *testing.T) {
	c := combo("combo-1", map[string]string{protocol.RoleVision: "eyes"})
	got := ExpandChainForRole([]string{"combo-1", "deleted-combo-or-provider"},
		[]protocol.ModelCombo{c}, protocol.RoleVision)
	// The unknown entry is treated as a provider ID; the registry drops it when
	// it fails to build, which is where a deleted provider is already handled.
	if len(got) != 2 {
		t.Errorf("chain = %v", got)
	}
}

func TestSimpleClassification(t *testing.T) {
	simple := combo("s", map[string]string{
		protocol.RoleVision: "a", protocol.RoleReasoning: "b"})
	if !simple.Simple() {
		t.Error("brain + hands should classify as simple")
	}
	complexOne := combo("c", map[string]string{
		protocol.RoleVision: "a", protocol.RoleSummarize: "b"})
	if complexOne.Simple() {
		t.Error("a combination naming other roles is not simple")
	}
}

// A model that answers with nothing gets one more chance without the thinking
// pass before the chain gives up on it. Falling straight through sends the
// work to a model the operator ranked lower, and on the live fleet it failed
// the whole task instead.
type flakyThinker struct {
	calls      int
	sawNoThink bool
	visionOK   bool
}

func (f *flakyThinker) ID() string   { return "flaky" }
func (f *flakyThinker) Name() string { return "Flaky thinker" }
func (f *flakyThinker) Vision() bool { return f.visionOK }
func (f *flakyThinker) Complete(ctx context.Context, req Request) (*Response, error) {
	f.calls++
	if !req.DisableThinking {
		return nil, fmt.Errorf("flaky: %w (done true)", ErrEmptyCompletion)
	}
	f.sawNoThink = true
	return &Response{Text: "recovered"}, nil
}

func TestEmptyCompletionRetriesWithoutThinking(t *testing.T) {
	f := &flakyThinker{visionOK: true}
	r := NewRegistry(nil, nil, slog.Default())

	resp, err := r.complete(context.Background(), []Connector{f}, Request{})
	if err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if resp.Text != "recovered" {
		t.Errorf("got %q", resp.Text)
	}
	if f.calls != 2 {
		t.Errorf("called %d times, want 2 (the attempt and one retry)", f.calls)
	}
	if !f.sawNoThink {
		t.Error("the retry did not disable thinking")
	}
}

// A request that already had thinking off must not be retried: the retry would
// be identical, so it would just double the latency of a real failure.
func TestNoRetryWhenThinkingAlreadyOff(t *testing.T) {
	f := &alwaysEmpty{}
	r := NewRegistry(nil, nil, slog.Default())

	if _, err := r.complete(context.Background(), []Connector{f},
		Request{DisableThinking: true}); err == nil {
		t.Fatal("expected failure")
	}
	if f.calls != 1 {
		t.Errorf("called %d times, want 1", f.calls)
	}
}

type alwaysEmpty struct{ calls int }

func (a *alwaysEmpty) ID() string   { return "empty" }
func (a *alwaysEmpty) Name() string { return "Always empty" }
func (a *alwaysEmpty) Vision() bool { return true }
func (a *alwaysEmpty) Complete(ctx context.Context, req Request) (*Response, error) {
	a.calls++
	return nil, fmt.Errorf("empty: %w", ErrEmptyCompletion)
}

// A text-only chain must still be driven: the screenshot is dropped and the
// request goes through, because the turn also carries the element marks and
// the accessibility tree. Before this the step failed with "no vision-capable
// provider is enabled" and a fleet on a text model could not take one step.
type textOnly struct {
	calls int
	saw   Request
}

func (t *textOnly) ID() string   { return "text-only" }
func (t *textOnly) Name() string { return "Text only" }
func (t *textOnly) Vision() bool { return false }
func (t *textOnly) Complete(ctx context.Context, req Request) (*Response, error) {
	t.calls++
	t.saw = req
	return &Response{Text: `{"action":"done"}`}, nil
}

func TestBlindChainDropsTheScreenshotInsteadOfFailing(t *testing.T) {
	c := &textOnly{}
	r := NewRegistry(nil, nil, slog.Default())
	req := Request{Messages: []Message{{Role: RoleUser, Text: "ELEMENTS ...", Image: "AAAA", ImageMime: "image/webp"}}}

	resp, err := r.complete(context.Background(), []Connector{c}, req)
	if err != nil {
		t.Fatalf("blind chain failed: %v", err)
	}
	if resp.Text == "" || c.calls != 1 {
		t.Fatalf("calls = %d, resp = %+v", c.calls, resp)
	}
	if c.saw.Messages[0].Image != "" || c.saw.Messages[0].ImageMime != "" {
		t.Error("the screenshot reached a model that cannot see")
	}
	if c.saw.Messages[0].Text != "ELEMENTS ..." {
		t.Error("the text of the turn was altered")
	}
	if req.Messages[0].Image != "AAAA" {
		t.Error("the caller's request was mutated")
	}
}

// With one sighted model in the chain nothing is dropped: the blind one is
// skipped, as before, and the sighted one gets the picture.
func TestMixedChainKeepsTheScreenshotForTheSightedModel(t *testing.T) {
	blind := &textOnly{}
	sighted := &flakyThinker{visionOK: true}
	r := NewRegistry(nil, nil, slog.Default())
	req := Request{Messages: []Message{{Role: RoleUser, Text: "turn", Image: "AAAA"}}}

	if _, err := r.complete(context.Background(), []Connector{blind, sighted}, req); err != nil {
		t.Fatalf("mixed chain failed: %v", err)
	}
	if blind.calls != 0 {
		t.Error("the blind model was called although a sighted one was available")
	}
	if sighted.calls == 0 {
		t.Error("the sighted model was never called")
	}
}

// A dropped connection is retried before the chain gives up on a provider: a
// gateway restart should cost a step a few seconds, not the whole task.
type blinking struct {
	calls int
	fails int
	err   error
}

func (b *blinking) ID() string   { return "blink" }
func (b *blinking) Name() string { return "Blinking gateway" }
func (b *blinking) Vision() bool { return true }
func (b *blinking) Complete(ctx context.Context, req Request) (*Response, error) {
	b.calls++
	if b.calls <= b.fails {
		return nil, b.err
	}
	return &Response{Text: "back"}, nil
}

func TestTransientTransportErrorsAreRetried(t *testing.T) {
	old := transientWaits
	transientWaits = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { transientWaits = old }()

	c := &blinking{fails: 2, err: fmt.Errorf("gateway: Post \"http://x/v1/chat/completions\": %w", io.EOF)}
	r := NewRegistry(nil, nil, slog.Default())
	resp, err := r.complete(context.Background(), []Connector{c}, Request{})
	if err != nil {
		t.Fatalf("two EOFs then an answer should succeed: %v", err)
	}
	if resp.Text != "back" || c.calls != 3 {
		t.Errorf("calls = %d, resp = %+v; want 3 calls and the answer", c.calls, resp)
	}
}

func TestModelLevelErrorsAreNotRetried(t *testing.T) {
	old := transientWaits
	transientWaits = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { transientWaits = old }()

	c := &blinking{fails: 99, err: fmt.Errorf("gateway: http 400: unknown model")}
	r := NewRegistry(nil, nil, slog.Default())
	if _, err := r.complete(context.Background(), []Connector{c}, Request{}); err == nil {
		t.Fatal("expected the model error to surface")
	}
	if c.calls != 1 {
		t.Errorf("calls = %d, want 1: a 400 does not get better on repetition", c.calls)
	}
}
