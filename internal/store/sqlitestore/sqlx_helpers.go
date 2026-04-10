//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	// Use explicit db struct tags for column mapping. CamelToSnake fallback for fields without db tag.
	pkgSqlxDB.Mapper = reflectx.NewMapperFunc("db", store.CamelToSnake)
}

// SqlxDB returns the package-level *sqlx.DB for use in store methods.
func SqlxDB() *sqlx.DB {
	return pkgSqlxDB
}
