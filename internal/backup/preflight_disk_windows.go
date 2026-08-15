//go:build windows

package backup

import (
	"fmt"
	"syscall"
	"unsafe"
)

func checkDiskSpace(dir string) (PreflightCheck, int64) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	dirPtr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "warning",
			Detail: fmt.Sprintf("could not check disk space: %v", err),
		}, 0
	}
	var freeBytesAvailable uint64
	ret, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		0,
		0,
	)
	if ret == 0 {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "warning",
			Detail: fmt.Sprintf("could not check disk space: %v", callErr),
		}, 0
	}
	const minFree = 1 << 30 // 1 GB
	if freeBytesAvailable < uint64(minFree) {
		return PreflightCheck{
			Name:   "disk_space",
			Status: "missing",
			Detail: fmt.Sprintf("only %d MB free (need at least 1 GB)", freeBytesAvailable>>20),
			Hint:   "Free up disk space before running a backup.",
		}, int64(freeBytesAvailable)
	}
	return PreflightCheck{
		Name:   "disk_space",
		Status: "ok",
		Detail: fmt.Sprintf("%d MB free", freeBytesAvailable>>20),
	}, int64(freeBytesAvailable)
}
