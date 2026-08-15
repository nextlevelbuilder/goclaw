//go:build !linux && !darwin && !windows

package tools

import (
	"io/fs"
	"os"
)

type artifactSecureRoot struct{}

func openArtifactSecureRoot(string) (*artifactSecureRoot, error) {
	return nil, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) close() error { return nil }

func (r *artifactSecureRoot) mkdirAll(string, fs.FileMode) error {
	return ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) createSubroot(string, fs.FileMode) (*artifactSecureRoot, error) {
	return nil, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) openSubroot(string) (*artifactSecureRoot, error) {
	return nil, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) createFile(string, fs.FileMode) (*os.File, error) {
	return nil, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) openEntry(string) (*artifactSecureEntry, error) {
	return nil, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) readDir(string) ([]string, error) {
	return nil, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) exists(string) (bool, error) {
	return false, ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) syncDir(string) error {
	return ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) removeTree(string) error {
	return ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) renameNoReplace(string, string) error {
	return ErrArtifactSecureUnavailable
}

func (r *artifactSecureRoot) renameReplace(string, string) error {
	return ErrArtifactSecureUnavailable
}

func isArtifactAlreadyExists(error) bool { return false }
func isArtifactNotExist(error) bool      { return false }
