//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
)

// pkgSqlxDB is the package-level *sqlx.DB wrapping the same *sql.DB connection pool.
// Initialized once in initSqlx() called from NewSQLiteStores.
// Phase 2+ will use this for Get/Select/StructScan migrations.
//
// Note on sqliteTime: sqlx StructScan uses sql.Scanner interface, so fields typed
// as sqliteTime (which implements sql.Scanner) work directly with StructScan.
// No additional adapter is needed — sqliteTime already handles SQLite's text timestamps.
var pkgSqlxDB *sqlx.DB

// initSqlx wraps an existing *sql.DB with sqlx and configures the json tag mapper.
// The returned *sqlx.DB shares the same connection pool — no new connections are created.
func initSqlx(db *sql.DB) {
	pkgSqlxDB = sqlx.NewDb(db, "sqlite")
	// Use json struct tags for column mapping with camelCase→snake_case conversion.
	pkgSqlxDB.Mapper = reflectx.NewMapperFunc("json", camelToSnake)
}

// camelToSnake converts a camelCase string to snake_case.
// Already-snake_case strings pass through unchanged.
func camelToSnake(s string) string {
	var result []byte
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z' {
				result = append(result, '_')
			}
			result = append(result, byte(r+32))
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
