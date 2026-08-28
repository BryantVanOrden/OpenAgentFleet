// Package bus is an in-process fan-out for live events. WebSocket clients (admin
// panel, Flutter app) each get a buffered subscriber; a slow reader is dropped
// rather than allowed to block the agent loop.
//
// Single-node only by design. Running several orchestrators behind a load
// balancer means swapping this for Redis pub/sub — the interface is deliberately
// narrow so that is a contained change.
package bus

import (
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

const subscriberBuffer = 64

type Subscription struct {
	C      chan protocol.Event
	id     uint64
	bus    *Bus
	filter string // instance id, or "" for everything
}

func (s *Subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if _, ok := s.bus.subs[s.id]; ok {
		delete(s.bus.subs, s.id)
		close(s.C)
	}
}

type Bus struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[uint64]*Subscription
	// dropped counts events discarded because a subscriber was too slow; the
	// health endpoint surfaces it so silent loss is visible.
	dropped uint64
}

func New() *Bus {
	return &Bus{subs: map[uint64]*Subscription{}}
}

// Subscribe returns a channel of events. Pass an instance id to receive only
// that instance's traffic, or "" for the whole fleet.
func (b *Bus) Subscribe(instanceID string) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	s := &Subscription{
		C:      make(chan protocol.Event, subscriberBuffer),
		id:     b.nextID,
		bus:    b,
		filter: instanceID,
	}
	b.subs[s.id] = s
	return s
}

func (b *Bus) Publish(ev protocol.Event) {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.filter != "" && s.filter != ev.InstanceID {
			continue
		}
		select {
		case s.C <- ev:
		default:
			b.dropped++
		}
	}
}

// Emit is the convenience form used throughout the agent loop.
func (b *Bus) Emit(kind, instanceID, taskID string, payload any) {
	b.Publish(protocol.Event{
		Type:       kind,
		InstanceID: instanceID,
		TaskID:     taskID,
		Payload:    payload,
		At:         time.Now().UTC(),
	})
}

func (b *Bus) Stats() (subscribers int, dropped uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs), b.dropped
}
