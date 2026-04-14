//go:build integration

package invariants

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const defaultTestDSN = "postgres://postgres:test@localhost:5433/goclaw_test?sslmode=disable"

var (
	sharedDB     *sql.DB
	sharedDBOnce sync.Once
	sharedDBErr  error
)

// testDB connects to the test PG instance, runs migrations once, and returns
// a shared *sql.DB. Skips test if PG is unreachable.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	sharedDBOnce.Do(func() {
		dsn := os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			dsn = defaultTestDSN
		}

		db, err := sql.Open("pgx", dsn)
		if err != nil {
			sharedDBErr = err
			return
		}
		if err := db.Ping(); err != nil {
			sharedDBErr = err
			return
		}

		m, err := migrate.New("file://../../migrations", dsn)
		if err != nil {
			sharedDBErr = err
			return
		}
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			sharedDBErr = err
			return
		}
		m.Close()

		pg.InitSqlx(db)
		sharedDB = db
	})

	if sharedDBErr != nil {
		t.Skipf("test PG not available: %v", sharedDBErr)
	}
	return sharedDB
}

// seedTenantAgent creates a minimal tenant + agent for FK satisfaction.
// Agent insert includes all columns expected by agentSelectCols (37 columns).
func seedTenantAgent(t *testing.T, db *sql.DB) (tenantID, agentID uuid.UUID) {
	t.Helper()

	tenantID = uuid.New()
	agentID = uuid.New()
	agentKey := "inv-" + agentID.String()[:8]

	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'active')
		 ON CONFLICT DO NOTHING`,
		tenantID, "inv-tenant-"+tenantID.String()[:8], "i"+tenantID.String()[:8])
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Insert with all 37 columns expected by agentSelectCols
	_, err = db.Exec(
		`INSERT INTO agents (
			id, agent_key, display_name, frontmatter, owner_id, provider, model,
			context_window, max_tool_iterations, workspace, restrict_to_workspace,
			tools_config, sandbox_config, subagents_config, memory_config,
			compaction_config, context_pruning, other_config,
			emoji, agent_description, thinking_level, max_tokens,
			self_evolve, skill_evolve, skill_nudge_interval,
			reasoning_config, workspace_sharing, chatgpt_oauth_routing,
			shell_deny_groups, kg_dedup_config,
			agent_type, is_default, status, budget_monthly_cents, created_at, updated_at, tenant_id
		) VALUES (
			$1, $2, $3, NULL, $4, $5, $6,
			8192, 10, '', false,
			'{}', NULL, NULL, NULL,
			NULL, NULL, '{}',
			'', '', '', 4096,
			false, false, 0,
			'{}', '{}', '{}',
			'{}', '{}',
			'predefined', false, 'active', 0, NOW(), NOW(), $7
		) ON CONFLICT DO NOTHING`,
		agentID, agentKey, "Test Agent "+agentKey, "test-owner", "test", "test-model", tenantID)
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	t.Cleanup(func() {
		cleanupTenant(db, tenantID, agentID)
	})

	return tenantID, agentID
}

// seedTwoTenants creates 2 independent tenants with agents for isolation testing.
func seedTwoTenants(t *testing.T, db *sql.DB) (tenantA, agentA, tenantB, agentB uuid.UUID) {
	t.Helper()
	tenantA, agentA = seedTenantAgent(t, db)
	tenantB, agentB = seedTenantAgent(t, db)
	return
}

// tenantCtx returns a context with tenant ID set for store scoping.
func tenantCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

// userCtx returns a context with both tenant ID and user ID set.
func userCtx(tenantID uuid.UUID, userID string) context.Context {
	ctx := store.WithTenantID(context.Background(), tenantID)
	return store.WithUserID(ctx, userID)
}

// agentCtx returns a context with tenant, agent type and agent ID set.
func agentCtx(tenantID, agentID uuid.UUID, agentType string) context.Context {
	ctx := store.WithTenantID(context.Background(), tenantID)
	ctx = store.WithAgentID(ctx, agentID)
	ctx = store.WithAgentType(ctx, agentType)
	return ctx
}

// assertAccessDenied verifies that a cross-tenant access returns nil or error.
// INVARIANT: Cross-tenant access MUST NOT return data.
func assertAccessDenied(t *testing.T, result any, err error, msg string) {
	t.Helper()
	// Access denied can manifest as:
	// 1. Non-nil error (explicit denial)
	// 2. Nil result with nil error (no data found - implicit denial)
	// Use reflection to check for nil because interface{} with typed nil pointer is not nil.
	isNil := result == nil || (reflect.ValueOf(result).Kind() == reflect.Ptr && reflect.ValueOf(result).IsNil())
	if err == nil && !isNil {
		t.Errorf("INVARIANT VIOLATION: %s - expected nil or error, got data", msg)
	}
}

// assertNotEmpty verifies that the result is not nil/empty (valid access).
func assertNotEmpty(t *testing.T, result any, msg string) {
	t.Helper()
	if result == nil {
		t.Errorf("%s: expected non-nil result", msg)
	}
}

// cleanupTenant removes all tenant data in FK order.
func cleanupTenant(db *sql.DB, tenantID, agentID uuid.UUID) {
	// Team-related
	db.Exec("DELETE FROM team_task_comments WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM team_task_events WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM team_task_attachments WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM team_tasks WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM agent_team_members WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM agent_teams WHERE tenant_id = $1", tenantID)

	// Knowledge stores
	db.Exec("DELETE FROM episodic_summaries WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM vault_links WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM vault_documents WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM kg_dedup_candidates WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM kg_relations WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM kg_entities WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM memory_chunks WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM memory_documents WHERE tenant_id = $1", tenantID)

	// Sessions
	db.Exec("DELETE FROM sessions WHERE tenant_id = $1", tenantID)

	// Skills
	db.Exec("DELETE FROM skill_agent_grants WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM skill_user_grants WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM skills WHERE tenant_id = $1", tenantID)

	// Cron
	db.Exec("DELETE FROM cron_run_logs WHERE job_id IN (SELECT id FROM cron_jobs WHERE tenant_id = $1)", tenantID)
	db.Exec("DELETE FROM cron_jobs WHERE tenant_id = $1", tenantID)

	// Security stores
	db.Exec("DELETE FROM mcp_user_credentials WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM mcp_access_requests WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM mcp_user_grants WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM mcp_agent_grants WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM mcp_servers WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM api_keys WHERE tenant_id = $1", tenantID)
	db.Exec("DELETE FROM agent_config_permissions WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM channel_contacts WHERE tenant_id = $1", tenantID)

	// Agent-related
	db.Exec("DELETE FROM agent_shares WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_context_files WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM user_context_files WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM user_agent_overrides WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_user_profiles WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_evolution_suggestions WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agent_evolution_metrics WHERE agent_id = $1", agentID)
	db.Exec("DELETE FROM agents WHERE id = $1", agentID)
	db.Exec("DELETE FROM tenants WHERE id = $1", tenantID)
}
