package event

import (
	"sync"
	"sync/atomic"
	"time"
)

// busDeliveryTimeout bounds how long Publish blocks on a guaranteed-delivery
// event before evicting a wedged subscriber (architecture §2.3). It is a var so
// tests can shorten it.
var busDeliveryTimeoutVar = 5 * time.Second

// subBufSize is the per-subscriber bounded channel capacity.
const subBufSize = 1024

// Bus is an in-memory pub/sub with per-subscriber bounded channels and a SPLIT
// drop policy (architecture §2.3). Droppable events (message.part.delta) use a
// non-blocking send; guaranteed-delivery events block with a bounded timeout and
// evict the subscriber on timeout.
type Bus struct {
	mu   sync.RWMutex
	subs map[uint64]*Subscriber
	seq  uint64
}

// Subscriber is one SSE consumer. The data channel is bounded; dropped counts
// deltas dropped under backpressure. The data channel (ch) is owned by the
// publisher and is NEVER closed from another goroutine (C1): closing a channel
// that a publisher may concurrently send on causes a panic that no select
// default can prevent. Instead, cancellation/eviction is signalled by closing
// the done channel via doneOnce, and the publisher selects on <-done.
type Subscriber struct {
	id      uint64
	ch      chan Event
	done    chan struct{}
	dropped atomic.Uint64

	doneOnce sync.Once
}

// NewBus creates an empty bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[uint64]*Subscriber)}
}

// Subscribe registers a new subscriber and returns it plus a cancel func that
// removes it from the bus and signals done (idempotent). It NEVER closes the
// data channel; a removed subscriber's ch is simply abandoned and GC'd. NO
// directory filter (architecture §7.2 / B3): every subscriber receives every
// event.
func (b *Bus) Subscribe() (*Subscriber, func()) {
	b.mu.Lock()
	b.seq++
	id := b.seq
	s := &Subscriber{id: id, ch: make(chan Event, subBufSize), done: make(chan struct{})}
	b.subs[id] = s
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
		s.signalDone() // NEVER close s.ch
	}
	return s, cancel
}

// signalDone closes the done channel exactly once. Idempotent across concurrent
// cancel()/evict() calls.
func (s *Subscriber) signalDone() {
	s.doneOnce.Do(func() { close(s.done) })
}

// Events returns the subscriber's receive channel. The channel is never closed;
// consumers must select on Done() to learn the subscriber was cancelled/evicted.
func (s *Subscriber) Events() <-chan Event { return s.ch }

// Done returns a channel closed when the subscriber is cancelled or evicted.
func (s *Subscriber) Done() <-chan struct{} { return s.done }

// Dropped returns the number of droppable events dropped for this subscriber.
func (s *Subscriber) Dropped() uint64 {
	return s.dropped.Load()
}

// Publish fans out an event to all subscribers using the per-type delivery
// policy (architecture §2.3).
func (b *Bus) Publish(ev Event) {
	// Snapshot subscribers under RLock so we don't hold the lock during a
	// potentially blocking guaranteed-delivery send.
	b.mu.RLock()
	subs := make([]*Subscriber, 0, len(b.subs))
	for _, s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	guaranteed := ev.GuaranteedDelivery()
	for _, s := range subs {
		if guaranteed {
			b.deliverGuaranteed(s, ev)
		} else {
			// Droppable: non-blocking send, count drops.
			select {
			case s.ch <- ev:
			default:
				s.dropped.Add(1)
			}
		}
	}
}

// deliverGuaranteed blocks with a bounded timeout; on timeout the subscriber is
// wedged, so it is evicted (removed + channel closed) rather than allowed to
// back-pressure the prompt worker indefinitely (architecture §2.3).
func (b *Bus) deliverGuaranteed(s *Subscriber, ev Event) {
	select {
	case s.ch <- ev:
	case <-time.After(busDeliveryTimeoutVar):
		b.evict(s)
	}
}

// evict removes a subscriber from the bus and closes its channel (idempotent).
func (b *Bus) evict(s *Subscriber) {
	b.mu.Lock()
	if _, ok := b.subs[s.id]; ok {
		delete(b.subs, s.id)
	}
	b.mu.Unlock()
	s.signalDone() // NEVER close s.ch (C1): signal done; ch is abandoned + GC'd
}

// SubscriberCount returns the current number of subscribers (test helper).
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
