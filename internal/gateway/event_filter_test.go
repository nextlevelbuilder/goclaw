package gateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- helpers ---

func testClient(role permissions.Role, userID string, tenantID uuid.UUID) *Client {
	return &Client{
		id:       "test-client",
		role:     role,
		userID:   userID,
		tenantID: tenantID,
	}
}

func testEvent(name string, payload any, tenantID uuid.UUID) bus.Event {
	return bus.Event{Name: name, Payload: payload, TenantID: tenantID}
}

// --- extractMapField ---

func TestExtractMapField_MapStringAny(t *testing.T) {
	payload := map[string]any{"userId": "alice", "count": 42}
	if got := extractMapField(payload, "userId"); got != "alice" {
		t.Errorf("got %q, want %q", got, "alice")
	}
	if got := extractMapField(payload, "missing"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	// non-string value
	if got := extractMapField(payload, "count"); got != "" {
		t.Errorf("got %q for non-string, want empty", got)
	}
}

func TestExtractMapField_MapStringString(t *testing.T) {
	payload := map[string]string{"userId": "bob"}
	if got := extractMapField(payload, "userId"); got != "bob" {
		t.Errorf("got %q, want %q", got, "bob")
	}
}

func TestExtractMapField_StructPayload_JSONFallback(t *testing.T) {
	type payload struct {
		UserID string `json:"userId"`
	}
	if got := extractMapField(payload{UserID: "carol"}, "userId"); got != "carol" {
		t.Errorf("got %q, want %q", got, "carol")
	}
}

func TestExtractMapField_Nil(t *testing.T) {
	if got := extractMapField(nil, "userId"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// --- extractEventUserID ---

func TestExtractEventUserID_AgentEvent(t *testing.T) {
	ae := agent.AgentEvent{UserID: "alice"}
	event := testEvent(protocol.EventAgent, ae, uuid.Nil)
	if got := extractEventUserID(event); got != "alice" {
		t.Errorf("got %q, want %q", got, "alice")
	}
}

func TestExtractEventUserID_AgentEventPtr(t *testing.T) {
	ae := &agent.AgentEvent{UserID: "bob"}
	event := testEvent(protocol.EventAgent, ae, uuid.Nil)
	if got := extractEventUserID(event); got != "bob" {
		t.Errorf("got %q, want %q", got, "bob")
	}
}

func TestExtractEventUserID_MapFallback(t *testing.T) {
	event := testEvent(protocol.EventAgent, map[string]any{"userId": "carol"}, uuid.Nil)
	if got := extractEventUserID(event); got != "carol" {
		t.Errorf("got %q, want %q", got, "carol")
	}
}

// --- extractTeamID ---

func TestExtractTeamID_TeamTaskEventPayload(t *testing.T) {
	te := protocol.TeamTaskEventPayload{TeamID: "team-1"}
	event := testEvent("team.task.created", te, uuid.Nil)
	if got := extractTeamID(event); got != "team-1" {
		t.Errorf("got %q, want %q", got, "team-1")
	}
}

func TestExtractTeamID_TeamTaskEventPayloadPtr(t *testing.T) {
	te := &protocol.TeamTaskEventPayload{TeamID: "team-2"}
	event := testEvent("team.task.created", te, uuid.Nil)
	if got := extractTeamID(event); got != "team-2" {
		t.Errorf("got %q, want %q", got, "team-2")
	}
}

func TestExtractTeamID_MapFallback_SnakeCase(t *testing.T) {
	event := testEvent("team.task.created", map[string]any{"team_id": "team-3"}, uuid.Nil)
	if got := extractTeamID(event); got != "team-3" {
		t.Errorf("got %q, want %q", got, "team-3")
	}
}

func TestExtractTeamID_MapFallback_CamelCase(t *testing.T) {
	event := testEvent("team.task.created", map[string]any{"teamId": "team-4"}, uuid.Nil)
	if got := extractTeamID(event); got != "team-4" {
		t.Errorf("got %q, want %q", got, "team-4")
	}
}

// --- clientCanReceiveEvent ---

var tenantA = uuid.MustParse("00000000-0000-0000-0000-000000000001")
var tenantB = uuid.MustParse("00000000-0000-0000-0000-000000000002")

func TestClientCanReceiveEvent_InternalEventsBlocked(t *testing.T) {
	c := testClient(permissions.RoleAdmin, "admin", tenantA)
	// cache.* events are internal-only
	event := testEvent("cache.invalidate", nil, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("internal cache events should be blocked")
	}
	// audit.log is internal
	event = testEvent(protocol.EventAuditLog, nil, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("audit log events should be blocked")
	}
}

func TestClientCanReceiveEvent_SystemEventsForAll(t *testing.T) {
	c := testClient(permissions.RoleViewer, "viewer", tenantA)
	for _, name := range []string{protocol.EventHealth, protocol.EventPresence, protocol.EventHeartbeat} {
		event := testEvent(name, nil, tenantA)
		if !clientCanReceiveEvent(c, event) {
			t.Errorf("viewer should receive system event %q", name)
		}
	}
}

func TestClientCanReceiveEvent_TenantIsolation(t *testing.T) {
	c := testClient(permissions.RoleAdmin, "admin", tenantA)
	// Event from a different tenant → blocked
	event := testEvent(protocol.EventAgent, nil, tenantB)
	if clientCanReceiveEvent(c, event) {
		t.Error("admin should NOT receive events from a different tenant")
	}
	// Same tenant → allowed
	event = testEvent(protocol.EventAgent, nil, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("admin should receive events from their own tenant")
	}
}

func TestClientCanReceiveEvent_NilTenantID_FailClosed(t *testing.T) {
	c := testClient(permissions.RoleAdmin, "admin", uuid.Nil)
	event := testEvent(protocol.EventAgent, nil, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("client with nil tenantID should be fail-closed")
	}
}

func TestClientCanReceiveEvent_UnscopedEvent_OwnerOnly(t *testing.T) {
	// Owner sees unscoped events
	owner := testClient(permissions.RoleOwner, "owner", tenantA)
	event := testEvent(protocol.EventAgent, nil, uuid.Nil)
	if !clientCanReceiveEvent(owner, event) {
		t.Error("owner should see unscoped events")
	}
	// Non-owner admin blocked from unscoped events
	admin := testClient(permissions.RoleAdmin, "admin", tenantA)
	if clientCanReceiveEvent(admin, event) {
		t.Error("non-owner admin should NOT see unscoped events (fail-closed)")
	}
}

func TestClientCanReceiveEvent_AgentEvent_FilteredByUserID(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	// Event for alice → delivered
	event := testEvent(protocol.EventAgent, agent.AgentEvent{UserID: "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own agent event")
	}
	// Event for bob → blocked
	event = testEvent(protocol.EventAgent, agent.AgentEvent{UserID: "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's agent event")
	}
}

func TestClientCanReceiveEvent_ChatEvent_FilteredByUserID(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent(protocol.EventChat, map[string]any{"userId": "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own chat event")
	}
	event = testEvent(protocol.EventChat, map[string]any{"userId": "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's chat event")
	}
}

func TestClientCanReceiveEvent_SessionUpdated_FilteredByUserID(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent(protocol.EventSessionUpdated, map[string]any{"userId": "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own session update")
	}
	event = testEvent(protocol.EventSessionUpdated, map[string]any{"userId": "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's session update")
	}
}

func TestClientCanReceiveEvent_CronEvent_FilteredByUserID(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent(protocol.EventCron, store.CronEvent{UserID: "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own cron event")
	}
	event = testEvent(protocol.EventCron, store.CronEvent{UserID: "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's cron event")
	}
}

func TestClientCanReceiveEvent_CronEvent_MapPayload(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent(protocol.EventCron, map[string]any{"userId": "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own cron event via map payload")
	}
}

func TestClientCanReceiveEvent_TraceEvent_FilteredByUserID(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent(protocol.EventTraceUpdated, map[string]any{"userId": "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own trace event")
	}
	event = testEvent(protocol.EventTraceUpdated, map[string]any{"userId": "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's trace event")
	}
}

func TestClientCanReceiveEvent_TeamEvent_FilteredByTeamAccess(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	c.SetTeamAccess([]string{"team-1"})

	event := testEvent("team.task.created", protocol.TeamTaskEventPayload{TeamID: "team-1"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive team event for accessible team")
	}
	event = testEvent("team.task.created", protocol.TeamTaskEventPayload{TeamID: "team-2"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive team event for inaccessible team")
	}
}

func TestClientCanReceiveEvent_AdminSeesTeamEvents(t *testing.T) {
	c := testClient(permissions.RoleAdmin, "admin", tenantA)
	// Admin sees all team events regardless of team access
	event := testEvent("team.task.created", protocol.TeamTaskEventPayload{TeamID: "any-team"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("admin should see all team events")
	}
}

func TestClientCanReceiveEvent_TenantAccessRevoked_ScopedToUser(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent(protocol.EventTenantAccessRevoked, map[string]any{"user_id": "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own tenant access revoked event")
	}
	event = testEvent(protocol.EventTenantAccessRevoked, map[string]any{"user_id": "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's tenant access revoked event")
	}
}

func TestClientCanReceiveEvent_AdminOnlyEvents_BlockedForNonAdmin(t *testing.T) {
	c := testClient(permissions.RoleOperator, "user", tenantA)
	for _, name := range []string{
		protocol.EventNodePairRequested,
		protocol.EventDevicePairReq,
		protocol.EventAgentLinkCreated,
		protocol.EventWorkspaceFileChanged,
	} {
		event := testEvent(name, nil, tenantA)
		if clientCanReceiveEvent(c, event) {
			t.Errorf("operator should NOT receive admin-only event %q", name)
		}
	}
}

func TestClientCanReceiveEvent_ExecApproval_FilteredByUserID(t *testing.T) {
	c := testClient(permissions.RoleOperator, "alice", tenantA)
	event := testEvent("exec.approval.requested", map[string]any{"userId": "alice"}, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("should receive own exec approval event")
	}
	event = testEvent("exec.approval.requested", map[string]any{"userId": "bob"}, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("should NOT receive other user's exec approval event")
	}
}

func TestClientCanReceiveEvent_SkillEvents_Broadcast(t *testing.T) {
	c := testClient(permissions.RoleOperator, "user", tenantA)
	event := testEvent("skill.installed", nil, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("operator should receive skill events")
	}
}

func TestClientCanReceiveEvent_ZaloPersonal_Blocked(t *testing.T) {
	c := testClient(permissions.RoleOperator, "user", tenantA)
	event := testEvent("zalo.personal.qr", nil, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("zalo personal events should be blocked for non-admin")
	}
}

func TestClientCanReceiveEvent_WhatsApp_Blocked(t *testing.T) {
	c := testClient(permissions.RoleOperator, "user", tenantA)
	event := testEvent("whatsapp.qr", nil, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("whatsapp events should be blocked for broadcast")
	}
}

func TestClientCanReceiveEvent_UnknownEvent_FailClosed(t *testing.T) {
	c := testClient(permissions.RoleOperator, "user", tenantA)
	event := testEvent("totally.unknown.event", nil, tenantA)
	if clientCanReceiveEvent(c, event) {
		t.Error("unknown events should be fail-closed for non-admin")
	}
}

func TestClientCanReceiveEvent_AdminSeesUnknownEvents(t *testing.T) {
	c := testClient(permissions.RoleAdmin, "admin", tenantA)
	event := testEvent("totally.unknown.event", nil, tenantA)
	if !clientCanReceiveEvent(c, event) {
		t.Error("admin should receive unknown events")
	}
}
