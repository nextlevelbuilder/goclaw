//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestBridgeTool_Execute_RevokeAgentGrant_ReturnsError: TDD-red for Phase 02.
// Skipped until BridgeTool.Execute rechecks grants at call time.
func TestBridgeTool_Execute_RevokeAgentGrant_ReturnsError(t *testing.T) {
	t.Skip("Phase 02: BridgeTool.Execute grant-recheck not yet implemented")

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)

	grantAgentAccess(t, db, tenantID, serverID, agentID)

	mcpStore := pg.NewPGMCPServerStore(db, testEncryptionKey)
	ctx := store.WithTenantID(context.Background(), tenantID)
	ctx = store.WithAgentID(ctx, agentID)
	ctx = store.WithUserID(ctx, "test-user")

	clientPtr := &atomic.Pointer[mcpclient.Client]{}
	connected := &atomic.Bool{}
	connected.Store(true)
	grantChecker := mcp.NewStoreGrantChecker(mcpStore, nil)

	tool := mcp.NewBridgeTool(
		"test-server",
		mcpgo.Tool{Name: "test_tool", Description: "test"},
		clientPtr,
		"mcp_test",
		60,
		connected,
		serverID,
		grantChecker,
	)

	if err := mcpStore.RevokeFromAgent(ctx, serverID, agentID); err != nil {
		t.Fatalf("RevokeFromAgent: %v", err)
	}

	result := tool.Execute(ctx, map[string]any{"arg": "value"})
	if !result.IsError {
		t.Error("expected error result after grant revoked, but got success")
	}
	if result.IsError && !containsGrantRevoked(result.ForLLM) {
		t.Errorf("expected 'grant revoked' error, got: %s", result.ForLLM)
	}
}

// TestBridgeTool_Execute_RevokeUserGrant_ReturnsError: TDD-red for Phase 02.
func TestBridgeTool_Execute_RevokeUserGrant_ReturnsError(t *testing.T) {
	t.Skip("Phase 02: user-grant-level revocation not yet implemented — see commit 8b8da3a3")

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)
	userID := "test-user-" + uuid.New().String()[:8]

	grantAgentAccess(t, db, tenantID, serverID, agentID)
	grantUserAccess(t, db, tenantID, serverID, userID)

	mcpStore := pg.NewPGMCPServerStore(db, testEncryptionKey)
	ctx := store.WithTenantID(context.Background(), tenantID)
	ctx = store.WithAgentID(ctx, agentID)
	ctx = store.WithUserID(ctx, userID)

	clientPtr := &atomic.Pointer[mcpclient.Client]{}
	connected := &atomic.Bool{}
	connected.Store(true)
	grantChecker := mcp.NewStoreGrantChecker(mcpStore, nil)

	tool := mcp.NewBridgeTool(
		"test-server",
		mcpgo.Tool{Name: "test_tool", Description: "test"},
		clientPtr,
		"mcp_test",
		60,
		connected,
		serverID,
		grantChecker,
	)

	if err := mcpStore.RevokeFromUser(ctx, serverID, userID); err != nil {
		t.Fatalf("RevokeFromUser: %v", err)
	}

	result := tool.Execute(ctx, map[string]any{"arg": "value"})

	if !result.IsError {
		t.Error("expected error result after user grant revoked")
	}
}

// TestResolver_Rebuild_AfterRevoke_NoToolInPrompt: regression guard — after revoking
// a grant, ListAccessible returns 0 servers so prompt rebuild has no tool.
func TestResolver_Rebuild_AfterRevoke_NoToolInPrompt(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	serverID := seedMCPServer(t, db, tenantID)

	grantAgentAccess(t, db, tenantID, serverID, agentID)

	mcpStore := pg.NewPGMCPServerStore(db, testEncryptionKey)
	ctx := store.WithTenantID(context.Background(), tenantID)

	accessible, err := mcpStore.ListAccessible(ctx, agentID, "test-user")
	if err != nil {
		t.Fatalf("ListAccessible before revoke: %v", err)
	}
	if len(accessible) == 0 {
		t.Fatal("expected accessible server before revoke")
	}
	serverName := accessible[0].Server.Name

	if err := mcpStore.RevokeFromAgent(ctx, serverID, agentID); err != nil {
		t.Fatalf("RevokeFromAgent: %v", err)
	}

	accessible, err = mcpStore.ListAccessible(ctx, agentID, "test-user")
	if err != nil {
		t.Fatalf("ListAccessible after revoke: %v", err)
	}
	if len(accessible) != 0 {
		t.Errorf("expected 0 accessible servers after revoke, got %d", len(accessible))
	}

	t.Logf("Regression guard PASS: server %q no longer accessible after revoke", serverName)
}

// --- Helpers ---

func grantAgentAccess(t *testing.T, db *sql.DB, tenantID, serverID, agentID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO mcp_agent_grants (id, server_id, agent_id, enabled, granted_by, created_at, tenant_id)
		 VALUES ($1, $2, $3, true, 'test-admin', NOW(), $4)
		 ON CONFLICT (server_id, agent_id) DO UPDATE SET enabled = true`,
		uuid.New(), serverID, agentID, tenantID)
	if err != nil {
		t.Fatalf("grantAgentAccess: %v", err)
	}
}

func grantUserAccess(t *testing.T, db *sql.DB, tenantID, serverID uuid.UUID, userID string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO mcp_user_grants (id, server_id, user_id, enabled, granted_by, created_at, tenant_id)
		 VALUES ($1, $2, $3, true, 'test-admin', NOW(), $4)
		 ON CONFLICT (server_id, user_id) DO UPDATE SET enabled = true`,
		uuid.New(), serverID, userID, tenantID)
	if err != nil {
		t.Fatalf("grantUserAccess: %v", err)
	}
}

func containsGrantRevoked(s string) bool {
	return len(s) > 0 && (strings.Contains(s, "grant revoked") || strings.Contains(s, "grant denied"))
}

