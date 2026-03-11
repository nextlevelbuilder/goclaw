//go:build windows

package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// hasMutableSymlinkParent checks for mutable symlink parents.
// On Windows, symlink rebind attacks are less common; we do a best-effort check
// using file attribute inspection instead of syscall.Access.
func hasMutableSymlinkParent(path string) bool {
	clean := filepath.Clean(path)
	components := strings.Split(clean, string(filepath.Separator))
	current := ""
	for _, comp := range components {
		if comp == "" {
			continue
		}
		if current == "" {
			current = comp
		} else {
			current = filepath.Join(current, comp)
		}
		info, err := os.Lstat(current)
		if err != nil {
			break // non-existent — stop checking
		}
		if info.Mode()&os.ModeSymlink != 0 {
			parentDir := filepath.Dir(current)
			// Check writability by attempting to open for writing
			if isDirWritable(parentDir) {
				return true
			}
		}
	}
	return false
}

// isDirWritable checks if a directory is writable by attempting to create a temp file.
func isDirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".wrchk")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// checkHardlink rejects regular files with nlink > 1 (hardlink attack prevention).
// On Windows, os.FileInfo.Sys() does not expose nlink, so we skip the check.
func checkHardlink(path string) error {
	return nil
}
