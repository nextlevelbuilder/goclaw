//go:build sqliteonly

package backup

import (
	"context"
	"fmt"
	"os"
)

// detectPGServerMajor is a no-op for SQLite builds; always returns 0 so
// RunPreflight falls back to a generic pg_dump hint.
func detectPGServerMajor(_ context.Context, _ string) int {
	return 0
}

// checkPgDumpServerCompat is a no-op for SQLite builds (no pg_dump involved).
// Returns an empty check (Name=="") so RunPreflight skips appending it.
func checkPgDumpServerCompat(_ context.Context, _ int) (PreflightCheck, bool) {
	return PreflightCheck{}, true
}

func checkDBSize(ctx context.Context, dsn string) (PreflightCheck, int64) {
	dbPath := parseSQLitePath(dsn)
	if dbPath == "" {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: "could not resolve SQLite database path",
		}, 0
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: fmt.Sprintf("could not stat SQLite db: %v", err),
		}, 0
	}
	return PreflightCheck{
		Name:   "db_size",
		Status: "ok",
		Detail: fmt.Sprintf("SQLite db %d MB", info.Size()>>20),
	}, info.Size()
}
