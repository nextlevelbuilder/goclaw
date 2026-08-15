package backup

import (
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
)

// PreflightCheck is the result of a single preflight validation item.
type PreflightCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "missing", "warning"
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// PreflightResult summarises whether backup can proceed.
type PreflightResult struct {
	Ready  bool             `json:"ready"`
	Checks []PreflightCheck `json:"checks"`

	// Flat fields consumed by the HTTP layer.
	PgDumpAvailable    bool
	DiskSpaceOK        bool
	FreeDiskBytes      int64
	DbSizeBytes        int64
	DataDirSizeBytes   int64
	WorkspaceSizeBytes int64
	Warnings           []string
}

// RunPreflight checks prerequisites before running a backup.
// Checks: pg_dump binary, free disk space, estimated DB size (PG builds only).
// A missing pg_dump makes ready=false, but filesystem-only backup may still work.
func RunPreflight(ctx context.Context, dsn, dataDir, workspace string) *PreflightResult {
	// Normalize a nil ctx once so every downstream helper (including
	// exec.CommandContext, which panics on nil) is safe. Historical callers
	// pass nil from tests; defensive at the boundary is cheaper than
	// per-helper guards.
	if ctx == nil {
		ctx = context.Background()
	}

	var checks []PreflightCheck
	ready := true

	// Detect the server's PostgreSQL major version once (if a DSN is
	// configured and reachable). We use it to tailor both the "pg_dump
	// missing" hint and the "pg_dump incompatible" hint so the user always
	// sees the exact postgresqlNN-client package to install. Returns 0 on
	// any error or for SQLite builds (stub).
	serverMajor := 0
	if dsn != "" {
		serverMajor = detectPGServerMajor(ctx, dsn)
	}

	pgDumpCheck := checkPgDump(ctx, serverMajor)
	checks = append(checks, pgDumpCheck)
	pgDumpAvail := pgDumpCheck.Status != "missing"

	// If pg_dump is present and we know the server major, verify its major
	// version is compatible. pg_dump aborts at runtime when client major <
	// server major, so we surface this up front as an actionable "not
	// available" state rather than letting the backup fail partway through.
	if pgDumpAvail && serverMajor > 0 {
		compatCheck, compatOK := checkPgDumpServerCompat(ctx, serverMajor)
		if compatCheck.Name != "" { // sqlite stub returns empty check
			checks = append(checks, compatCheck)
			if !compatOK {
				pgDumpAvail = false
			}
		}
	}
	if !pgDumpAvail {
		ready = false
	}

	diskCheck, freeDisk := checkDiskSpace(".")
	checks = append(checks, diskCheck)
	diskOK := diskCheck.Status != "missing"
	if !diskOK {
		ready = false
	}

	var dbSizeBytes int64
	if dsn != "" {
		dbCheck, dbBytes := checkDBSize(ctx, dsn)
		checks = append(checks, dbCheck)
		dbSizeBytes = dbBytes
	}

	// Collect warnings from non-ok checks (use make to avoid JSON null).
	// Both "warning" and "missing" surface Detail (the problem) so the user
	// sees the actual cause, not just the Hint (the fix).
	warnings := make([]string, 0)
	for _, c := range checks {
		if (c.Status == "warning" || c.Status == "missing") && c.Detail != "" {
			warnings = append(warnings, c.Detail)
		}
		if c.Hint != "" {
			warnings = append(warnings, c.Hint)
		}
	}

	return &PreflightResult{
		Ready:              ready,
		Checks:             checks,
		PgDumpAvailable:    pgDumpAvail,
		DiskSpaceOK:        diskOK,
		FreeDiskBytes:      freeDisk,
		DbSizeBytes:        dbSizeBytes,
		DataDirSizeBytes:   DirSize(dataDir),
		WorkspaceSizeBytes: DirSize(workspace),
		Warnings:           warnings,
	}
}

// checkPgDump verifies pg_dump is on PATH. When serverMajor > 0 (detected
// from the live PG server), the missing-hint names the exact package to
// install (e.g. postgresql18-client). When serverMajor is 0 (no DSN, or
// server unreachable, or SQLite build), we fall back to a generic hint.
func checkPgDump(ctx context.Context, serverMajor int) PreflightCheck {
	if ctx == nil {
		ctx = context.Background()
	}
	path, err := exec.LookPath("pg_dump")
	if err != nil {
		hint := "Install a PostgreSQL client package whose major version matches your server, or add pg_dump to PATH. Filesystem-only backup still works with --exclude-db."
		if serverMajor > 0 {
			hint = fmt.Sprintf("Install postgresql%d-client to match your PostgreSQL %d server, or add pg_dump to PATH. Filesystem-only backup still works with --exclude-db.", serverMajor, serverMajor)
		}
		return PreflightCheck{
			Name:   "pg_dump",
			Status: "missing",
			Detail: "pg_dump not found on PATH",
			Hint:   hint,
		}
	}
	ver, verErr := PgDumpVersion(ctx)
	if verErr != nil {
		return PreflightCheck{
			Name:   "pg_dump",
			Status: "warning",
			Detail: fmt.Sprintf("found at %s but could not get version: %v", path, verErr),
		}
	}
	return PreflightCheck{
		Name:   "pg_dump",
		Status: "ok",
		Detail: fmt.Sprintf("%s (%s)", path, ver),
	}
}

// DirSize returns the total size of all regular files under path.
// Returns 0 on any error (missing dir, permission, etc.).
func DirSize(path string) int64 {
	if path == "" {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors, best-effort
		}
		if !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// FormatBytes returns a human-readable byte size (e.g. "1.5 GB", "340 MB").
func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
