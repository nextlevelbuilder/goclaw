package tools

import (
	"errors"
	"io/fs"
	"os"
)

const (
	artifactSecureReadBatchSize       = 64
	artifactSecureMaxDirectoryEntries = 4096
	artifactSecureMaxDepth            = 64
	artifactSecureCleanupEntryBudget  = 4096
)

var (
	errArtifactSymlink      = errors.New("symlink rejected")
	errArtifactReparsePoint = errors.New("reparse point rejected")
)

type artifactEntryKind uint8

const (
	artifactEntryOther artifactEntryKind = iota
	artifactEntryRegular
	artifactEntryDirectory
)

type artifactSecureEntry struct {
	file  *os.File
	kind  artifactEntryKind
	size  int64
	links uint64
}

func (e *artifactSecureEntry) close() error {
	if e == nil || e.file == nil {
		return nil
	}
	return e.file.Close()
}

func (e *artifactSecureEntry) readDirBatch() ([]fs.DirEntry, error) {
	if e == nil || e.file == nil || e.kind != artifactEntryDirectory {
		return nil, ErrArtifactNonRegular
	}
	return e.file.ReadDir(artifactSecureReadBatchSize)
}
