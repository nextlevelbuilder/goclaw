package heartbeat

import (
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ProviderResolver resolves LLM providers by tenant and name.
// Abstracts *providers.Registry for testability.
type ProviderResolver interface {
	GetForTenant(tenantID uuid.UUID, name string) (providers.Provider, error)
}

// EventPublisher publishes outbound messages.
// Abstracts *bus.MessageBus for testability.
type EventPublisher interface {
	PublishOutbound(msg bus.OutboundMessage)
}

// ActiveSessionChecker checks if a scheduler has active sessions for an agent.
// Abstracts *scheduler.Scheduler for testability.
type ActiveSessionChecker interface {
	HasActiveSessionsForAgent(agentKey string) bool
}
