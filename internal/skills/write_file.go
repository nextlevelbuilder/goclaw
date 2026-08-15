package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Errors returned by WriteVersionedFile, mapped by callers (HTTP handler,
// MCP tool) to their own status codes.
var (
	ErrSkillFileNotFound = errors.New("skill file not found")
	ErrSkillIsSystem     = errors.New("cannot edit a system skill")
	ErrSkillInvalidPath  = errors.New("invalid file path")
)

// WriteVersionedFile writes relPath's content into a new immutable version of
// a managed (non-system) skill: it copies the current version directory,
// writes the file into the copy, atomically renames it into place, and
// repoints the skill's DB row at the new version. Historical versions remain
// immutable. Shared by internal/http's skill-editor endpoint
// (SkillsHandler.handleWriteFile) and the goclaw_skills_write_file MCP tool
// so both surfaces apply identical validation and versioning.
func WriteVersionedFile(ctx context.Context, manage store.SkillManageStore, tenantSkillsDir string, id uuid.UUID, relPath, content string) (path string, version int, err error) {
	if strings.Contains(relPath, "..") {
		return "", 0, ErrSkillInvalidPath
	}

	filePath, slug, currentVersion, isSystem, ok := manage.GetSkillFilePath(ctx, id)
	if !ok {
		return "", 0, ErrSkillFileNotFound
	}
	if isSystem {
		return "", 0, ErrSkillIsSystem
	}

	slugDir := store.SkillSlugDir(filePath)
	if slugDir == "" {
		return "", 0, ErrSkillFileNotFound
	}
	currentDir := filepath.Join(slugDir, strconv.Itoa(currentVersion))
	if info, statErr := os.Stat(currentDir); statErr != nil || !info.IsDir() {
		return "", 0, ErrSkillFileNotFound
	}

	cleanRelPath := filepath.Clean(relPath)
	// Validate the path against the CURRENT version directory before staging
	// a copy — cheaper failure path and keeps the escape/symlink checks close
	// to the original request path.
	checkPath := filepath.Join(currentDir, cleanRelPath)
	if !strings.HasPrefix(checkPath, currentDir+string(filepath.Separator)) {
		return "", 0, ErrSkillInvalidPath
	}
	if fi, lstatErr := os.Lstat(checkPath); lstatErr == nil {
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			return "", 0, ErrSkillInvalidPath
		}
	}
	if IsSystemArtifact(filepath.Base(cleanRelPath)) {
		return "", 0, ErrSkillInvalidPath
	}

	// Create a new immutable version: lock the next version number, stage a
	// copy of the current version directory, write the edited file into the
	// staged copy, then atomically rename it into place and repoint the
	// skill's DB row.
	newVersion, commitLock, err := manage.GetNextVersionLocked(ctx, slug)
	if err != nil {
		return "", 0, err
	}
	defer commitLock() //nolint:errcheck

	destDir := filepath.Join(tenantSkillsDir, slug, strconv.Itoa(newVersion))
	tmpDir := destDir + ".tmp-" + uuid.NewString()
	if err := CopyDir(currentDir, tmpDir); err != nil {
		return "", 0, err
	}
	removeDestOnError := true
	defer func() {
		_ = os.RemoveAll(tmpDir)
		if removeDestOnError {
			_ = os.RemoveAll(destDir)
		}
	}()

	absPath := filepath.Join(tmpDir, cleanRelPath)
	if !strings.HasPrefix(absPath, tmpDir+string(filepath.Separator)) {
		return "", 0, ErrSkillInvalidPath
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", 0, err
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpDir, destDir); err != nil {
		return "", 0, err
	}

	hash, size, err := HashDir(destDir)
	if err != nil {
		return "", 0, err
	}
	if err := manage.UpdateSkill(ctx, id, map[string]any{
		"version":    newVersion,
		"file_path":  destDir,
		"file_size":  size,
		"file_hash":  &hash,
		"updated_at": time.Now(),
	}); err != nil {
		return "", 0, err
	}
	removeDestOnError = false

	manage.BumpVersion()
	return relPath, newVersion, nil
}
