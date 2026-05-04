package methods

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestFilesSet_MasterCanEditPredefined_OtherUserAgent(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantA, "alice"),
		agentTenant: tenantA,
	}
	m := newGuardMethods(t, stub, "system")

	getLog := captureSlog(t)
	client := guardClient(permissions.RoleOwner, tenantA, "system")
	req := buildRequest(t, "files-set-master", protocol.MethodAgentsFileSet, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "SOUL.md",
		"content": "# soul (master edit)",
	})

	m.handleFilesSet(guardCtx(client), client, req)

	if stub.setFileRecorded() != 1 {
		t.Fatalf("SetAgentContextFile called %d times, want 1", stub.setFileRecorded())
	}
	logged := getLog()
	if !strings.Contains(logged, "security.master_override") {
		t.Fatalf("missing security.master_override slog line. got: %q", logged)
	}
	if !strings.Contains(logged, "SOUL.md") {
		t.Fatalf("expected file name SOUL.md in slog. got: %q", logged)
	}
	if !strings.Contains(logged, stub.agent.ID.String()) {
		t.Fatalf("expected agent_id in slog. got: %q", logged)
	}
}

func TestFilesSet_OwnerCanEditOwnPredefined(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantA, "alice"),
		agentTenant: tenantA,
	}
	m := newGuardMethods(t, stub)

	getLog := captureSlog(t)
	client := guardClient(permissions.RoleAdmin, tenantA, "alice")
	req := buildRequest(t, "files-set-owner", protocol.MethodAgentsFileSet, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "SOUL.md",
		"content": "# soul (owner edit)",
	})

	m.handleFilesSet(guardCtx(client), client, req)

	if stub.setFileRecorded() != 1 {
		t.Fatalf("SetAgentContextFile called %d times, want 1", stub.setFileRecorded())
	}
	if logged := getLog(); strings.Contains(logged, "security.master_override") {
		t.Fatalf("owner branch must not emit master_override slog. got: %q", logged)
	}
}

func TestFilesSet_NonOwnerNonMaster_Rejected(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantA, "alice"),
		agentTenant: tenantA,
	}
	m := newGuardMethods(t, stub)

	client := guardClient(permissions.RoleAdmin, tenantA, "bob")
	req := buildRequest(t, "files-set-bob", protocol.MethodAgentsFileSet, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "SOUL.md",
		"content": "# soul (unauthorized)",
	})

	m.handleFilesSet(guardCtx(client), client, req)

	if stub.setFileRecorded() != 0 {
		t.Fatalf("SetAgentContextFile called %d times, want 0 (rejected write must not mutate)", stub.setFileRecorded())
	}
}

// Two-tenant fixture: master in tenant A must not reach an agent in tenant B.
// GetByKey returns not-found before the master guard runs.
func TestFilesSet_CrossTenantMaster_NotFound(t *testing.T) {
	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	stub := &guardStubStore{
		agent:       newPredefinedAgent(tenantB, "alice"),
		agentTenant: tenantB,
	}
	m := newGuardMethods(t, stub)

	client := guardClient(permissions.RoleOwner, tenantA, "system")
	req := buildRequest(t, "files-set-cross-tenant", protocol.MethodAgentsFileSet, map[string]any{
		"agentId": stub.agent.AgentKey,
		"name":    "SOUL.md",
		"content": "# soul (cross-tenant attempt)",
	})

	m.handleFilesSet(guardCtx(client), client, req)

	if stub.setFileRecorded() != 0 {
		t.Fatalf("SetAgentContextFile called %d times, want 0 (cross-tenant lookup must not mutate)", stub.setFileRecorded())
	}
}
