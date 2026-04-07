package pg

import (
	"database/sql"
	"database/sql/driver"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
	"github.com/lib/pq"
)

// pkgSqlxDB is the package-level *sqlx.DB wrapping the same *sql.DB connection pool.
// Initialized once in initSqlx() called from NewPGStores.
// Phase 2+ will use this for Get/Select/StructScan migrations.
var pkgSqlxDB *sqlx.DB

// initSqlx wraps an existing *sql.DB with sqlx and configures the json tag mapper.
// The returned *sqlx.DB shares the same connection pool — no new connections are created.
func initSqlx(db *sql.DB) {
	pkgSqlxDB = sqlx.NewDb(db, "pgx")
	// Use json struct tags for column mapping with camelCase→snake_case conversion.
	// Handles both camelCase tags (agentId → agent_id) and already snake_case (parent_trace_id → unchanged).
	pkgSqlxDB.Mapper = reflectx.NewMapperFunc("json", camelToSnake)
}

// camelToSnake converts a camelCase string to snake_case.
// Already-snake_case strings pass through unchanged.
// Examples: "agentId" → "agent_id", "parent_trace_id" → "parent_trace_id", "ID" → "id"
func camelToSnake(s string) string {
	var result []byte
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z' {
				result = append(result, '_')
			}
			result = append(result, byte(r+32)) // toLower
		} else {
			result = append(result, byte(r))
		}
	}
	return string(result)
}

// SqlxDB returns the package-level *sqlx.DB for use in store methods.
func SqlxDB() *sqlx.DB {
	return pkgSqlxDB
}

// --- UUIDArray: pq.Array-compatible scanner for []uuid.UUID ---

// UUIDArray wraps []uuid.UUID for pq.Array compatibility with sqlx StructScan.
// Use as a field type in scan structs where the column is a PostgreSQL uuid[].
type UUIDArray []uuid.UUID

// Scan implements sql.Scanner by delegating to pq.Array.
func (a *UUIDArray) Scan(src any) error {
	return pq.Array((*[]uuid.UUID)(a)).Scan(src)
}

// Value implements driver.Valuer by delegating to pq.Array.
func (a UUIDArray) Value() (driver.Value, error) {
	return pq.Array([]uuid.UUID(a)).Value()
}
