//go:build integration

package integration

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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

		// Run migrations once for the entire test run.
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
		sharedDB = db
	})

	if sharedDBErr != nil {
		t.Skipf("test PG not available: %v", sharedDBErr)
	}
	return sharedDB
}

// seedTenantAgent creates a minimal tenant + agent for FK satisfaction.
// Returns tenantID + agentID. Each test gets unique IDs for isolation.
func seedTenantAgent(t *testing.T, db *sql.DB) (tenantID, agentID uuid.UUID) {
	t.Helper()

	tenantID = uuid.New()
	agentID = uuid.New()
	agentKey := "test-" + agentID.String()[:8]

	// Insert tenant (minimal required fields).
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'active')
		 ON CONFLICT DO NOTHING`,
		tenantID, "test-tenant-"+tenantID.String()[:8], "t"+tenantID.String()[:8])
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Insert agent (minimal required fields including owner_id).
	_, err = db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')
		 ON CONFLICT DO NOTHING`,
		agentID, tenantID, agentKey)
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	// Cleanup after test — delete in FK order (children first, parents last).
	t.Cleanup(func() {
		// Team-related (deepest children first)
		db.Exec("DELETE FROM team_task_comments WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM team_task_events WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM team_task_attachments WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM team_tasks WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM agent_team_members WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM agent_teams WHERE tenant_id = $1", tenantID)

		// Episodic
		db.Exec("DELETE FROM episodic_summaries WHERE tenant_id = $1", tenantID)

		// Cron
		db.Exec("DELETE FROM cron_run_logs WHERE job_id IN (SELECT id FROM cron_jobs WHERE tenant_id = $1)", tenantID)
		db.Exec("DELETE FROM cron_jobs WHERE tenant_id = $1", tenantID)

		// Knowledge stores
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

		// Security stores
		db.Exec("DELETE FROM mcp_user_credentials WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM mcp_access_requests WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM mcp_user_grants WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM mcp_agent_grants WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM mcp_servers WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM secure_cli_user_credentials WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM secure_cli_agent_grants WHERE binary_id IN (SELECT id FROM secure_cli_binaries WHERE tenant_id = $1)", tenantID)
		db.Exec("DELETE FROM secure_cli_binaries WHERE tenant_id = $1", tenantID)
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
	})

	return tenantID, agentID
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

// crossTenantCtx returns a context that bypasses tenant scoping.
func crossTenantCtx() context.Context {
	return store.WithCrossTenant(
		store.WithTenantID(context.Background(), store.MasterTenantID),
	)
}

// testEncryptionKey is a fixed 32-byte key for stores that require AES-256-GCM encryption in tests.
const testEncryptionKey = "0123456789abcdef0123456789abcdef"
