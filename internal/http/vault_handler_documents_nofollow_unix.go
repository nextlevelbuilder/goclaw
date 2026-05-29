//go:build !windows

package http

import (
	"errors"
	"syscall"
)

// oNoFollow is OR'd into the open flags so the kernel refuses to follow a
// symlink at the final path component — closes the Lstat→Open TOCTOU window
// where a tenant could swap a regular file for a symlink between the two
// syscalls. On Windows this is a no-op (see the _windows.go counterpart).
const oNoFollow = syscall.O_NOFOLLOW

// isSymlinkLoopErr reports whether err is the kernel's "refused to follow
// symlink" signal from an O_NOFOLLOW open (ELOOP on Linux/macOS).
func isSymlinkLoopErr(err error) bool {
	return errors.Is(err, syscall.ELOOP)
}
