//go:build sqliteonly

package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// DumpDatabase copies the SQLite database file to w.
// dsn is expected to be a file path or "file:/path/to/db" format.
func DumpDatabase(ctx context.Context, dsn string, w io.Writer) error {
	dbPath := parseSQLitePath(dsn)
	if dbPath == "" {
		return fmt.Errorf("could not resolve SQLite database path from DSN: %q", dsn)
	}

	f, err := os.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open SQLite db %q: %w", dbPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("copy SQLite db: %w", err)
	}
	return nil
}

// PgDumpVersion returns "sqlite" for SQLite builds (no pg_dump available).
func PgDumpVersion(_ context.Context) (string, error) {
	return "sqlite", nil
}

// parseSQLitePath extracts the file path from a SQLite DSN.
// Handles formats: "/path/to/db", "file:/path/to/db", "file:///path/to/db".
func parseSQLitePath(dsn string) string {
	if strings.HasPrefix(dsn, "file:///") {
		return strings.TrimPrefix(dsn, "file://")
	}
	if strings.HasPrefix(dsn, "file:/") {
		return strings.TrimPrefix(dsn, "file:")
	}
	return dsn
}
