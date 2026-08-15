//go:build darwin

package tools

import (
	"errors"

	"golang.org/x/sys/unix"
)

func (r *artifactSecureRoot) renameNoReplace(oldName, newName string) error {
	oldPath, err := validateArtifactRelativePath(oldName)
	if err != nil {
		return err
	}
	newPath, err := validateArtifactRelativePath(newName)
	if err != nil {
		return err
	}
	err = unix.RenameatxNp(r.fd, oldPath, r.fd, newPath, unix.RENAME_EXCL)
	if errors.Is(err, unix.EEXIST) {
		return errors.Join(ErrArtifactPublishConflict, err)
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) {
		return errors.Join(ErrArtifactSecureUnavailable, err)
	}
	return err
}

func isArtifactNotExist(err error) bool {
	return errors.Is(err, unix.ENOENT)
}
