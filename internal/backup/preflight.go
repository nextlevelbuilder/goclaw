package backup

import (
	"context"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"syscall"
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
	var checks []PreflightCheck
	ready := true

	pgDumpCheck := checkPgDump(ctx)
	checks = append(checks, pgDumpCheck)
	pgDumpAvail := pgDumpCheck.Status != "missing"
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
	warnings := make([]string, 0)
	for _, c := range checks {
		if c.Status == "warning" {
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

func checkPgDump(ctx context.Context) PreflightCheck {
	path, err := exec.LookPath("pg_dump")
	if err != nil {
		return PreflightCheck{
			Name:   "pg_dump",
			Status: "missing",
			Detail: "pg_dump not found on PATH",
			Hint:   "Install postgresql-client or add pg_dump to PATH. Filesystem-only backup still works with --exclude-db.",
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

func checkDiskSpace(dir string) (PreflightCheck, int64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "warning",
			Detail: fmt.Sprintf("could not check disk space: %v", err),
		}, 0
	}
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	const minFree = 1 << 30 // 1 GB
	if freeBytes < minFree {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "missing",
			Detail: fmt.Sprintf("only %d MB free (need at least 1 GB)", freeBytes>>20),
			Hint:   "Free up disk space before running a backup.",
		}, int64(freeBytes)
	}
	return PreflightCheck{
		Name:   "disk_space",
		Status: "ok",
		Detail: fmt.Sprintf("%d MB free", freeBytes>>20),
	}, int64(freeBytes)
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
