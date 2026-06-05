package event

import (
	"testing"
	"time"
)

// TestDeltaDroppableUnderFullBuffer verifies that message.part.delta is dropped
// (non-blocking) when the subscriber buffer is full, and the drop counter
// increments, without blocking Publish.
func TestDeltaDroppableUnderFullBuffer(t *testing.T) {
	b := NewBus()
	sub, cancel := b.Subscribe()
	defer cancel()

	// Fill the buffer completely with droppable events (never drained).
	for i := 0; i < subBufSize; i++ {
		b.Publish(NewMessagePartDelta("ses_1", "msg_1", "prt_1", "text", "x"))
	}

	// One more delta must be dropped, not block.
	done := make(chan struct{})
	go func() {
		b.Publish(NewMessagePartDelta("ses_1", "msg_1", "prt_1", "text", "overflow"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish of droppable event blocked on full buffer")
	}

	if sub.Dropped() == 0 {
		t.Fatalf("expected dropped > 0, got %d", sub.Dropped())
	}
}

// TestGuaranteedDeliveredToHealthyConsumer verifies a guaranteed event reaches a
// draining subscriber.
func TestGuaranteedDeliveredToHealthyConsumer(t *testing.T) {
	b := NewBus()
	sub, cancel := b.Subscribe()
	defer cancel()

	go b.Publish(NewSessionIdle("ses_1"))

	select {
	case ev := <-sub.Events():
		if ev.Type != TypeSessionIdle {
			t.Fatalf("got %q, want session.idle", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guaranteed event not delivered to healthy consumer")
	}
}

// TestGuaranteedEvictsWedgedConsumer verifies that a guaranteed event to a
// subscriber whose buffer is full and never drained evicts the subscriber
// (channel closed) rather than blocking forever. Uses a temporarily shortened
// timeout to keep the test fast.
func TestGuaranteedEvictsWedgedConsumer(t *testing.T) {
	// Shorten the delivery timeout for this test.
	orig := busDeliveryTimeoutVar
	busDeliveryTimeoutVar = 100 * time.Millisecond
	defer func() { busDeliveryTimeoutVar = orig }()

	b := NewBus()
	sub, cancel := b.Subscribe()
	defer cancel()

	// Fill the buffer; never drain.
	for i := 0; i < subBufSize; i++ {
		b.Publish(NewMessagePartDelta("ses_1", "msg_1", "prt_1", "text", "x"))
	}

	// Publishing a guaranteed event must block up to the timeout then evict.
	done := make(chan struct{})
	go func() {
		b.Publish(NewSessionIdle("ses_1"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish of guaranteed event did not return (no eviction)")
	}

	// After eviction the subscriber is signaled via Done() and removed from
	// the bus. Eviction never closes the data channel.
	select {
	case <-sub.Done():
		// evicted as expected
	case <-time.After(2 * time.Second):
		t.Fatal("evicted subscriber Done() was not signaled")
	}

	if c := b.SubscriberCount(); c != 0 {
		t.Fatalf("evicted subscriber not removed: count=%d", c)
	}
}
