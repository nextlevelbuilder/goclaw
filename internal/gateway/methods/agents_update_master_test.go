package methods

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestAgentsUpdate_MasterCanUpdatePredefined_OtherUserAgent(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantA, "alice"),
		agentTenant: tenantA,
	}
	m := newGuardMethods(t, stub, "system")

	getLog := captureSlog(t)
	client := guardClient(permissions.RoleOwner, tenantA, "system")
	req := buildRequest(t, "update-master", protocol.MethodAgentsUpdate, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "Renamed Agent",
	})

	m.handleUpdate(guardCtx(client), client, req)

	if stub.updateRecorded() != 1 {
		t.Fatalf("Update called %d times, want 1", stub.updateRecorded())
	}
	if got := stub.updateCalls[0]["display_name"]; got != "Renamed Agent" {
		t.Fatalf("display_name = %v, want Renamed Agent", got)
	}
	logged := getLog()
	if !strings.Contains(logged, "security.master_override") {
		t.Fatalf("missing security.master_override slog line. got: %q", logged)
	}
	if !strings.Contains(logged, protocol.MethodAgentsUpdate) {
		t.Fatalf("expected method=%q in slog. got: %q", protocol.MethodAgentsUpdate, logged)
	}
}

func TestAgentsUpdate_OwnerCanUpdateOwnPredefined(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantA, "alice"),
		agentTenant: tenantA,
	}
	m := newGuardMethods(t, stub)

	getLog := captureSlog(t)
	client := guardClient(permissions.RoleAdmin, tenantA, "alice")
	req := buildRequest(t, "update-owner", protocol.MethodAgentsUpdate, map[string]any{
		"agentId": stub.agent.AgentKey,
		"model":   "claude-opus-4-1",
	})

	m.handleUpdate(guardCtx(client), client, req)

	if stub.updateRecorded() != 1 {
		t.Fatalf("Update called %d times, want 1", stub.updateRecorded())
	}
	if logged := getLog(); strings.Contains(logged, "security.master_override") {
		t.Fatalf("owner branch must not emit master_override slog. got: %q", logged)
	}
}

func TestAgentsUpdate_NonOwnerNonMaster_Rejected(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantA, "alice"),
		agentTenant: tenantA,
	}
	m := newGuardMethods(t, stub)

	client := guardClient(permissions.RoleAdmin, tenantA, "bob")
	req := buildRequest(t, "update-bob", protocol.MethodAgentsUpdate, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "Hijacked",
	})

	m.handleUpdate(guardCtx(client), client, req)

	if stub.updateRecorded() != 0 {
		t.Fatalf("Update called %d times, want 0 (rejected request must not mutate)", stub.updateRecorded())
	}
}

func TestAgentsUpdate_CrossTenantMaster_NotFound(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantB, "alice"),
		agentTenant: tenantB,
	}
	m := newGuardMethods(t, stub)

	client := guardClient(permissions.RoleOwner, tenantA, "system")
	req := buildRequest(t, "update-cross-tenant", protocol.MethodAgentsUpdate, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "Cross Tenant Attempt",
	})

	m.handleUpdate(guardCtx(client), client, req)

	if stub.updateRecorded() != 0 {
		t.Fatalf("Update called %d times, want 0 (cross-tenant lookup must not mutate)", stub.updateRecorded())
	}
}
