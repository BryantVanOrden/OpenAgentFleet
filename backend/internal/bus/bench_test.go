package bus

import (
	"fmt"
	"sync"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// subscriberCounts models the realistic range: one operator watching a single
// instance, a small team on the fleet view, and a pathological hundred (every
// admin tab plus every phone in the building).
var subscriberCounts = []int{1, 10, 100}

func benchEvent() protocol.Event {
	return protocol.Event{
		Type:       "agent.step",
		InstanceID: "inst-bench",
		TaskID:     "task-bench",
		Payload:    map[string]any{"step": 42, "action": "click"},
	}
}

// BenchmarkPublishFanOut measures Publish alone — the call the agent loop makes
// on every step. Subscribers are drained by live readers so the buffers never
// fill and the drop path is not what is being timed.
func BenchmarkPublishFanOut(b *testing.B) {
	for _, n := range subscriberCounts {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			bus := New()
			subs := make([]*Subscription, n)
			var wg sync.WaitGroup
			for i := range subs {
				s := bus.Subscribe("")
				subs[i] = s
				wg.Add(1)
				go func(s *Subscription) {
					defer wg.Done()
					for range s.C {
					}
				}(s)
			}

			ev := benchEvent()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bus.Publish(ev)
			}
			b.StopTimer()

			for _, s := range subs {
				s.Close()
			}
			wg.Wait()
		})
	}
}

// BenchmarkPublishToDeliver measures the full publish -> every subscriber has
// the event path. This is the number that matters for streaming agent steps to
// a watching operator, since it includes the channel handoff and not just the
// enqueue.
func BenchmarkPublishToDeliver(b *testing.B) {
	for _, n := range subscriberCounts {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			bus := New()
			subs := make([]*Subscription, n)
			for i := range subs {
				subs[i] = bus.Subscribe("")
			}

			ev := benchEvent()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bus.Publish(ev)
				for _, s := range subs {
					<-s.C
				}
			}
			b.StopTimer()

			for _, s := range subs {
				s.Close()
			}
		})
	}
}

// BenchmarkPublishFilteredMiss times the case where every subscriber is pinned
// to a different instance, so the event matches none of them. The filter check
// happens under the same global lock as delivery, so a large fleet of
// single-instance watchers still pays for every unrelated event.
func BenchmarkPublishFilteredMiss(b *testing.B) {
	for _, n := range subscriberCounts {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			bus := New()
			subs := make([]*Subscription, n)
			for i := range subs {
				subs[i] = bus.Subscribe(fmt.Sprintf("other-inst-%d", i))
			}

			ev := benchEvent()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bus.Publish(ev)
			}
			b.StopTimer()

			for _, s := range subs {
				s.Close()
			}
		})
	}
}

// BenchmarkPublishParallel puts several agent loops on the bus at once, which is
// the real shape of a busy fleet: Publish takes an exclusive mutex, so this
// shows what that serialisation costs.
func BenchmarkPublishParallel(b *testing.B) {
	for _, n := range subscriberCounts {
		b.Run(fmt.Sprintf("subs=%d", n), func(b *testing.B) {
			bus := New()
			subs := make([]*Subscription, n)
			var wg sync.WaitGroup
			for i := range subs {
				s := bus.Subscribe("")
				subs[i] = s
				wg.Add(1)
				go func(s *Subscription) {
					defer wg.Done()
					for range s.C {
					}
				}(s)
			}

			ev := benchEvent()
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					bus.Publish(ev)
				}
			})
			b.StopTimer()

			for _, s := range subs {
				s.Close()
			}
			wg.Wait()
		})
	}
}
