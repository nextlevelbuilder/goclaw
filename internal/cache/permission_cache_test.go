package cache

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestPermissionCache_TenantRole(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	tenantID := uuid.New()
	userID := "user-1"

	// miss before set
	_, ok := pc.GetTenantRole(ctx, tenantID, userID)
	if ok {
		t.Fatal("expected cache miss before SetTenantRole")
	}

	// set then hit
	pc.SetTenantRole(ctx, tenantID, userID, "admin")
	role, ok := pc.GetTenantRole(ctx, tenantID, userID)
	if !ok {
		t.Fatal("expected cache hit after SetTenantRole")
	}
	if role != "admin" {
		t.Fatalf("expected role 'admin', got %q", role)
	}
}

func TestPermissionCache_AgentAccess(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	agentID := uuid.New()
	userID := "user-2"

	// miss
	_, _, ok := pc.GetAgentAccess(ctx, agentID, userID)
	if ok {
		t.Fatal("expected cache miss before SetAgentAccess")
	}

	// set with allowed=true, role=editor
	pc.SetAgentAccess(ctx, agentID, userID, true, "editor")

	allowed, role, ok := pc.GetAgentAccess(ctx, agentID, userID)
	if !ok {
		t.Fatal("expected cache hit after SetAgentAccess")
	}
	if !allowed {
		t.Fatal("expected allowed=true")
	}
	if role != "editor" {
		t.Fatalf("expected role 'editor', got %q", role)
	}
}

func TestPermissionCache_AgentAccess_Denied(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	agentID := uuid.New()
	userID := "user-denied"

	pc.SetAgentAccess(ctx, agentID, userID, false, "")

	allowed, role, ok := pc.GetAgentAccess(ctx, agentID, userID)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if allowed {
		t.Fatal("expected allowed=false")
	}
	if role != "" {
		t.Fatalf("expected empty role, got %q", role)
	}
}

func TestPermissionCache_TeamAccess(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	teamID := uuid.New()
	userID := "user-3"

	// miss
	_, ok := pc.GetTeamAccess(ctx, teamID, userID)
	if ok {
		t.Fatal("expected cache miss before SetTeamAccess")
	}

	// set true
	pc.SetTeamAccess(ctx, teamID, userID, true)

	allowed, ok := pc.GetTeamAccess(ctx, teamID, userID)
	if !ok {
		t.Fatal("expected cache hit after SetTeamAccess")
	}
	if !allowed {
		t.Fatal("expected allowed=true")
	}
}

func TestPermissionCache_TeamAccess_False(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	teamID := uuid.New()
	userID := "user-4"

	pc.SetTeamAccess(ctx, teamID, userID, false)

	allowed, ok := pc.GetTeamAccess(ctx, teamID, userID)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if allowed {
		t.Fatal("expected allowed=false")
	}
}

func TestPermissionCache_Close_Idempotent(t *testing.T) {
	pc := NewPermissionCache()
	pc.Close()
	pc.Close() // must not panic
}

func TestPermissionCache_HandleInvalidation_TenantUsers(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	tenantID := uuid.New()

	// populate tenant roles for two users
	pc.SetTenantRole(ctx, tenantID, "u1", "admin")
	pc.SetTenantRole(ctx, tenantID, "u2", "viewer")

	// invalidate tenant_users → clears all tenant roles
	pc.HandleInvalidation(bus.CacheInvalidatePayload{Kind: bus.CacheKindTenantUsers, Key: "u1"})

	// both should be gone
	if _, ok := pc.GetTenantRole(ctx, tenantID, "u1"); ok {
		t.Error("u1 tenant role should be cleared after tenant_users invalidation")
	}
	if _, ok := pc.GetTenantRole(ctx, tenantID, "u2"); ok {
		t.Error("u2 tenant role should be cleared after tenant_users invalidation")
	}
}

func TestPermissionCache_HandleInvalidation_AgentAccess_WithKey(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	agentID1 := uuid.New()
	agentID2 := uuid.New()

	pc.SetAgentAccess(ctx, agentID1, "u1", true, "admin")
	pc.SetAgentAccess(ctx, agentID1, "u2", true, "viewer")
	pc.SetAgentAccess(ctx, agentID2, "u1", true, "admin")

	// invalidate agent_access for agentID1 only
	pc.HandleInvalidation(bus.CacheInvalidatePayload{Kind: bus.CacheKindAgentAccess, Key: agentID1.String()})

	// agentID1 entries should be gone
	if _, _, ok := pc.GetAgentAccess(ctx, agentID1, "u1"); ok {
		t.Error("agentID1:u1 access should be cleared")
	}
	if _, _, ok := pc.GetAgentAccess(ctx, agentID1, "u2"); ok {
		t.Error("agentID1:u2 access should be cleared")
	}
	// agentID2 should still be cached
	if _, _, ok := pc.GetAgentAccess(ctx, agentID2, "u1"); !ok {
		t.Error("agentID2:u1 access should still be cached")
	}
}

func TestPermissionCache_HandleInvalidation_AgentAccess_ClearAll(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	agentID := uuid.New()

	pc.SetAgentAccess(ctx, agentID, "u1", true, "admin")

	// empty Key → clear all
	pc.HandleInvalidation(bus.CacheInvalidatePayload{Kind: bus.CacheKindAgentAccess, Key: ""})

	if _, _, ok := pc.GetAgentAccess(ctx, agentID, "u1"); ok {
		t.Error("agent access should be cleared when Key is empty")
	}
}

func TestPermissionCache_HandleInvalidation_TeamAccess_WithKey(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	teamID1 := uuid.New()
	teamID2 := uuid.New()

	pc.SetTeamAccess(ctx, teamID1, "u1", true)
	pc.SetTeamAccess(ctx, teamID1, "u2", false)
	pc.SetTeamAccess(ctx, teamID2, "u1", true)

	// invalidate team_access for teamID1 only
	pc.HandleInvalidation(bus.CacheInvalidatePayload{Kind: bus.CacheKindTeamAccess, Key: teamID1.String()})

	if _, ok := pc.GetTeamAccess(ctx, teamID1, "u1"); ok {
		t.Error("teamID1:u1 access should be cleared")
	}
	if _, ok := pc.GetTeamAccess(ctx, teamID1, "u2"); ok {
		t.Error("teamID1:u2 access should be cleared")
	}
	if _, ok := pc.GetTeamAccess(ctx, teamID2, "u1"); !ok {
		t.Error("teamID2:u1 access should still be cached")
	}
}

func TestPermissionCache_HandleInvalidation_TeamAccess_ClearAll(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	teamID := uuid.New()

	pc.SetTeamAccess(ctx, teamID, "u1", true)

	pc.HandleInvalidation(bus.CacheInvalidatePayload{Kind: bus.CacheKindTeamAccess, Key: ""})

	if _, ok := pc.GetTeamAccess(ctx, teamID, "u1"); ok {
		t.Error("team access should be cleared when Key is empty")
	}
}

func TestPermissionCache_HandleInvalidation_UnknownKind(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	// unknown kind should not panic
	pc.HandleInvalidation(bus.CacheInvalidatePayload{Kind: "unknown_kind", Key: "anything"})
}

func TestPermissionCache_MultipleUsers_Isolation(t *testing.T) {
	pc := NewPermissionCache()
	defer pc.Close()

	ctx := context.Background()
	tenantID := uuid.New()

	pc.SetTenantRole(ctx, tenantID, "user-a", "admin")
	pc.SetTenantRole(ctx, tenantID, "user-b", "viewer")

	roleA, okA := pc.GetTenantRole(ctx, tenantID, "user-a")
	roleB, okB := pc.GetTenantRole(ctx, tenantID, "user-b")

	if !okA || roleA != "admin" {
		t.Errorf("user-a: expected admin, got %q ok=%v", roleA, okA)
	}
	if !okB || roleB != "viewer" {
		t.Errorf("user-b: expected viewer, got %q ok=%v", roleB, okB)
	}
}
