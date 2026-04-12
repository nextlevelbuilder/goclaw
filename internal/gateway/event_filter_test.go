package gateway

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// makeClient builds a minimal Client for filter tests.
// conn and server are nil — only role/userID/tenantID fields are used.
func makeClient(role permissions.Role, userID string, tenantID uuid.UUID) *Client {
	c := &Client{}
	c.role = role
	c.userID = userID
	c.tenantID = tenantID
	return c
}

// makeEvent builds a bus.Event for testing.
func makeEvent(name string, tenantID uuid.UUID, payload any) bus.Event {
	return bus.Event{Name: name, TenantID: tenantID, Payload: payload}
}

var masterTenant = uuid.MustParse("00000000-0000-0000-0000-000000000001")
var otherTenant = uuid.MustParse("00000000-0000-0000-0000-000000000002")

// ---- isSystemEvent ----

func TestIsSystemEvent_KnownNames(t *testing.T) {
	systemEvents := []string{
		protocol.EventHealth,
		protocol.EventPresence,
		protocol.EventVoicewakeChanged,
		protocol.EventTick,
		protocol.EventShutdown,
		protocol.EventConnectChallenge,
		protocol.EventTalkMode,
		protocol.EventHeartbeat,
	}
	for _, name := range systemEvents {
		if !isSystemEvent(name) {
			t.Errorf("isSystemEvent(%q) = false, want true", name)
		}
	}
}

func TestIsSystemEvent_UnknownName(t *testing.T) {
	if isSystemEvent("agent") {
		t.Error("isSystemEvent('agent') should return false")
	}
}

// ---- isAdminOnlyEvent ----

func TestIsAdminOnlyEvent_KnownAdminEvents(t *testing.T) {
	adminEvents := []string{
		protocol.EventNodePairRequested,
		protocol.EventDevicePairReq,
		protocol.EventAgentLinkCreated,
		protocol.EventWorkspaceFileChanged,
	}
	for _, name := range adminEvents {
		if !isAdminOnlyEvent(name) {
			t.Errorf("isAdminOnlyEvent(%q) = false, want true", name)
		}
	}
}

func TestIsAdminOnlyEvent_NonAdminEvent(t *testing.T) {
	if isAdminOnlyEvent(protocol.EventAgent) {
		t.Error("isAdminOnlyEvent('agent') should return false")
	}
}

// ---- Internal event filtering (cache., audit.log) ----

func TestClientCanReceiveEvent_InternalCacheEvent_Blocked(t *testing.T) {
	c := makeClient(permissions.RoleAdmin, "admin", masterTenant)
	c.role = permissions.RoleOwner // even owner shouldn't get cache events
	evt := makeEvent("cache.invalidate", masterTenant, nil)
	if clientCanReceiveEvent(c, evt) {
		t.Error("cache.* events must never be forwarded to any client")
	}
}

func TestClientCanReceiveEvent_AuditLog_Blocked(t *testing.T) {
	c := makeClient(permissions.RoleOwner, "admin", masterTenant)
	evt := makeEvent(protocol.EventAuditLog, masterTenant, nil)
	if clientCanReceiveEvent(c, evt) {
		t.Error("audit.log events must never be forwarded")
	}
}

// ---- System events broadcast to everyone ----

func TestClientCanReceiveEvent_SystemEvent_AllowedToAnyClient(t *testing.T) {
	c := makeClient(permissions.RoleViewer, "viewer", masterTenant)
	evt := makeEvent(protocol.EventHealth, uuid.Nil, nil) // system events have no tenant
	// system events bypass tenant checks and go to everyone
	if !clientCanReceiveEvent(c, evt) {
		t.Error("system events (health) should reach all clients")
	}
}

// ---- Tenant isolation ----

func TestClientCanReceiveEvent_NoTenantOnClient_Blocked(t *testing.T) {
	c := makeClient(permissions.RoleAdmin, "admin", uuid.Nil) // no tenant assigned
	evt := makeEvent(protocol.EventAgent, masterTenant, nil)
	if clientCanReceiveEvent(c, evt) {
		t.Error("client with no tenant should not receive any non-system events")
	}
}

func TestClientCanReceiveEvent_TenantMismatch_Blocked(t *testing.T) {
	c := makeClient(permissions.RoleAdmin, "admin", masterTenant)
	evt := makeEvent(protocol.EventAgent, otherTenant, nil) // different tenant
	if clientCanReceiveEvent(c, evt) {
		t.Error("event from different tenant should be blocked")
	}
}

func TestClientCanReceiveEvent_UnscopedEvent_OnlyOwnerReceives(t *testing.T) {
	// Event with no tenant — only owner-role clients should get it.
	ownerClient := makeClient(permissions.RoleOwner, "owner", masterTenant)
	adminClient := makeClient(permissions.RoleAdmin, "admin", masterTenant)

	evt := makeEvent(protocol.EventAgent, uuid.Nil, nil) // no tenant on event

	if !clientCanReceiveEvent(ownerClient, evt) {
		t.Error("owner should receive unscoped events")
	}
	if clientCanReceiveEvent(adminClient, evt) {
		t.Error("non-owner admin should NOT receive unscoped events (fail-closed)")
	}
}

// ---- User-scoped events (agent, chat) ----

func TestClientCanReceiveEvent_AgentEvent_FilteredByUserID(t *testing.T) {
	userA := makeClient(permissions.RoleOperator, "user-a", masterTenant)
	userB := makeClient(permissions.RoleOperator, "user-b", masterTenant)

	evt := makeEvent(protocol.EventAgent, masterTenant, map[string]any{"userId": "user-a"})

	if !clientCanReceiveEvent(userA, evt) {
		t.Error("user-a should receive their own agent event")
	}
	if clientCanReceiveEvent(userB, evt) {
		t.Error("user-b should NOT receive user-a's agent event")
	}
}

func TestClientCanReceiveEvent_AgentEvent_AdminSeesAll(t *testing.T) {
	admin := makeClient(permissions.RoleAdmin, "admin", masterTenant)
	evt := makeEvent(protocol.EventAgent, masterTenant, map[string]any{"userId": "user-x"})
	if !clientCanReceiveEvent(admin, evt) {
		t.Error("admin should receive all agent events")
	}
}

// ---- Team events ----

func TestClientCanReceiveEvent_TeamEvent_FilteredByTeamID(t *testing.T) {
	c := makeClient(permissions.RoleOperator, "user", masterTenant)
	c.SetTeamAccess([]string{"team-1"})

	evtMyTeam := makeEvent("team.task.created", masterTenant, map[string]any{"team_id": "team-1"})
	evtOtherTeam := makeEvent("team.task.created", masterTenant, map[string]any{"team_id": "team-2"})

	if !clientCanReceiveEvent(c, evtMyTeam) {
		t.Error("user should receive events for their team")
	}
	if clientCanReceiveEvent(c, evtOtherTeam) {
		t.Error("user should NOT receive events for teams they don't belong to")
	}
}

// ---- Admin-only events ----

func TestClientCanReceiveEvent_AdminOnlyEvent_BlockedForNonAdmin(t *testing.T) {
	c := makeClient(permissions.RoleOperator, "user", masterTenant)
	// Admin-only events land in the default deny path for non-admin.
	evt := makeEvent(protocol.EventNodePairRequested, masterTenant, nil)
	if clientCanReceiveEvent(c, evt) {
		t.Error("admin-only events should not reach operator-role clients")
	}
}

// ---- Skill events broadcast ----

func TestClientCanReceiveEvent_SkillEvent_BroadcastToTenantClients(t *testing.T) {
	ownerClient := makeClient(permissions.RoleOwner, "owner", masterTenant)
	// skill.deps.checked is a skill event — should broadcast
	evt := makeEvent(protocol.EventSkillDepsChecked, uuid.Nil, nil)
	// owner receives unscoped skill events
	if !clientCanReceiveEvent(ownerClient, evt) {
		t.Error("owner should receive skill events")
	}
}

// ---- Tenant access revocation ----

func TestClientCanReceiveEvent_TenantAccessRevoked_DeliveredToCorrectUser(t *testing.T) {
	userA := makeClient(permissions.RoleOperator, "user-a", masterTenant)
	userB := makeClient(permissions.RoleOperator, "user-b", masterTenant)

	evt := makeEvent(protocol.EventTenantAccessRevoked, masterTenant, map[string]any{"user_id": "user-a"})

	if !clientCanReceiveEvent(userA, evt) {
		t.Error("revocation event should be delivered to the affected user")
	}
	if clientCanReceiveEvent(userB, evt) {
		t.Error("revocation event should NOT be delivered to a different user")
	}
}

// ---- extractMapField ----

func TestExtractMapField_StringAnyMap(t *testing.T) {
	m := map[string]any{"userId": "abc", "teamId": "team-1"}
	if got := extractMapField(m, "userId"); got != "abc" {
		t.Errorf("extractMapField = %q, want %q", got, "abc")
	}
}

func TestExtractMapField_StringStringMap(t *testing.T) {
	m := map[string]string{"user_id": "xyz"}
	if got := extractMapField(m, "user_id"); got != "xyz" {
		t.Errorf("extractMapField = %q, want %q", got, "xyz")
	}
}

func TestExtractMapField_MissingKey(t *testing.T) {
	m := map[string]any{"other": "value"}
	if got := extractMapField(m, "userId"); got != "" {
		t.Errorf("extractMapField missing key = %q, want empty", got)
	}
}

func TestExtractMapField_StructPayload_JSONFallback(t *testing.T) {
	type payload struct {
		UserID string `json:"userId"`
	}
	p := payload{UserID: "from-struct"}
	if got := extractMapField(p, "userId"); got != "from-struct" {
		t.Errorf("extractMapField JSON fallback = %q, want %q", got, "from-struct")
	}
}
