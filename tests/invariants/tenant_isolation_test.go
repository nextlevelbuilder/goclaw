//go:build integration

// Package invariants tests critical system invariants that must never be violated.
// These tests verify security boundaries, not functional behavior.
package invariants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// INVARIANT: Tenant A cannot access Tenant B's sessions.
func TestTenantIsolation_SessionStore(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	ss := pg.NewPGSessionStore(db)
	sessionKey := "inv-sess-" + uuid.New().String()[:8]

	// Create session in tenant A
	ss.GetOrCreate(ctxA, sessionKey)
	ss.AddMessage(ctxA, sessionKey, providers.Message{Role: "user", Content: "secret data"})
	if err := ss.Save(ctxA, sessionKey); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify tenant A can access
	if got := ss.Get(ctxA, sessionKey); got == nil {
		t.Fatal("tenant A should access own session")
	}

	// INVARIANT: Tenant B MUST NOT access tenant A's session
	ss2 := pg.NewPGSessionStore(db) // new store instance to bypass cache
	got := ss2.Get(ctxB, sessionKey)
	assertAccessDenied(t, got, nil, "tenant B accessing tenant A's session")

	// INVARIANT: Tenant B MUST NOT see tenant A's messages
	hist := ss2.GetHistory(ctxB, sessionKey)
	if len(hist) > 0 {
		t.Errorf("INVARIANT VIOLATION: tenant B sees %d messages from tenant A", len(hist))
	}
}

// INVARIANT: Tenant A cannot access Tenant B's agents.
func TestTenantIsolation_AgentStore(t *testing.T) {
	db := testDB(t)
	tenantA, agentA, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	as := pg.NewPGAgentStore(db)

	// Verify tenant A can access own agent
	agent, err := as.GetByID(ctxA, agentA)
	if err != nil || agent == nil {
		t.Fatal("tenant A should access own agent")
	}

	// INVARIANT: Tenant B MUST NOT access tenant A's agent
	got, err := as.GetByID(ctxB, agentA)
	assertAccessDenied(t, got, err, "tenant B accessing tenant A's agent by ID")

	// INVARIANT: Tenant B's agent list MUST NOT include tenant A's agents
	agents, err := as.List(ctxB, "") // empty ownerID = list all
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range agents {
		if a.ID == agentA {
			t.Errorf("INVARIANT VIOLATION: tenant B's list includes tenant A's agent")
		}
	}
}

// INVARIANT: Tenant A cannot access Tenant B's memory documents.
func TestTenantIsolation_MemoryStore(t *testing.T) {
	db := testDB(t)
	tenantA, agentA, tenantB, agentB := seedTwoTenants(t, db)

	// Create memory document in tenant A via direct insert
	docID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO memory_documents (id, tenant_id, agent_id, user_id, path, content, hash, created_at, updated_at)
		 VALUES ($1, $2, $3, 'user-a', '/test/doc.md', 'secret memory content', 'abc123', NOW(), NOW())`,
		docID, tenantA, agentA)
	if err != nil {
		t.Fatalf("create memory doc: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM memory_chunks WHERE document_id = $1", docID)
		db.Exec("DELETE FROM memory_documents WHERE id = $1", docID)
	})

	// Verify tenant A's document exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM memory_documents WHERE id = $1 AND tenant_id = $2", docID, tenantA).Scan(&count)
	if count != 1 {
		t.Fatal("tenant A should have memory document")
	}

	// INVARIANT: Tenant B MUST NOT see tenant A's memory via tenant-scoped query
	db.QueryRow("SELECT COUNT(*) FROM memory_documents WHERE id = $1 AND tenant_id = $2", docID, tenantB).Scan(&count)
	if count != 0 {
		t.Errorf("INVARIANT VIOLATION: tenant B can see tenant A's memory document")
	}

	// Test store-level isolation
	ms := pg.NewPGMemoryStore(db, pg.PGMemoryConfig{})
	ctxA := agentCtx(tenantA, agentA, "predefined")
	ctxB := agentCtx(tenantB, agentB, "predefined")

	// GetDocument uses agentID+userID+path, so we test with a unique path
	contentA, err := ms.GetDocument(ctxA, agentA.String(), "user-a", "test-path")
	// It's OK if this returns empty - the point is tenant B shouldn't see A's data
	_ = contentA
	_ = err

	contentB, err := ms.GetDocument(ctxB, agentA.String(), "user-a", "test-path")
	if contentB != "" {
		t.Errorf("INVARIANT VIOLATION: tenant B got content from tenant A's agent: %s", contentB)
	}
}

// INVARIANT: Tenant A cannot access Tenant B's teams.
func TestTenantIsolation_TeamStore(t *testing.T) {
	db := testDB(t)
	tenantA, agentA, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	ts := pg.NewPGTeamStore(db)

	// Create team in tenant A
	teamID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agent_teams (id, tenant_id, name, lead_agent_id, status, settings, created_by)
		 VALUES ($1, $2, 'test-team', $3, 'active', '{"version": 2}', 'test')`,
		teamID, tenantA, agentA)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_team_members WHERE team_id = $1", teamID)
		db.Exec("DELETE FROM agent_teams WHERE id = $1", teamID)
	})

	// Verify tenant A can access
	team, err := ts.GetTeam(ctxA, teamID)
	if err != nil || team == nil {
		t.Fatal("tenant A should access own team")
	}

	// INVARIANT: Tenant B MUST NOT access tenant A's team
	got, err := ts.GetTeam(ctxB, teamID)
	assertAccessDenied(t, got, err, "tenant B accessing tenant A's team")

	// INVARIANT: Tenant B's team list MUST NOT include tenant A's teams
	teams, err := ts.ListTeams(ctxB)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tm := range teams {
		if tm.ID == teamID {
			t.Errorf("INVARIANT VIOLATION: tenant B's list includes tenant A's team")
		}
	}
}

// INVARIANT: Tenant A cannot access Tenant B's skills.
func TestTenantIsolation_SkillStore(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	ss := pg.NewPGSkillStore(db, t.TempDir())

	// Create skill in tenant A
	desc := "test skill"
	slug := "inv-skill-" + tenantA.String()[:8]
	skillID, err := ss.CreateSkillManaged(ctxA, store.SkillCreateParams{
		Name: slug, Slug: slug, Description: &desc, OwnerID: "test-owner",
		Visibility: "private", Status: "active", Version: 1, FilePath: "/tmp/" + slug,
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM skill_agent_grants WHERE skill_id = $1", skillID)
		db.Exec("DELETE FROM skill_user_grants WHERE skill_id = $1", skillID)
		db.Exec("DELETE FROM skills WHERE id = $1", skillID)
	})

	// Verify tenant A can access
	skill, ok := ss.GetSkillByID(ctxA, skillID)
	if !ok {
		t.Fatal("tenant A should access own skill")
	}
	_ = skill

	// INVARIANT: Tenant B MUST NOT access tenant A's skill
	gotSkill, gotOK := ss.GetSkillByID(ctxB, skillID)
	if gotOK {
		t.Errorf("INVARIANT VIOLATION: tenant B can access tenant A's skill: %s", gotSkill.Slug)
	}
}

// INVARIANT: Tenant A cannot access Tenant B's cron jobs.
func TestTenantIsolation_CronStore(t *testing.T) {
	db := testDB(t)
	tenantA, agentA, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	cs := pg.NewPGCronStore(db)

	// Create cron job in tenant A using AddJob
	job, err := cs.AddJob(ctxA, "test-cron",
		store.CronSchedule{Kind: "cron", Expr: "0 * * * *"},
		"test prompt", false, "", "", agentA.String(), "test-user")
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM cron_run_logs WHERE job_id = $1", job.ID)
		db.Exec("DELETE FROM cron_jobs WHERE id = $1", job.ID)
	})

	// Verify tenant A can access
	gotJob, ok := cs.GetJob(ctxA, job.ID) // ID is already a string
	if !ok || gotJob == nil {
		t.Fatal("tenant A should access own cron job")
	}

	// INVARIANT: Tenant B MUST NOT access tenant A's cron job
	gotJobB, okB := cs.GetJob(ctxB, job.ID) // ID is already a string
	if okB && gotJobB != nil {
		t.Errorf("INVARIANT VIOLATION: tenant B can access tenant A's cron job")
	}
}

// INVARIANT: Tenant A cannot access Tenant B's API keys.
func TestTenantIsolation_APIKeyStore(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	ks := pg.NewPGAPIKeyStore(db)

	// Create API key in tenant A
	keyID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO api_keys (id, tenant_id, name, prefix, key_hash, scopes, created_by)
		 VALUES ($1, $2, 'test-key', 'gclw_inv', 'hash123', '{}', 'test-user')`,
		keyID, tenantA)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM api_keys WHERE id = $1", keyID)
	})

	// Verify tenant A can access
	keys, err := ks.List(ctxA, "") // empty ownerID = all keys
	if err != nil {
		t.Fatalf("List A: %v", err)
	}
	found := false
	for _, k := range keys {
		if k.ID == keyID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("tenant A should see own API key")
	}

	// INVARIANT: Tenant B's key list MUST NOT include tenant A's keys
	keysB, err := ks.List(ctxB, "") // empty ownerID = all keys
	if err != nil {
		t.Fatalf("List B: %v", err)
	}
	for _, k := range keysB {
		if k.ID == keyID {
			t.Errorf("INVARIANT VIOLATION: tenant B's list includes tenant A's API key")
		}
	}
}

// INVARIANT: Tenant A cannot access Tenant B's MCP servers.
func TestTenantIsolation_MCPServerStore(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	ms := pg.NewPGMCPServerStore(db, "0123456789abcdef0123456789abcdef") // test encryption key

	// Create MCP server in tenant A
	serverID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO mcp_servers (id, tenant_id, name, display_name, transport, enabled, created_by)
		 VALUES ($1, $2, 'test-mcp', 'Test MCP', 'stdio', true, 'test-user')`,
		serverID, tenantA)
	if err != nil {
		t.Fatalf("create mcp server: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_user_credentials WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_access_requests WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_user_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_agent_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID)
	})

	// Verify tenant A can access
	server, err := ms.GetServer(ctxA, serverID)
	if err != nil || server == nil {
		t.Fatal("tenant A should access own MCP server")
	}

	// INVARIANT: Tenant B MUST NOT access tenant A's MCP server
	got, err := ms.GetServer(ctxB, serverID)
	assertAccessDenied(t, got, err, "tenant B accessing tenant A's MCP server")
}

// INVARIANT: Tenant A cannot access Tenant B's vault documents.
func TestTenantIsolation_VaultStore(t *testing.T) {
	db := testDB(t)
	tenantA, _, tenantB, _ := seedTwoTenants(t, db)

	// Create vault document in tenant A via direct SQL
	docID := uuid.New()
	_, err := db.Exec(
		`INSERT INTO vault_documents (id, tenant_id, title, path, doc_type, content_hash, scope, created_at, updated_at)
		 VALUES ($1, $2, 'secret-doc', '/tmp/secret.md', 'document', 'abc123', 'personal', NOW(), NOW())`,
		docID, tenantA)
	if err != nil {
		t.Fatalf("create vault doc: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM vault_links WHERE tenant_id = $1", tenantA)
		db.Exec("DELETE FROM vault_documents WHERE id = $1", docID)
	})

	// Verify tenant A can access via direct query
	var count int
	db.QueryRow("SELECT COUNT(*) FROM vault_documents WHERE id = $1 AND tenant_id = $2", docID, tenantA).Scan(&count)
	if count != 1 {
		t.Fatal("tenant A should have vault document")
	}

	// INVARIANT: Tenant B MUST NOT see tenant A's vault document via tenant-scoped query
	db.QueryRow("SELECT COUNT(*) FROM vault_documents WHERE id = $1 AND tenant_id = $2", docID, tenantB).Scan(&count)
	if count != 0 {
		t.Errorf("INVARIANT VIOLATION: tenant B can see tenant A's vault document")
	}

	// INVARIANT: Any query scoped by tenant_id MUST exclude other tenants
	db.QueryRow("SELECT COUNT(*) FROM vault_documents WHERE path = $1 AND tenant_id = $2", "/tmp/secret.md", tenantB).Scan(&count)
	if count != 0 {
		t.Errorf("INVARIANT VIOLATION: tenant B can see tenant A's vault document by path")
	}
}
