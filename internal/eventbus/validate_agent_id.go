package eventbus

import (
	"log/slog"

	"github.com/google/uuid"
)

// validateAgentID is a publish-time observer that logs a warning when a
// DomainEvent carries a non-UUID AgentID. It does NOT block the publish —
// observability only. Acts as a safety net catching future drift before the
// event reaches a consumer that parses the field as a UUID and queries the DB
// with it.
//
// Log field name is `non_uuid_agent_id` — intentionally distinct from the
// standard `agent_id` field used elsewhere — to avoid collision with
// observability tooling that parses `agent_id` as a UUID.
//
// See docs/agent-identity-conventions.md for the full convention.
func validateAgentID(event DomainEvent) {
	if event.AgentID == "" {
		return // legitimate team-owned, tenant-scoped, or anonymous event
	}
	if _, err := uuid.Parse(event.AgentID); err != nil {
		slog.Warn("eventbus.non_uuid_agent_id",
			"event_type", event.Type,
			"non_uuid_agent_id", event.AgentID,
			"source_id", event.SourceID,
		)
	}
}
