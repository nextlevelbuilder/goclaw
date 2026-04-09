package backup

import (
	"context"
	"fmt"
	"os/exec"
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
}

// RunPreflight checks prerequisites before running a backup.
// Checks: pg_dump binary, free disk space, estimated DB size (PG builds only).
// A missing pg_dump makes ready=false, but filesystem-only backup may still work.
func RunPreflight(ctx context.Context, dsn, dataDir, workspace string) *PreflightResult {
	var checks []PreflightCheck
	ready := true

	pgDumpCheck := checkPgDump(ctx)
	checks = append(checks, pgDumpCheck)
	if pgDumpCheck.Status == "missing" {
		ready = false
	}

	diskCheck := checkDiskSpace(".")
	checks = append(checks, diskCheck)
	if diskCheck.Status == "missing" {
		ready = false
	}

	// DB size check is build-tag gated — implemented in preflight_pg.go / preflight_sqlite.go.
	if dsn != "" {
		dbSizeCheck := checkDBSize(ctx, dsn)
		checks = append(checks, dbSizeCheck)
	}

	return &PreflightResult{Ready: ready, Checks: checks}
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

func checkDiskSpace(dir string) PreflightCheck {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "warning",
			Detail: fmt.Sprintf("could not check disk space: %v", err),
		}
	}
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	const minFree = 1 << 30 // 1 GB
	if freeBytes < minFree {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "missing",
			Detail: fmt.Sprintf("only %d MB free (need at least 1 GB)", freeBytes>>20),
			Hint:   "Free up disk space before running a backup.",
		}
	}
	return PreflightCheck{
		Name:   "disk_space",
		Status: "ok",
		Detail: fmt.Sprintf("%d MB free", freeBytes>>20),
	}
}
