//go:build !windows

package tools

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// hasMutableSymlinkParent checks if any component of the resolved path is a symlink
// whose parent directory is writable by the current process. A writable parent means
// the symlink could be replaced between path resolution and actual file operation
// (TOCTOU symlink rebind attack).
func hasMutableSymlinkParent(path string) bool {
	clean := filepath.Clean(path)
	components := strings.Split(clean, string(filepath.Separator))
	current := string(filepath.Separator)
	for _, comp := range components {
		if comp == "" {
			continue
		}
		current = filepath.Join(current, comp)
		info, err := os.Lstat(current)
		if err != nil {
			break // non-existent — stop checking
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Symlink found — check if its parent dir is writable
			parentDir := filepath.Dir(current)
			if syscall.Access(parentDir, 0x2 /* W_OK */) == nil {
				return true
			}
		}
	}
	return false
}

// checkHardlink rejects regular files with nlink > 1 (hardlink attack prevention).
// Directories naturally have nlink > 1 and are exempt.
func checkHardlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return nil // non-existent files are OK — will fail at read/write
	}
	if info.IsDir() {
		return nil
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if stat.Nlink > 1 {
			slog.Warn("security.hardlink_rejected", "path", path, "nlink", stat.Nlink)
			return fmt.Errorf("access denied: hardlinked file not allowed")
		}
	}
	return nil
}
