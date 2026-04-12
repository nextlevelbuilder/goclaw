//go:build !sqliteonly

package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// DumpDatabase runs pg_dump and streams plain-SQL output to w.
// Uses a temporary .pgpass file (0600) to pass the password securely.
// The child process receives only PGPASSFILE, PATH, HOME, LC_ALL=C.
func DumpDatabase(ctx context.Context, dsn string, w io.Writer) error {
	creds, err := ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}

	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		return fmt.Errorf("pg_dump not found on PATH: %w", err)
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
		"--format=plain",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
	}

	cmd := exec.CommandContext(ctx, pgDump, args...)
	cmd.Env = CleanEnv(pgpassPath)
	cmd.Stdout = w

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("pg_dump failed: %s", errMsg)
	}
	return nil
}

// PgDumpVersion returns the version string from pg_dump --version.
func PgDumpVersion(ctx context.Context) (string, error) {
	pgDump, err := exec.LookPath("pg_dump")
	if err != nil {
		return "", fmt.Errorf("pg_dump not found: %w", err)
	}

	out, err := exec.CommandContext(ctx, pgDump, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("pg_dump --version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ParsePgDumpMajor extracts the PostgreSQL major version number from a
// pg_dump --version string. Returns 0 if parsing fails.
// Example inputs:
//
//	"pg_dump (PostgreSQL) 17.9 (Debian 17.9-1.pgdg12+1)" -> 17
//	"pg_dump (PostgreSQL) 18.3"                          -> 18
func ParsePgDumpMajor(version string) int {
	const marker = "(PostgreSQL) "
	idx := strings.Index(version, marker)
	if idx < 0 {
		return 0
	}
	rest := version[idx+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	major, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return major
}
