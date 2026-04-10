package pg

import (
	"database/sql"
	"database/sql/driver"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// pkgSqlxDB is the package-level *sqlx.DB wrapping the same *sql.DB connection pool.
// Initialized once in initSqlx() called from NewPGStores.
// Phase 2+ will use this for Get/Select/StructScan migrations.
var pkgSqlxDB *sqlx.DB

// InitSqlx is the exported variant of initSqlx, intended for use in integration tests
// that construct stores directly (e.g. pg.NewPGTeamStore) rather than via NewPGStores.
func InitSqlx(db *sql.DB) { initSqlx(db) }

// initSqlx wraps an existing *sql.DB with sqlx and configures the json tag mapper.
// The returned *sqlx.DB shares the same connection pool — no new connections are created.
func initSqlx(db *sql.DB) {
	pkgSqlxDB = sqlx.NewDb(db, "pgx")
	// Use explicit db struct tags for column mapping. CamelToSnake fallback for fields without db tag.
	pkgSqlxDB.Mapper = reflectx.NewMapperFunc("db", store.CamelToSnake)
}

// SqlxDB returns the package-level *sqlx.DB for use in store methods.
func SqlxDB() *sqlx.DB {
	return pkgSqlxDB
}

// sqlxTx wraps an existing *sql.Tx with sqlx, sharing the same mapper as pkgSqlxDB.
// This allows using SelectContext/GetContext on transactions that were started with *sql.DB.
func sqlxTx(tx *sql.Tx) *sqlx.Tx {
	return &sqlx.Tx{Tx: tx, Mapper: pkgSqlxDB.Mapper}
}

// --- UUIDArray: pq.Array-compatible scanner for []uuid.UUID ---

// UUIDArray wraps []uuid.UUID for pq.Array compatibility with sqlx StructScan.
// Use as a field type in scan structs where the column is a PostgreSQL uuid[].
// Planned for teams_tasks.go blocked_by column migration.
type UUIDArray []uuid.UUID

// Scan implements sql.Scanner by delegating to pq.Array.
func (a *UUIDArray) Scan(src any) error {
	return pq.Array((*[]uuid.UUID)(a)).Scan(src)
}

// Value implements driver.Valuer by delegating to pq.Array.
func (a UUIDArray) Value() (driver.Value, error) {
	return pq.Array([]uuid.UUID(a)).Value()
}
