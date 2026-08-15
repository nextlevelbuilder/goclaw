package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Pub/Sub delivery ---

func TestPublishInbound_ConsumeInbound(t *testing.T) {
	mb := New()
	defer mb.Close()

	msg := InboundMessage{Channel: "telegram", Content: "hello"}
	mb.PublishInbound(msg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected to consume message")
	}
	if got.Content != "hello" {
		t.Fatalf("content mismatch: got %q, want %q", got.Content, "hello")
	}
}

func TestPublishOutbound_SubscribeOutbound(t *testing.T) {
	mb := New()
	defer mb.Close()

	msg := OutboundMessage{Channel: "discord", Content: "world"}
	mb.PublishOutbound(msg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected to receive message")
	}
	if got.Content != "world" {
		t.Fatalf("content mismatch: got %q, want %q", got.Content, "world")
	}
}

// --- Context cancellation ---

func TestConsumeInbound_ContextCancelled(t *testing.T) {
	mb := New()
	defer mb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, ok := mb.ConsumeInbound(ctx)
	if ok {
		t.Fatal("expected false on cancelled context")
	}
}

// --- TryPublish non-blocking ---

func TestTryPublishInbound_BufferFull(t *testing.T) {
	mb := &MessageBus{
		inbound:     make(chan InboundMessage, 1), // tiny buffer
		outbound:    make(chan OutboundMessage, 1),
		handlers:    make(map[string]MessageHandler),
		subscribers: make(map[string]EventHandler),
	}

	// First message fits
	if !mb.TryPublishInbound(InboundMessage{Content: "1"}) {
		t.Fatal("first message should succeed")
	}
	// Second message should be dropped (buffer full)
	if mb.TryPublishInbound(InboundMessage{Content: "2"}) {
		t.Fatal("second message should fail (buffer full)")
	}
}

func TestTryPublishOutbound_BufferFull(t *testing.T) {
	mb := &MessageBus{
		inbound:     make(chan InboundMessage, 1),
		outbound:    make(chan OutboundMessage, 1),
		handlers:    make(map[string]MessageHandler),
		subscribers: make(map[string]EventHandler),
	}

	if !mb.TryPublishOutbound(OutboundMessage{Content: "1"}) {
		t.Fatal("first message should succeed")
	}
	if mb.TryPublishOutbound(OutboundMessage{Content: "2"}) {
		t.Fatal("second message should fail (buffer full)")
	}
}

// --- Broadcast delivery ---

func TestBroadcast_DeliveredToAllSubscribers(t *testing.T) {
	mb := New()
	defer mb.Close()

	var count atomic.Int32
	mb.Subscribe("sub1", func(e Event) { count.Add(1) })
	mb.Subscribe("sub2", func(e Event) { count.Add(1) })
	mb.Subscribe("sub3", func(e Event) { count.Add(1) })

	mb.Broadcast(Event{Name: "test"})

	if got := count.Load(); got != 3 {
		t.Fatalf("expected 3 deliveries, got %d", got)
	}
}

// --- Broadcast panic recovery: panicking handler must NOT crash other subscribers ---

func TestBroadcast_PanickingHandler_DoesNotCrashBus(t *testing.T) {
	mb := New()
	defer mb.Close()

	var delivered atomic.Int32

	mb.Subscribe("panicker", func(e Event) {
		panic("subscriber exploded")
	})
	mb.Subscribe("normal", func(e Event) {
		delivered.Add(1)
	})

	// This must not panic — the bus should catch the panicking handler
	mb.Broadcast(Event{Name: "test"})

	// The normal handler may or may not be called depending on iteration order,
	// but the important thing is we didn't crash.
	// Broadcast a second time to verify bus is still functional.
	mb.Broadcast(Event{Name: "test2"})

	// After two broadcasts, normal handler should have been called at least once
	if got := delivered.Load(); got == 0 {
		t.Fatal("normal handler should have been called at least once after two broadcasts")
	}
}

// --- Subscribe / Unsubscribe ---

func TestUnsubscribe_StopsDelivery(t *testing.T) {
	mb := New()
	defer mb.Close()

	var count atomic.Int32
	mb.Subscribe("temp", func(e Event) { count.Add(1) })

	mb.Broadcast(Event{Name: "before"})
	if count.Load() != 1 {
		t.Fatal("expected delivery before unsubscribe")
	}

	mb.Unsubscribe("temp")
	mb.Broadcast(Event{Name: "after"})
	if count.Load() != 1 {
		t.Fatal("expected no delivery after unsubscribe")
	}
}

func TestSubscribe_OverwritesPrevious(t *testing.T) {
	mb := New()
	defer mb.Close()

	var first, second atomic.Int32
	mb.Subscribe("id1", func(e Event) { first.Add(1) })
	mb.Subscribe("id1", func(e Event) { second.Add(1) }) // overwrite

	mb.Broadcast(Event{Name: "test"})
	if first.Load() != 0 {
		t.Fatal("first handler should have been replaced")
	}
	if second.Load() != 1 {
		t.Fatal("second handler should have been called")
	}
}

// --- Handler registration ---

func TestRegisterHandler_GetHandler(t *testing.T) {
	mb := New()
	defer mb.Close()

	called := false
	mb.RegisterHandler("telegram", func(msg InboundMessage) error {
		called = true
		return nil
	})

	handler, ok := mb.GetHandler("telegram")
	if !ok {
		t.Fatal("expected handler to be registered")
	}
	_ = handler(InboundMessage{})
	if !called {
		t.Fatal("expected handler to be called")
	}

	_, ok = mb.GetHandler("nonexistent")
	if ok {
		t.Fatal("expected no handler for unregistered channel")
	}
}

// --- Concurrent safety ---

func TestBroadcast_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	mb := New()
	defer mb.Close()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Broadcast in a goroutine
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
				mb.Broadcast(Event{Name: "concurrent"})
			}
		}
	})

	// Subscribe/unsubscribe rapidly
	for range 100 {
		mb.Subscribe("rapid", func(e Event) {})
		mb.Unsubscribe("rapid")
	}

	close(done)
	wg.Wait()
	// No panic = success
}

func TestPublishInbound_ConcurrentProducers(t *testing.T) {
	mb := New()
	defer mb.Close()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			mb.TryPublishInbound(InboundMessage{Content: "msg"})
		}()
	}
	wg.Wait()
	// No panic = success
}
