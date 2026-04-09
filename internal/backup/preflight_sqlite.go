//go:build sqliteonly

package backup

import (
	"context"
	"fmt"
	"os"
)

func checkDBSize(ctx context.Context, dsn string) PreflightCheck {
	dbPath := parseSQLitePath(dsn)
	if dbPath == "" {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: "could not resolve SQLite database path",
		}
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		return PreflightCheck{
			Name:   "db_size",
			Status: "warning",
			Detail: fmt.Sprintf("could not stat SQLite db: %v", err),
		}
	}
	return PreflightCheck{
		Name:   "db_size",
		Status: "ok",
		Detail: fmt.Sprintf("SQLite db %d MB", info.Size()>>20),
	}
}
