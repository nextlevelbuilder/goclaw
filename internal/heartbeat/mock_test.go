package heartbeat

import (
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// mockProviderResolver implements ProviderResolver for testing.
type mockProviderResolver struct {
	provider providers.Provider
	err      error
}

func (m *mockProviderResolver) GetForTenant(_ uuid.UUID, _ string) (providers.Provider, error) {
	return m.provider, m.err
}

// mockEventPublisher implements EventPublisher for testing.
type mockEventPublisher struct {
	messages []bus.OutboundMessage
}

func (m *mockEventPublisher) PublishOutbound(msg bus.OutboundMessage) {
	m.messages = append(m.messages, msg)
}

// mockSessionChecker implements ActiveSessionChecker for testing.
type mockSessionChecker struct {
	active bool
}

func (m *mockSessionChecker) HasActiveSessionsForAgent(_ string) bool {
	return m.active
}
