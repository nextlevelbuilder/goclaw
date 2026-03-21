//go:build windows

package tools

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
)

// hasMutableSymlinkParent checks if any component of the resolved path is a symlink
// whose parent directory is writable by the current process. A writable parent means
// the symlink could be replaced between path resolution and actual file operation
// (TOCTOU symlink rebind attack).
func hasMutableSymlinkParent(path string) bool {
	clean := filepath.Clean(path)

	// Traverse from leaf up to root to find all path components.
	var components []string
	curr := clean
	for {
		components = append([]string{curr}, components...)
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	for _, p := range components {
		info, err := os.Lstat(p)
		if err != nil {
			break // non-existent — stop checking
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Symlink found — check if its parent dir is writable
			if isDirWritable(filepath.Dir(p)) {
				return true
			}
		}
	}
	return false
}

// isDirWritable checks if a directory is writable by attempting to open it with write access.
// On Windows, this is more robust than temporary file creation and avoids artifact leakage.
func isDirWritable(dir string) bool {
	dirUTF16, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return false
	}
	// FILE_FLAG_BACKUP_SEMANTICS is required to open a handle to a directory.
	h, err := syscall.CreateFile(
		dirUTF16,
		syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0)
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}

// checkHardlink rejects regular files with NumberOfLinks > 1 (hardlink attack prevention).
func checkHardlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return nil
	}

	pathUTF16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}

	h, err := syscall.CreateFile(
		pathUTF16,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)

	var fi syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &fi); err != nil {
		return nil
	}

	if fi.NumberOfLinks > 1 {
		slog.Warn("security.hardlink_rejected", "path", path, "nlink", fi.NumberOfLinks)
		return fmt.Errorf("access denied: hardlinked file not allowed")
	}
	return nil
}
