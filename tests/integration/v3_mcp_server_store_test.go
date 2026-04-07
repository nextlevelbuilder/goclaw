//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreMCP_CreateAndGet(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGMCPServerStore(db, testEncryptionKey)

	srv := &store.MCPServerData{
		Name:        "test-mcp-" + uuid.New().String()[:8],
		DisplayName: "Test MCP Server",
		Transport:   "stdio",
		Command:     "test-cmd",
		URL:         "http://localhost:8080",
		APIKey:      "test-api-key",
		ToolPrefix:  "test_",
		Enabled:     true,
		CreatedBy:   "test-user",
	}
	if err := s.CreateServer(ctx, srv); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if srv.ID == uuid.Nil {
		t.Fatal("expected srv.ID to be set after create")
	}

	// GetServer by ID.
	got, err := s.GetServer(ctx, srv.ID)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.Name != srv.Name {
		t.Errorf("Name mismatch: got %q, want %q", got.Name, srv.Name)
	}
	if !got.Enabled {
		t.Error("expected Enabled=true")
	}

	// GetServerByName.
	byName, err := s.GetServerByName(ctx, srv.Name)
	if err != nil {
		t.Fatalf("GetServerByName: %v", err)
	}
	if byName.ID != srv.ID {
		t.Errorf("GetServerByName ID mismatch: got %v, want %v", byName.ID, srv.ID)
	}
}

func TestStoreMCP_GrantToAgent(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGMCPServerStore(db, testEncryptionKey)

	serverID := seedMCPServer(t, db, tenantID)

	grant := &store.MCPAgentGrant{
		ServerID:  serverID,
		AgentID:   agentID,
		Enabled:   true,
		GrantedBy: "test-user",
	}
	if err := s.GrantToAgent(ctx, grant); err != nil {
		t.Fatalf("GrantToAgent: %v", err)
	}

	// ListServerGrants — expect 1 (uses COALESCE so nullable JSONB is safe).
	grants, err := s.ListServerGrants(ctx, serverID)
	if err != nil {
		t.Fatalf("ListServerGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].AgentID != agentID {
		t.Errorf("AgentID mismatch: got %v, want %v", grants[0].AgentID, agentID)
	}

	// RevokeFromAgent — should reduce to 0.
	if err := s.RevokeFromAgent(ctx, serverID, agentID); err != nil {
		t.Fatalf("RevokeFromAgent: %v", err)
	}
	grants2, err := s.ListServerGrants(ctx, serverID)
	if err != nil {
		t.Fatalf("ListServerGrants after revoke: %v", err)
	}
	if len(grants2) != 0 {
		t.Errorf("expected 0 grants after revoke, got %d", len(grants2))
	}
}

func TestStoreMCP_ListAccessible(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGMCPServerStore(db, testEncryptionKey)

	serverID := seedMCPServer(t, db, tenantID)

	grant := &store.MCPAgentGrant{
		ServerID:  serverID,
		AgentID:   agentID,
		Enabled:   true,
		GrantedBy: "test-user",
	}
	if err := s.GrantToAgent(ctx, grant); err != nil {
		t.Fatalf("GrantToAgent: %v", err)
	}

	accessible, err := s.ListAccessible(ctx, agentID, "test-user")
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	found := false
	for _, a := range accessible {
		if a.Server.ID == serverID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected serverID %v in ListAccessible result (got %d entries)", serverID, len(accessible))
	}
}

func TestStoreMCP_TenantIsolation(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantA, tenantB, _, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	s := pg.NewPGMCPServerStore(db, testEncryptionKey)

	// Create server in tenant A.
	srv := &store.MCPServerData{
		Name:        "iso-mcp-" + uuid.New().String()[:8],
		DisplayName: "Isolation Test Server",
		Transport:   "stdio",
		Enabled:     true,
		CreatedBy:   "test-user",
	}
	if err := s.CreateServer(ctxA, srv); err != nil {
		t.Fatalf("CreateServer tenantA: %v", err)
	}

	// Tenant B cannot see tenant A's server.
	got, err := s.GetServer(ctxB, srv.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for tenant B, got err=%v doc=%v", err, got)
	}
}
