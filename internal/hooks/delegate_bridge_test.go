package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
)

// --- fakes ---

// fakeBus records Subscribe calls and lets tests trigger handlers manually.
type fakeBus struct {
	handlers map[eventbus.EventType][]eventbus.DomainEventHandler
}

func newFakeBus() *fakeBus {
	return &fakeBus{handlers: make(map[eventbus.EventType][]eventbus.DomainEventHandler)}
}

func (b *fakeBus) Publish(_ eventbus.DomainEvent) {}
func (b *fakeBus) Subscribe(et eventbus.EventType, h eventbus.DomainEventHandler) func() {
	b.handlers[et] = append(b.handlers[et], h)
	return func() {}
}
func (b *fakeBus) Start(_ context.Context) {}
func (b *fakeBus) Drain(_ time.Duration) error { return nil }

func (b *fakeBus) trigger(ctx context.Context, et eventbus.EventType, ev eventbus.DomainEvent) {
	for _, h := range b.handlers[et] {
		_ = h(ctx, ev)
	}
}

// fakeCapturingDispatcher records events passed to Fire.
type fakeCapturingDispatcher struct {
	events []Event
}

func (f *fakeCapturingDispatcher) Fire(_ context.Context, ev Event) (FireResult, error) {
	f.events = append(f.events, ev)
	return FireResult{Decision: DecisionAllow}, nil
}

// --- tests ---

func TestSubscribeDelegateEvents_CompletedFiresSubagentStop(t *testing.T) {
	bus := newFakeBus()
	disp := &fakeCapturingDispatcher{}
	SubscribeDelegateEvents(bus, disp)

	delegationID := uuid.NewString()
	bus.trigger(context.Background(), eventbus.EventDelegateCompleted, eventbus.DomainEvent{
		Type:    eventbus.EventDelegateCompleted,
		Payload: eventbus.DelegateCompletedPayload{DelegationID: delegationID},
		Timestamp: time.Now(),
	})

	if len(disp.events) != 1 {
		t.Fatalf("expected 1 Fire call; got %d", len(disp.events))
	}
	got := disp.events[0]
	if got.HookEvent != EventSubagentStop {
		t.Errorf("expected EventSubagentStop; got %q", got.HookEvent)
	}
	if got.EventID != delegationID {
		t.Errorf("expected EventID=%q; got %q", delegationID, got.EventID)
	}
}

func TestSubscribeDelegateEvents_FailedFiresSubagentStop(t *testing.T) {
	bus := newFakeBus()
	disp := &fakeCapturingDispatcher{}
	SubscribeDelegateEvents(bus, disp)

	delegationID := uuid.NewString()
	bus.trigger(context.Background(), eventbus.EventDelegateFailed, eventbus.DomainEvent{
		Type:    eventbus.EventDelegateFailed,
		Payload: eventbus.DelegateFailedPayload{DelegationID: delegationID, Error: "timeout"},
		Timestamp: time.Now(),
	})

	if len(disp.events) != 1 {
		t.Fatalf("expected 1 Fire call; got %d", len(disp.events))
	}
	got := disp.events[0]
	if got.HookEvent != EventSubagentStop {
		t.Errorf("expected EventSubagentStop; got %q", got.HookEvent)
	}
	if got.EventID != delegationID {
		t.Errorf("expected EventID=%q; got %q", delegationID, got.EventID)
	}
}

func TestSubscribeDelegateEvents_NilInputs_NoPanic(t *testing.T) {
	// Should not panic with nil bus or nil dispatcher.
	SubscribeDelegateEvents(nil, &fakeCapturingDispatcher{})
	SubscribeDelegateEvents(newFakeBus(), nil)
}

func TestSubscribeDelegateEvents_UnknownPayload_NoFire(t *testing.T) {
	bus := newFakeBus()
	disp := &fakeCapturingDispatcher{}
	SubscribeDelegateEvents(bus, disp)

	// Trigger with unknown payload type — dispatcher should not be called.
	bus.trigger(context.Background(), eventbus.EventDelegateCompleted, eventbus.DomainEvent{
		Type:    eventbus.EventDelegateCompleted,
		Payload: "unexpected string payload",
	})

	if len(disp.events) != 0 {
		t.Errorf("expected 0 Fire calls for unknown payload; got %d", len(disp.events))
	}
}
