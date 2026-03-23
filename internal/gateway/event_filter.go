package gateway

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// clientCanReceiveEvent checks whether a WS client should receive a given bus event.
// Admin clients receive all events. Non-admin clients are filtered by user/team scope.
func clientCanReceiveEvent(c *Client, event bus.Event) bool {
	// Internal events are never forwarded.
	if strings.HasPrefix(event.Name, "cache.") || event.Name == protocol.EventAuditLog {
		return false
	}

	// System-wide events go to everyone.
	if isSystemEvent(event.Name) {
		return true
	}

	// Tenant isolation: fail-closed, 3-mode filtering.
	//
	// Mode 1: Unscoped admin (crossTenant=true, tenantID=Nil) → see everything
	// Mode 2: Scoped admin  (crossTenant=true, tenantID=X)   → tenant X events + unscoped system events
	// Mode 3: Regular user   (crossTenant=false, tenantID=X)  → ONLY tenant X events (fail-closed)
	if !c.crossTenant && c.tenantID == uuid.Nil {
		return false // fail-closed: no tenant assigned to non-admin client
	} else if c.crossTenant && c.tenantID == uuid.Nil {
		// Mode 1: unscoped cross-tenant admin → no tenant filtering
	} else if c.tenantID != uuid.Nil {
		// Event has explicit tenant → must match client's tenant
		if event.TenantID != uuid.Nil && event.TenantID != c.tenantID {
			return false
		}
		// Event has no tenant → only cross-tenant admin (scoped) can see unscoped events
		if event.TenantID == uuid.Nil && !c.crossTenant {
			return false // fail-closed: regular users blocked from unscoped events
		}
	}

	// Admin sees everything (when not tenant-scoped, handled above).
	if permissions.HasMinRole(c.role, permissions.RoleAdmin) {
		return true
	}

	// Agent / chat events: filter by UserID.
	if event.Name == protocol.EventAgent || event.Name == protocol.EventChat {
		if uid := extractEventUserID(event); uid != "" {
			return uid == c.userID
		}
		return true // no routing context → broadcast (legacy)
	}

	// Session events: filter by UserID in payload.
	if event.Name == protocol.EventSessionUpdated {
		if uid := extractMapField(event.Payload, "userId"); uid != "" {
			return uid == c.userID
		}
		return true
	}

	// Cron events: filter by UserID in payload.
	if event.Name == protocol.EventCron {
		if ce, ok := event.Payload.(store.CronEvent); ok && ce.UserID != "" {
			return ce.UserID == c.userID
		}
		if uid := extractMapField(event.Payload, "userId"); uid != "" {
			return uid == c.userID
		}
		return true
	}

	// Trace events: filter by UserID.
	if event.Name == protocol.EventTraceUpdated {
		if uid := extractMapField(event.Payload, "userId"); uid != "" {
			return uid == c.userID
		}
		return true
	}

	// Team events: filter by TeamID.
	if strings.HasPrefix(event.Name, "team.") || strings.HasPrefix(event.Name, "delegation.") {
		if tid := extractTeamID(event); tid != "" {
			return c.hasTeamAccess(tid)
		}
		return true // no team context → broadcast
	}

	// Tenant access revocation: deliver to the affected user only.
	if event.Name == protocol.EventTenantAccessRevoked {
		if uid := extractMapField(event.Payload, "user_id"); uid != "" {
			return uid == c.userID
		}
		return false
	}

	// Admin-only events: pairing, node, agent links.
	if isAdminOnlyEvent(event.Name) {
		return false // non-admin clients don't receive these
	}

	// Exec approval events: scoped to the requesting user.
	if strings.HasPrefix(event.Name, "exec.approval.") {
		if uid := extractMapField(event.Payload, "userId"); uid != "" {
			return uid == c.userID
		}
		return true
	}

	// Zalo personal QR events: admin-only (channel management).
	if strings.HasPrefix(event.Name, "zalo.personal.") {
		return false
	}

	// Skill dep events → broadcast (non-sensitive, skill names only).
	if strings.HasPrefix(event.Name, "skill.") {
		return true
	}

	// Default: deny unknown events to non-admin (fail-closed).
	return false
}

// isSystemEvent returns true for events that should be broadcast to all clients.
func isSystemEvent(name string) bool {
	switch name {
	case protocol.EventHealth, protocol.EventPresence, protocol.EventVoicewakeChanged,
		protocol.EventTick, protocol.EventShutdown, protocol.EventConnectChallenge,
		protocol.EventTalkMode, protocol.EventHeartbeat:
		return true
	}
	return false
}

// isAdminOnlyEvent returns true for events that only admin clients should receive.
func isAdminOnlyEvent(name string) bool {
	switch name {
	case protocol.EventNodePairRequested, protocol.EventNodePairResolved,
		protocol.EventDevicePairReq, protocol.EventDevicePairRes,
		protocol.EventAgentLinkCreated, protocol.EventAgentLinkUpdated, protocol.EventAgentLinkDeleted,
		protocol.EventWorkspaceFileChanged:
		return true
	}
	return false
}

// extractEventUserID extracts UserID from agent.AgentEvent payload.
func extractEventUserID(event bus.Event) string {
	switch ae := event.Payload.(type) {
	case agent.AgentEvent:
		return ae.UserID
	case *agent.AgentEvent:
		return ae.UserID
	}
	return extractMapField(event.Payload, "userId")
}

// extractTeamID extracts TeamID from team event payloads.
func extractTeamID(event bus.Event) string {
	switch te := event.Payload.(type) {
	case protocol.TeamTaskEventPayload:
		return te.TeamID
	case *protocol.TeamTaskEventPayload:
		return te.TeamID
	}
	// Fallback: check map payloads.
	if tid := extractMapField(event.Payload, "team_id"); tid != "" {
		return tid
	}
	return extractMapField(event.Payload, "teamId")
}

// extractMapField extracts a string field from a map[string]any or map[string]string payload.
// Falls back to JSON unmarshaling for struct payloads.
func extractMapField(payload any, key string) string {
	switch m := payload.(type) {
	case map[string]any:
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	case map[string]string:
		return m[key]
	default:
		// Try JSON round-trip for struct payloads (lazy, used rarely).
		data, err := json.Marshal(payload)
		if err != nil {
			return ""
		}
		var parsed map[string]any
		if json.Unmarshal(data, &parsed) == nil {
			if v, ok := parsed[key]; ok {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}
