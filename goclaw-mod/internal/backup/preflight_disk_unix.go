//go:build !windows

package backup

import (
	"fmt"
	"syscall"
)

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
