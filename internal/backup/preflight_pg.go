//go:build !sqliteonly

package backup

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// detectPGServerMajor queries the live PostgreSQL server for its major
// version number. Returns 0 on any error (no DSN, unreachable, parse
// failure) or for exotic values below the PG 8.x floor, so callers can
// fall back to a generic hint. Uses server_version_num which encodes
// major*10000 + minor for PG 10+ (e.g. 180003 = 18.3, 170009 = 17.9).
func detectPGServerMajor(ctx context.Context, dsn string) int {
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0
	}
	defer db.Close()

	var serverNum int
	if err := db.QueryRowContext(ctx, "SHOW server_version_num").Scan(&serverNum); err != nil {
		return 0
	}
	major := serverNum / 10000
	// Clamp implausible values (negative, zero, or pre-8 output from an
	// exotic pooler) so we never render a hint like "postgresql-1-client".
	if major < 8 {
		return 0
	}
	return major
}

// checkPgDumpServerCompat verifies that the installed pg_dump can dump a
// PostgreSQL server of the given major version. pg_dump's major version must
// be >= the server's; otherwise pg_dump aborts at runtime with a version
// mismatch error. Returns (check, compatible). When the check cannot read
// pg_dump's version, the result is a non-fatal warning and compatible=true.
func checkPgDumpServerCompat(ctx context.Context, serverMajor int) (PreflightCheck, bool) {
	name := "pg_version_compat"

	pgDumpVer, err := PgDumpVersion(ctx)
	if err != nil {
		return PreflightCheck{
			Name:   name,
			Status: "warning",
			Detail: fmt.Sprintf("could not read pg_dump version: %v", err),
		}, true
	}
	clientMajor := ParsePgDumpMajor(pgDumpVer)
	if clientMajor == 0 {
		return PreflightCheck{
			Name:   name,
			Status: "warning",
			Detail: fmt.Sprintf("could not parse pg_dump version: %q", pgDumpVer),
		}, true
	}

	if clientMajor < serverMajor {
		return PreflightCheck{
			Name:   name,
			Status: "missing",
			Detail: fmt.Sprintf("pg_dump %d cannot dump PostgreSQL %d server (major version mismatch)", clientMajor, serverMajor),
			Hint:   fmt.Sprintf("Install postgresql%d-client to match the server major version %d.", serverMajor, serverMajor),
		}, false
	}
	return PreflightCheck{
		Name:   name,
		Status: "ok",
		Detail: fmt.Sprintf("pg_dump %d, server %d", clientMajor, serverMajor),
	}, true
}

func checkDBSize(ctx context.Context, dsn string) (PreflightCheck, int64) {
	creds, err := ParseDSN(dsn)
	if err != nil {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: "could not parse DSN to estimate database size",
		}, 0
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: fmt.Sprintf("could not open DB connection: %v", err),
		}, 0
	}
	defer db.Close()

	var sizeBytes int64
	if err := db.QueryRowContext(ctx, "SELECT pg_database_size($1)", creds.DBName).Scan(&sizeBytes); err != nil {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: fmt.Sprintf("could not query database size: %v", err),
		}, 0
	}
	return PreflightCheck{
		Name:   "db_size",
		Status: "ok",
		Detail: fmt.Sprintf("estimated %d MB", sizeBytes>>20),
	}, sizeBytes
}
