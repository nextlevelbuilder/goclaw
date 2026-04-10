// Package backup provides system-level backup and restore for GoClaw.
package backup

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// PGCredentials holds parsed PostgreSQL connection parameters.
type PGCredentials struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// ParseDSN extracts connection parameters from a PostgreSQL DSN URL.
// Accepts format: postgres://user:password@host:port/dbname?sslmode=disable
func ParseDSN(dsn string) (*PGCredentials, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("unsupported scheme %q, expected postgres://", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}

	user := ""
	password := ""
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	dbname := strings.TrimPrefix(u.Path, "/")
	if dbname == "" {
		return nil, fmt.Errorf("database name is required in DSN")
	}

	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "prefer"
	}

	return &PGCredentials{
		Host: host, Port: port,
		User: user, Password: password,
		DBName: dbname, SSLMode: sslmode,
	}, nil
}

// WritePgpass creates a temporary .pgpass file with 0600 permissions.
// Returns the temp directory path (caller must defer os.RemoveAll).
// The .pgpass file path is returned as second value.
func WritePgpass(creds *PGCredentials) (tempDir, pgpassPath string, err error) {
	tempDir, err = os.MkdirTemp("", "goclaw-backup-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}

	pgpassPath = filepath.Join(tempDir, ".pgpass")
	// Format: hostname:port:database:username:password
	// Colons and backslashes in values must be escaped with backslash.
	content := fmt.Sprintf("%s:%s:%s:%s:%s\n",
		escapePgpass(creds.Host),
		escapePgpass(creds.Port),
		escapePgpass(creds.DBName),
		escapePgpass(creds.User),
		escapePgpass(creds.Password),
	)

	if err := os.WriteFile(pgpassPath, []byte(content), 0600); err != nil {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("write .pgpass: %w", err)
	}

	return tempDir, pgpassPath, nil
}

// CleanEnv returns a minimal environment for pg_dump/psql child processes.
// Only includes PGPASSFILE, PATH, HOME, and locale — no secrets leak.
func CleanEnv(pgpassPath string) []string {
	return []string{
		"PGPASSFILE=" + pgpassPath,
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"LC_ALL=C",
	}
}

// SanitizeDSN strips the password from a DSN for safe logging/manifest.
func SanitizeDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "***"
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

// escapePgpass escapes colons and backslashes in .pgpass field values.
func escapePgpass(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `:`, `\:`)
	return s
}
