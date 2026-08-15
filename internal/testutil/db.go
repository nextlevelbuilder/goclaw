//go:build integration

// Package testutil provides reusable test helpers for integration tests:
// - TestDB: connects to Postgres via TEST_DATABASE_URL and runs migrations once.
// - Context builders: TenantCtx, UserCtx, AgentCtx, FullCtx.
// - Mock stores: gomock-generated interfaces for unit tests.
//
// Integration helpers live behind `//go:build integration` so default builds stay
// dependency-free. Call TestDB from integration tests; it skips gracefully if
// Postgres is unreachable, matching the pattern in tests/integration/v3_test_helper.go.
package testutil

import (
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// defaultTestDSN matches the pgvector test container in the README.
const defaultTestDSN = "postgres://postgres:test@localhost:5433/goclaw_test?sslmode=disable"

var (
	sharedDB     *sql.DB
	sharedDBOnce sync.Once
	sharedDBErr  error
)

// TestDB returns a shared *sql.DB for the test binary, running migrations once.
// If Postgres is unreachable it skips the calling test with a clear reason.
// The migrationsDir argument is the filesystem path to the migrations folder
// relative to the test binary's working directory (e.g. "../../migrations").
func TestDB(t *testing.T, migrationsDir string) *sql.DB {
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
		m, err := migrate.New("file://"+migrationsDir, dsn)
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
