//go:build !sqliteonly

package backup

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// RestoreDatabase restores a PostgreSQL database from a plain-SQL dump reader.
// Uses a temporary .pgpass file (0600) to pass credentials securely.
// The child psql process receives only PGPASSFILE, PATH, HOME, LC_ALL=C.
func RestoreDatabase(ctx context.Context, dsn string, dumpReader io.Reader) error {
	creds, err := ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}

	psql, err := exec.LookPath("psql")
	if err != nil {
		return fmt.Errorf("psql not found on PATH: %w", err)
	}

	tempDir, pgpassPath, err := WritePgpass(creds)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	args := []string{
		"--host", creds.Host,
		"--port", creds.Port,
		"--username", creds.User,
		"--dbname", creds.DBName,
		"--no-password",
	}

	cmd := exec.CommandContext(ctx, psql, args...)
	cmd.Env = CleanEnv(pgpassPath)
	cmd.Stdin = dumpReader

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		// Truncate very long psql error output.
		if len(errMsg) > 512 {
			errMsg = errMsg[:512] + "..."
		}
		return fmt.Errorf("psql restore failed: %s", errMsg)
	}
	return nil
}

// CheckActiveConnections returns the number of active backend connections to the
// database (excluding the current connection). Used as a pre-restore safety check.
func CheckActiveConnections(ctx context.Context, dsn string) (int, error) {
	creds, err := ParseDSN(dsn)
	if err != nil {
		return 0, fmt.Errorf("parse DSN: %w", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var count int
	query := `SELECT COUNT(*) FROM pg_stat_activity
	           WHERE datname = $1 AND pid <> pg_backend_pid()`
	if err := db.QueryRowContext(ctx, query, creds.DBName).Scan(&count); err != nil {
		return 0, fmt.Errorf("query pg_stat_activity: %w", err)
	}
	return count, nil
}
