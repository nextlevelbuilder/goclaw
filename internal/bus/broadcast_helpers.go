package bus

import "github.com/google/uuid"

// BroadcastForTenant broadcasts an event with explicit tenant scoping.
// Use this for events that carry tenant-specific data to ensure proper
// isolation in the WS event filter (event_filter.go).
func BroadcastForTenant(pub EventPublisher, name string, tenantID uuid.UUID, payload any) {
	pub.Broadcast(Event{Name: name, TenantID: tenantID, Payload: payload})
}
