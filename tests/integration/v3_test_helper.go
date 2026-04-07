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

	// Cleanup after test.
	t.Cleanup(func() {
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
