//go:build linux || darwin

package tools

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

type artifactSecureRoot struct {
	fd int
}

func openArtifactSecureRoot(hostPath string) (*artifactSecureRoot, error) {
	fd, err := unix.Open(hostPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, translateArtifactPOSIXError(err)
	}
	return &artifactSecureRoot{fd: fd}, nil
}

func (r *artifactSecureRoot) close() error {
	if r == nil || r.fd < 0 {
		return nil
	}
	fd := r.fd
	r.fd = -1
	return unix.Close(fd)
}

func (r *artifactSecureRoot) mkdirAll(relativePath string, mode fs.FileMode) error {
	components, err := artifactPathComponents(relativePath)
	if err != nil {
		return err
	}
	current, err := unix.Dup(r.fd)
	if err != nil {
		return err
	}
	unix.CloseOnExec(current)
	defer func() {
		_ = unix.Close(current)
	}()
	for _, component := range components {
		if err := unix.Mkdirat(current, component, uint32(mode.Perm())); err != nil && !errors.Is(err, unix.EEXIST) {
			return translateArtifactPOSIXError(err)
		}
		next, err := unix.Openat(
			current,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
			0,
		)
		if err != nil {
			return translateArtifactPOSIXError(err)
		}
		unix.Close(current)
		current = next
	}
	return nil
}

func (r *artifactSecureRoot) createSubroot(relativePath string, mode fs.FileMode) (*artifactSecureRoot, error) {
	parent, name, err := r.openParent(relativePath, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	if err := unix.Mkdirat(parent, name, uint32(mode.Perm())); err != nil {
		return nil, translateArtifactPOSIXError(err)
	}
	fd, err := unix.Openat(
		parent,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, translateArtifactPOSIXError(err)
	}
	return &artifactSecureRoot{fd: fd}, nil
}

func (r *artifactSecureRoot) openSubroot(relativePath string) (*artifactSecureRoot, error) {
	fd, err := r.openDir(relativePath)
	if err != nil {
		return nil, err
	}
	return &artifactSecureRoot{fd: fd}, nil
}

func (r *artifactSecureRoot) createFile(
	relativePath string,
	mode fs.FileMode,
) (*os.File, error) {
	parent, name, err := r.openParent(relativePath, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(
		parent,
		name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		uint32(mode.Perm()),
	)
	if err != nil {
		return nil, translateArtifactPOSIXError(err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (r *artifactSecureRoot) openEntry(relativePath string) (*artifactSecureEntry, error) {
	parent, name, err := r.openParent(relativePath, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(
		parent,
		name,
		unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, translateArtifactPOSIXError(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		return nil, err
	}
	kind := artifactEntryOther
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		kind = artifactEntryRegular
	case unix.S_IFDIR:
		kind = artifactEntryDirectory
	}
	return &artifactSecureEntry{
		file:  os.NewFile(uintptr(fd), name),
		kind:  kind,
		size:  stat.Size,
		links: uint64(stat.Nlink),
	}, nil
}

func (r *artifactSecureRoot) readDir(relativePath string) ([]string, error) {
	entry, err := r.openEntry(relativePath)
	if err != nil {
		return nil, err
	}
	defer entry.close()
	if entry.kind != artifactEntryDirectory {
		return nil, ErrArtifactNonRegular
	}

	names := make([]string, 0, artifactSecureReadBatchSize)
	for {
		dirEntries, readErr := entry.readDirBatch()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if len(dirEntries) > artifactSecureMaxDirectoryEntries-len(names) {
			return nil, ErrArtifactLimitExceeded
		}
		for _, dirEntry := range dirEntries {
			names = append(names, dirEntry.Name())
		}
		if errors.Is(readErr, io.EOF) {
			return names, nil
		}
	}
}

func (r *artifactSecureRoot) exists(relativePath string) (bool, error) {
	parent, name, err := r.openParent(relativePath, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer unix.Close(parent)
	var stat unix.Stat_t
	err = unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ENOENT):
		return false, nil
	default:
		return false, translateArtifactPOSIXError(err)
	}
}

func (r *artifactSecureRoot) syncDir(relativePath string) error {
	fd, err := r.openDir(relativePath)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	err = unix.Fsync(fd)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	return err
}

func (r *artifactSecureRoot) removeTree(relativePath string) error {
	parent, name, err := r.openParent(relativePath, false)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	remaining := artifactSecureCleanupEntryBudget
	return removeArtifactTreeAt(parent, name, 0, &remaining)
}

func removeArtifactTreeAt(parent int, name string, depth int, remaining *int) error {
	if depth > artifactSecureMaxDepth {
		return ErrArtifactLimitExceeded
	}
	if *remaining <= 0 {
		return ErrArtifactLimitExceeded
	}
	*remaining--
	var stat unix.Stat_t
	if err := unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return translateArtifactPOSIXError(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return translateArtifactPOSIXError(unix.Unlinkat(parent, name, 0))
	}

	fd, err := unix.Openat(
		parent,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0,
	)
	if err != nil {
		return translateArtifactPOSIXError(err)
	}
	dir := os.NewFile(uintptr(fd), name)
	for {
		entries, readErr := dir.ReadDir(artifactSecureReadBatchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = dir.Close()
			return readErr
		}
		for _, entry := range entries {
			if err := removeArtifactTreeAt(fd, entry.Name(), depth+1, remaining); err != nil {
				_ = dir.Close()
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if err := dir.Close(); err != nil {
		return err
	}
	return translateArtifactPOSIXError(unix.Unlinkat(parent, name, unix.AT_REMOVEDIR))
}

func (r *artifactSecureRoot) renameReplace(oldName, newName string) error {
	oldPath, err := validateArtifactRelativePath(oldName)
	if err != nil {
		return err
	}
	newPath, err := validateArtifactRelativePath(newName)
	if err != nil {
		return err
	}
	if path.Dir(oldPath) != "." || path.Dir(newPath) != "." {
		return ErrArtifactInvalidPath
	}
	return translateArtifactPOSIXError(unix.Renameat(r.fd, oldPath, r.fd, newPath))
}

func (r *artifactSecureRoot) openParent(
	relativePath string,
	create bool,
) (int, string, error) {
	components, err := artifactPathComponents(relativePath)
	if err != nil {
		return -1, "", err
	}
	if len(components) == 0 {
		return -1, "", ErrArtifactInvalidPath
	}
	parentComponents := components[:len(components)-1]
	current, err := unix.Dup(r.fd)
	if err != nil {
		return -1, "", err
	}
	unix.CloseOnExec(current)
	for _, component := range parentComponents {
		if create {
			if err := unix.Mkdirat(current, component, 0750); err != nil && !errors.Is(err, unix.EEXIST) {
				unix.Close(current)
				return -1, "", translateArtifactPOSIXError(err)
			}
		}
		next, err := unix.Openat(
			current,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
			0,
		)
		if err != nil {
			unix.Close(current)
			return -1, "", translateArtifactPOSIXError(err)
		}
		unix.Close(current)
		current = next
	}
	return current, components[len(components)-1], nil
}

func (r *artifactSecureRoot) openDir(relativePath string) (int, error) {
	if relativePath == "." {
		fd, err := unix.Dup(r.fd)
		if err == nil {
			unix.CloseOnExec(fd)
		}
		return fd, err
	}
	components, err := artifactPathComponents(relativePath)
	if err != nil {
		return -1, err
	}
	current, err := unix.Dup(r.fd)
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(current)
	for _, component := range components {
		next, err := unix.Openat(
			current,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
			0,
		)
		if err != nil {
			unix.Close(current)
			return -1, translateArtifactPOSIXError(err)
		}
		unix.Close(current)
		current = next
	}
	return current, nil
}

func artifactPathComponents(relativePath string) ([]string, error) {
	if relativePath == "." {
		return nil, nil
	}
	clean, err := validateArtifactRelativePath(relativePath)
	if err != nil {
		return nil, err
	}
	return strings.Split(clean, "/"), nil
}

func translateArtifactPOSIXError(err error) error {
	if errors.Is(err, unix.ELOOP) {
		return fmtArtifactJoined(errArtifactSymlink, err)
	}
	return err
}

func fmtArtifactJoined(classification, cause error) error {
	return errors.Join(classification, cause)
}

func isArtifactAlreadyExists(err error) bool {
	return errors.Is(err, unix.EEXIST) || errors.Is(err, ErrArtifactPublishConflict)
}
