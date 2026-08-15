//go:build sqliteonly

package backup

import (
	"context"
	"fmt"
	"io"
	"os"
)

// RestoreDatabase copies the dump stream directly to the SQLite database file.
// dsn is expected to be a file path or "file:/path/to/db" format.
func RestoreDatabase(_ context.Context, dsn string, dumpReader io.Reader) error {
	dbPath := parseSQLitePath(dsn)
	if dbPath == "" {
		return fmt.Errorf("could not resolve SQLite database path from DSN: %q", dsn)
	}

	f, err := os.OpenFile(dbPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open SQLite db for restore %q: %w", dbPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, dumpReader); err != nil {
		return fmt.Errorf("write SQLite db: %w", err)
	}
	return nil
}

// CheckActiveConnections always returns 0 for SQLite builds.
// SQLite does not have a server process; concurrent access is handled by file locks.
func CheckActiveConnections(_ context.Context, _ string) (int, error) {
	return 0, nil
}
