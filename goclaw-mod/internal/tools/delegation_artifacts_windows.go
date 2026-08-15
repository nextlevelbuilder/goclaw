//go:build windows

package tools

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type artifactSecureRoot struct {
	handle windows.Handle
}

type artifactFileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

func openArtifactSecureRoot(hostPath string) (*artifactSecureRoot, error) {
	absolute, err := filepath.Abs(hostPath)
	if err != nil {
		return nil, err
	}
	handle, err := artifactNtOpen(
		0,
		artifactNTAbsolutePath(absolute),
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_OPEN,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, translateArtifactWindowsError(err)
	}
	if err := rejectArtifactReparsePoint(handle); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	return &artifactSecureRoot{handle: handle}, nil
}

func (r *artifactSecureRoot) close() error {
	if r == nil || r.handle == 0 || r.handle == windows.InvalidHandle {
		return nil
	}
	handle := r.handle
	r.handle = windows.InvalidHandle
	return windows.CloseHandle(handle)
}

func (r *artifactSecureRoot) mkdirAll(relativePath string, _ fs.FileMode) error {
	components, err := artifactPathComponentsWindows(relativePath)
	if err != nil {
		return err
	}
	current, err := duplicateArtifactHandle(r.handle)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(current)
	for _, component := range components {
		next, err := artifactNtOpen(
			current,
			component,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
			windows.FILE_OPEN_IF,
			windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT,
			windows.FILE_ATTRIBUTE_NORMAL,
		)
		if err != nil {
			return translateArtifactWindowsError(err)
		}
		if err := rejectArtifactReparsePoint(next); err != nil {
			windows.CloseHandle(next)
			return err
		}
		windows.CloseHandle(current)
		current = next
	}
	return nil
}

func (r *artifactSecureRoot) createSubroot(relativePath string, _ fs.FileMode) (*artifactSecureRoot, error) {
	parent, name, err := r.openParent(relativePath)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(parent)
	handle, err := artifactNtOpen(
		parent,
		name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_CREATE,
		windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT,
		windows.FILE_ATTRIBUTE_NORMAL,
	)
	if err != nil {
		return nil, translateArtifactWindowsError(err)
	}
	if err := rejectArtifactReparsePoint(handle); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	return &artifactSecureRoot{handle: handle}, nil
}

func (r *artifactSecureRoot) openSubroot(relativePath string) (*artifactSecureRoot, error) {
	handle, err := r.openDir(relativePath)
	if err != nil {
		return nil, err
	}
	return &artifactSecureRoot{handle: handle}, nil
}

func (r *artifactSecureRoot) createFile(relativePath string, _ fs.FileMode) (*os.File, error) {
	parent, name, err := r.openParent(relativePath)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(parent)
	handle, err := artifactNtOpen(
		parent,
		name,
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_CREATE,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT,
		windows.FILE_ATTRIBUTE_NORMAL,
	)
	if err != nil {
		return nil, translateArtifactWindowsError(err)
	}
	if err := rejectArtifactReparsePoint(handle); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func (r *artifactSecureRoot) openEntry(relativePath string) (*artifactSecureEntry, error) {
	parent, name, err := r.openParent(relativePath)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(parent)
	handle, err := artifactNtOpen(
		parent,
		name,
		windows.FILE_GENERIC_READ|windows.SYNCHRONIZE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, translateArtifactWindowsError(err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(handle)
		return nil, errArtifactReparsePoint
	}
	kind := artifactEntryRegular
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		kind = artifactEntryDirectory
	}
	size := int64(uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow))
	return &artifactSecureEntry{
		file:  os.NewFile(uintptr(handle), name),
		kind:  kind,
		size:  size,
		links: uint64(info.NumberOfLinks),
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
	parent, name, err := r.openParent(relativePath)
	if err != nil {
		if isArtifactWindowsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(parent)
	handle, err := artifactNtOpen(
		parent,
		name,
		windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		if isArtifactWindowsNotExist(err) {
			return false, nil
		}
		return false, translateArtifactWindowsError(err)
	}
	windows.CloseHandle(handle)
	return true, nil
}

func (r *artifactSecureRoot) syncDir(relativePath string) error {
	handle, err := r.openDir(relativePath)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	err = windows.FlushFileBuffers(handle)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return nil
	}
	return err
}

func (r *artifactSecureRoot) removeTree(relativePath string) error {
	parent, name, err := r.openParent(relativePath)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	remaining := artifactSecureCleanupEntryBudget
	return removeArtifactTreeAt(parent, name, 0, &remaining)
}

func removeArtifactTreeAt(parent windows.Handle, name string, depth int, remaining *int) error {
	if depth > artifactSecureMaxDepth {
		return ErrArtifactLimitExceeded
	}
	if *remaining <= 0 {
		return ErrArtifactLimitExceeded
	}
	*remaining--
	handle, err := artifactNtOpen(
		parent,
		name,
		windows.FILE_GENERIC_READ|windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		if isArtifactWindowsNotExist(err) {
			return nil
		}
		return translateArtifactWindowsError(err)
	}
	defer windows.CloseHandle(handle)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	isDirectory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	isReparsePoint := info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	if isDirectory && !isReparsePoint {
		duplicate, err := duplicateArtifactHandle(handle)
		if err != nil {
			return err
		}
		dir := os.NewFile(uintptr(duplicate), name)
		for {
			entries, readErr := dir.ReadDir(artifactSecureReadBatchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				_ = dir.Close()
				return readErr
			}
			for _, entry := range entries {
				if err := removeArtifactTreeAt(handle, entry.Name(), depth+1, remaining); err != nil {
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
	}
	deleteFlag := byte(1)
	return windows.SetFileInformationByHandle(
		handle,
		windows.FileDispositionInfo,
		&deleteFlag,
		1,
	)
}

func (r *artifactSecureRoot) renameNoReplace(oldName, newName string) error {
	return r.rename(oldName, newName, false)
}

func (r *artifactSecureRoot) renameReplace(oldName, newName string) error {
	return r.rename(oldName, newName, true)
}

func (r *artifactSecureRoot) rename(oldName, newName string, replace bool) error {
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
	source, err := artifactNtOpen(
		r.handle,
		oldPath,
		windows.FILE_GENERIC_READ|windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return translateArtifactWindowsError(err)
	}
	defer windows.CloseHandle(source)
	if err := rejectArtifactReparsePoint(source); err != nil {
		return err
	}

	newNameUTF16, err := windows.UTF16FromString(newPath)
	if err != nil {
		return err
	}
	nameBytes := (len(newNameUTF16) - 1) * 2
	var layout artifactFileRenameInformation
	bufferSize := int(unsafe.Offsetof(layout.FileName)) + nameBytes
	buffer := make([]byte, bufferSize)
	info := (*artifactFileRenameInformation)(unsafe.Pointer(&buffer[0]))
	if replace {
		info.ReplaceIfExists = 1
	}
	info.RootDirectory = r.handle
	info.FileNameLength = uint32(nameBytes)
	copy(
		(*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&info.FileName[0]))[:nameBytes/2:nameBytes/2],
		newNameUTF16[:len(newNameUTF16)-1],
	)
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(
		source,
		&iosb,
		&buffer[0],
		uint32(bufferSize),
		windows.FileRenameInformation,
	)
	if !replace && isArtifactAlreadyExists(err) {
		return errors.Join(ErrArtifactPublishConflict, err)
	}
	return translateArtifactWindowsError(err)
}

func (r *artifactSecureRoot) openParent(relativePath string) (windows.Handle, string, error) {
	components, err := artifactPathComponentsWindows(relativePath)
	if err != nil {
		return windows.InvalidHandle, "", err
	}
	if len(components) == 0 {
		return windows.InvalidHandle, "", ErrArtifactInvalidPath
	}
	current, err := duplicateArtifactHandle(r.handle)
	if err != nil {
		return windows.InvalidHandle, "", err
	}
	for _, component := range components[:len(components)-1] {
		next, err := artifactNtOpen(
			current,
			component,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
			windows.FILE_OPEN,
			windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT,
			0,
		)
		if err != nil {
			windows.CloseHandle(current)
			return windows.InvalidHandle, "", translateArtifactWindowsError(err)
		}
		if err := rejectArtifactReparsePoint(next); err != nil {
			windows.CloseHandle(next)
			windows.CloseHandle(current)
			return windows.InvalidHandle, "", err
		}
		windows.CloseHandle(current)
		current = next
	}
	return current, components[len(components)-1], nil
}

func (r *artifactSecureRoot) openDir(relativePath string) (windows.Handle, error) {
	if relativePath == "." {
		return duplicateArtifactHandle(r.handle)
	}
	components, err := artifactPathComponentsWindows(relativePath)
	if err != nil {
		return windows.InvalidHandle, err
	}
	current, err := duplicateArtifactHandle(r.handle)
	if err != nil {
		return windows.InvalidHandle, err
	}
	for _, component := range components {
		next, err := artifactNtOpen(
			current,
			component,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE|windows.SYNCHRONIZE,
			windows.FILE_OPEN,
			windows.FILE_DIRECTORY_FILE|windows.FILE_OPEN_REPARSE_POINT,
			0,
		)
		if err != nil {
			windows.CloseHandle(current)
			return windows.InvalidHandle, translateArtifactWindowsError(err)
		}
		if err := rejectArtifactReparsePoint(next); err != nil {
			windows.CloseHandle(next)
			windows.CloseHandle(current)
			return windows.InvalidHandle, err
		}
		windows.CloseHandle(current)
		current = next
	}
	return current, nil
}

func artifactNtOpen(
	root windows.Handle,
	name string,
	access uint32,
	disposition uint32,
	options uint32,
	attributes uint32,
) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	oa := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: root,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE,
	}
	var handle windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	var allocationSize int64
	err = windows.NtCreateFile(
		&handle,
		access,
		oa,
		&iosb,
		&allocationSize,
		attributes,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		disposition,
		options,
		0,
		0,
	)
	return handle, err
}

func duplicateArtifactHandle(source windows.Handle) (windows.Handle, error) {
	var target windows.Handle
	err := windows.DuplicateHandle(
		windows.CurrentProcess(),
		source,
		windows.CurrentProcess(),
		&target,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	)
	return target, err
}

func rejectArtifactReparsePoint(handle windows.Handle) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errArtifactReparsePoint
	}
	return nil
}

func artifactNTAbsolutePath(absolute string) string {
	if strings.HasPrefix(absolute, `\\`) {
		return `\??\UNC\` + strings.TrimPrefix(absolute, `\\`)
	}
	return `\??\` + absolute
}

func artifactPathComponentsWindows(relativePath string) ([]string, error) {
	if relativePath == "." {
		return nil, nil
	}
	clean, err := validateArtifactRelativePath(relativePath)
	if err != nil {
		return nil, err
	}
	return strings.Split(clean, "/"), nil
}

func translateArtifactWindowsError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

func isArtifactWindowsNotExist(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, windows.ERROR_PATH_NOT_FOUND) ||
		errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) ||
		errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND)
}

func isArtifactAlreadyExists(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) ||
		errors.Is(err, windows.ERROR_FILE_EXISTS) ||
		errors.Is(err, windows.STATUS_OBJECT_NAME_COLLISION) ||
		errors.Is(err, ErrArtifactPublishConflict)
}

func isArtifactNotExist(err error) bool {
	return isArtifactWindowsNotExist(err)
}
