package hooks

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
)

// SubscribeDelegateEvents wires delegate.completed and delegate.failed eventbus
// events into SubagentStop hook invocations. Call once during startup after
// both the event bus and dispatcher are initialised.
// The SubagentStop hook is non-blocking — fire-and-forget via dispatcher.
func SubscribeDelegateEvents(bus eventbus.DomainEventBus, d Dispatcher) {
	if bus == nil || d == nil {
		return
	}

	handler := func(ctx context.Context, event eventbus.DomainEvent) error {
		var delegationID string
		switch p := event.Payload.(type) {
		case eventbus.DelegateCompletedPayload:
			delegationID = p.DelegationID
		case eventbus.DelegateFailedPayload:
			delegationID = p.DelegationID
		default:
			return nil
		}

		tenantID, _ := uuid.Parse(event.TenantID)
		agentID, _ := uuid.Parse(event.AgentID)

		ev := Event{
			EventID:   delegationID,
			SessionID: event.SourceID,
			TenantID:  tenantID,
			AgentID:   agentID,
			HookEvent: EventSubagentStop,
		}
		if _, err := d.Fire(ctx, ev); err != nil {
			slog.Warn("hooks.delegate_bridge.fire_error",
				"delegation_id", delegationID,
				"err", err,
			)
		}
		// SubagentStop is non-blocking; Updated* from FireResult is ignored
		// (delegate bridge has no mutation path in Wave 1).
		return nil
	}

	bus.Subscribe(eventbus.EventDelegateCompleted, handler)
	bus.Subscribe(eventbus.EventDelegateFailed, handler)
}
