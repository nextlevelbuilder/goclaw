//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteMCPListAgentGrantsByAgentIDsIsBatchAndTenantScoped(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	agentA, agentB := uuid.New(), uuid.New()
	serverA, serverB := uuid.New(), uuid.New()
	for i, tenantID := range []uuid.UUID{tenantA, tenantB} {
		if _, err := db.Exec(`INSERT INTO tenants (id,name,slug,status,settings) VALUES (?,?,?,'active','{}')`, tenantID, "Tenant", "tenant-"+tenantID.String()); err != nil {
			t.Fatalf("seed tenant %d: %v", i, err)
		}
	}
	seedAgent := func(id, tenantID uuid.UUID, key string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO agents (id,agent_key,owner_id,provider,model,tenant_id) VALUES (?,?,?,'openai','test',?)`, id, key, "owner", tenantID); err != nil {
			t.Fatalf("seed agent %s: %v", key, err)
		}
	}
	seedServer := func(id, tenantID uuid.UUID, name string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO mcp_servers (id,name,transport,created_by,tenant_id) VALUES (?,?,'stdio','owner',?)`, id, name, tenantID); err != nil {
			t.Fatalf("seed server %s: %v", name, err)
		}
	}
	seedAgent(agentA, tenantA, "agent-a")
	seedAgent(agentB, tenantB, "agent-b")
	seedServer(serverA, tenantA, "server-a")
	seedServer(serverB, tenantB, "server-b")
	for _, row := range []struct {
		id, serverID, agentID, tenantID uuid.UUID
	}{{uuid.New(), serverA, agentA, tenantA}, {uuid.New(), serverB, agentB, tenantB}} {
		if _, err := db.Exec(`INSERT INTO mcp_agent_grants (id,server_id,agent_id,enabled,granted_by,tenant_id) VALUES (?,?,?,1,'owner',?)`, row.id, row.serverID, row.agentID, row.tenantID); err != nil {
			t.Fatalf("seed grant: %v", err)
		}
	}

	mcpStore := NewSQLiteMCPServerStore(db, "")
	grants, err := mcpStore.ListAgentGrantsByAgentIDs(store.WithTenantID(context.Background(), tenantA), []uuid.UUID{agentA, agentB})
	if err != nil {
		t.Fatalf("ListAgentGrantsByAgentIDs: %v", err)
	}
	if len(grants) != 1 || grants[0].AgentID != agentA || grants[0].ServerID != serverA {
		t.Fatalf("tenant A grants = %+v", grants)
	}
}
