package bus

import (
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// waitOrFail runs fn and fails the test if it has not returned in time. The
// point of every test here is that a publisher never blocks, so a hang is the
// failure mode being hunted rather than a flake.
func waitOrFail(t *testing.T, within time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("%s did not return within %s — the publisher is blocked", what, within)
	}
}

// A subscriber that has gone away must not wedge the agent loop, and must not
// cause a send on a closed channel either. Close and Publish take the same
// mutex, which is what makes this safe; the test pins that.
func TestPublishAfterCloseNeitherBlocksNorPanics(t *testing.T) {
	b := New()
	gone := b.Subscribe("")
	live := b.Subscribe("")

	gone.Close()

	waitOrFail(t, 2*time.Second, "Publish after a subscriber closed", func() {
		for i := 0; i < 200; i++ {
			b.Emit("agent.step", "inst-1", "task-1", i)
		}
	})

	// The surviving subscriber still gets traffic; the buffer is 64, so the
	// tail was dropped rather than blocking.
	select {
	case ev := <-live.C:
		if ev.Type != "agent.step" {
			t.Errorf("Type = %q", ev.Type)
		}
	default:
		t.Error("the live subscriber received nothing")
	}

	subs, dropped := b.Stats()
	if subs != 1 {
		t.Errorf("subscribers = %d, want 1 after one closed", subs)
	}
	if dropped == 0 {
		t.Error("expected the undrained subscriber to have dropped events past its buffer")
	}
}

// Closing subscribers while a publisher is mid-flight is the shutdown path when
// an operator closes a browser tab during a run. Run with -race.
func TestConcurrentCloseDuringPublish(t *testing.T) {
	const subscribers = 32

	b := New()
	subs := make([]*Subscription, subscribers)
	for i := range subs {
		subs[i] = b.Subscribe("")
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Publisher runs flat out until told to stop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				b.Emit("agent.step", "inst-1", "task-1", nil)
			}
		}
	}()

	// A couple of readers drain whatever is still open, so some sends succeed
	// and some hit the drop path while closes are happening.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for range subs[i].C {
			}
		}(i)
	}

	// Tear the subscribers down from several goroutines at once, including
	// double-closing the ones the readers are draining.
	var closers sync.WaitGroup
	for _, sub := range subs {
		closers.Add(1)
		go func(s *Subscription) {
			defer closers.Done()
			s.Close()
			s.Close() // idempotent: must not panic or double-close the channel
		}(sub)
	}
	closers.Wait()

	close(stop)
	wg.Wait()

	if n, _ := b.Stats(); n != 0 {
		t.Errorf("subscribers = %d after closing all of them, want 0", n)
	}
}

// A subscriber that never reads must not stall the fleet: once its buffer is
// full every further event is counted as dropped and Publish returns anyway.
func TestUndrainedSubscriberOnlyCostsDroppedEvents(t *testing.T) {
	b := New()
	stuck := b.Subscribe("")

	const sent = subscriberBuffer + 50
	waitOrFail(t, 2*time.Second, "Publish into a full subscriber buffer", func() {
		for i := 0; i < sent; i++ {
			b.Publish(protocol.Event{Type: "agent.step", InstanceID: "inst-1"})
		}
	})

	got := 0
	for {
		select {
		case <-stuck.C:
			got++
			continue
		default:
		}
		break
	}
	if got != subscriberBuffer {
		t.Errorf("buffered %d events, want exactly the %d-slot buffer", got, subscriberBuffer)
	}

	if _, dropped := b.Stats(); dropped != uint64(sent-subscriberBuffer) {
		t.Errorf("dropped = %d, want %d", dropped, sent-subscriberBuffer)
	}
	stuck.Close()
}
