package bus

import (
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// recv takes one event off a subscription, failing fast rather than hanging the
// suite if the fan-out is broken.
func recv(t *testing.T, s *Subscription) protocol.Event {
	t.Helper()
	select {
	case ev, ok := <-s.C:
		if !ok {
			t.Fatal("subscription channel was closed")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return protocol.Event{}
	}
}

func expectEmpty(t *testing.T, s *Subscription) {
	t.Helper()
	select {
	case ev := <-s.C:
		t.Fatalf("expected no event, got %+v", ev)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPublishFansOutToEverySubscriber(t *testing.T) {
	b := New()
	a := b.Subscribe("")
	c := b.Subscribe("")
	defer a.Close()
	defer c.Close()

	b.Emit("task.step", "inst-1", "task-1", map[string]any{"step": 1})

	for i, s := range []*Subscription{a, c} {
		ev := recv(t, s)
		if ev.Type != "task.step" {
			t.Errorf("subscriber %d: Type = %q, want task.step", i, ev.Type)
		}
		if ev.InstanceID != "inst-1" || ev.TaskID != "task-1" {
			t.Errorf("subscriber %d: got %+v", i, ev)
		}
	}
}

func TestSubscribeFiltersByInstance(t *testing.T) {
	b := New()
	all := b.Subscribe("")
	one := b.Subscribe("inst-1")
	other := b.Subscribe("inst-2")
	defer all.Close()
	defer one.Close()
	defer other.Close()

	b.Emit("instance.state", "inst-1", "", "running")

	if ev := recv(t, all); ev.InstanceID != "inst-1" {
		t.Errorf("unfiltered subscriber got %+v", ev)
	}
	if ev := recv(t, one); ev.InstanceID != "inst-1" {
		t.Errorf("matching subscriber got %+v", ev)
	}
	expectEmpty(t, other)
}

func TestFleetWideEventsSkipInstanceFilteredSubscribers(t *testing.T) {
	// An event with no instance id (a fleet-level alert) does not match a
	// subscriber that asked for one specific instance.
	b := New()
	filtered := b.Subscribe("inst-1")
	defer filtered.Close()

	b.Emit("alert", "", "", "budget exceeded")
	expectEmpty(t, filtered)
}

func TestPublishStampsTheTimestamp(t *testing.T) {
	b := New()
	s := b.Subscribe("")
	defer s.Close()

	b.Publish(protocol.Event{Type: "chat"}) // At is zero
	ev := recv(t, s)
	if ev.At.IsZero() {
		t.Error("Publish left At zero; clients cannot order the stream")
	}
	if ev.At.Location() != time.UTC {
		t.Errorf("At is in %v, want UTC", ev.At.Location())
	}
}

func TestPublishPreservesAnExplicitTimestamp(t *testing.T) {
	b := New()
	s := b.Subscribe("")
	defer s.Close()

	want := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	b.Publish(protocol.Event{Type: "chat", At: want})
	if got := recv(t, s).At; !got.Equal(want) {
		t.Errorf("At = %v, want %v", got, want)
	}
}

func TestASlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	// This is the property the whole package exists for: a stalled WebSocket
	// client must never wedge the agent loop.
	b := New()
	slow := b.Subscribe("")
	defer slow.Close()

	total := subscriberBuffer + 25
	done := make(chan struct{})
	go func() {
		for i := 0; i < total; i++ {
			b.Emit("task.step", "inst-1", "task-1", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}

	subs, dropped := b.Stats()
	if subs != 1 {
		t.Errorf("subscribers = %d, want 1", subs)
	}
	if dropped != uint64(total-subscriberBuffer) {
		t.Errorf("dropped = %d, want %d", dropped, total-subscriberBuffer)
	}
	if len(slow.C) != subscriberBuffer {
		t.Errorf("buffered = %d, want the full buffer of %d", len(slow.C), subscriberBuffer)
	}
}

func TestCloseUnsubscribesAndIsIdempotent(t *testing.T) {
	b := New()
	s := b.Subscribe("")

	if subs, _ := b.Stats(); subs != 1 {
		t.Fatalf("subscribers = %d, want 1", subs)
	}
	s.Close()
	if subs, _ := b.Stats(); subs != 0 {
		t.Errorf("subscribers after Close = %d, want 0", subs)
	}
	if _, ok := <-s.C; ok {
		t.Error("the channel should be closed after Close")
	}
	// A double Close must not panic on a second close of the same channel.
	s.Close()

	// Publishing to a bus with no subscribers is a no-op, not a panic.
	b.Emit("task.step", "inst-1", "", nil)
	if subs, dropped := b.Stats(); subs != 0 || dropped != 0 {
		t.Errorf("Stats() = (%d, %d), want (0, 0)", subs, dropped)
	}
}

func TestClosingOneSubscriberLeavesTheOthers(t *testing.T) {
	b := New()
	a := b.Subscribe("")
	c := b.Subscribe("")
	defer c.Close()

	a.Close()
	b.Emit("alert", "inst-1", "", "still here")

	if ev := recv(t, c); ev.Type != "alert" {
		t.Errorf("surviving subscriber got %+v", ev)
	}
}

func TestConcurrentPublishAndSubscribe(t *testing.T) {
	// Run with -race in CI; this exists to give the detector something to chew.
	b := New()
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := b.Subscribe("")
			defer s.Close()
			for j := 0; j < 50; j++ {
				b.Emit("task.step", "inst-1", "task-1", j)
				select {
				case <-s.C:
				default:
				}
			}
		}()
	}
	wg.Wait()

	if subs, _ := b.Stats(); subs != 0 {
		t.Errorf("subscribers = %d after every goroutine closed, want 0", subs)
	}
}
