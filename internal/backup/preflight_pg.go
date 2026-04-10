//go:build !sqliteonly

package backup

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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
