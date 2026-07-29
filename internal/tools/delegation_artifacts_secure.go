package tools

import (
	"errors"
	"os"
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
