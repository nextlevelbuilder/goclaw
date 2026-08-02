package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Workspace limits shared across workspace interceptor and HTTP upload handlers.
const (
	maxFileSizeBytes = 50 * 1024 * 1024 // 50MB
	maxFilesPerScope = 100

	// MaxFileSizeBytes is the exported form of maxFileSizeBytes for HTTP handlers.
	MaxFileSizeBytes int64 = maxFileSizeBytes
	// MaxFilesPerScope is the exported form of maxFilesPerScope for HTTP handlers.
	MaxFilesPerScope = maxFilesPerScope
)

// scopeQuotaWindow is the age window the file quota applies over. The quota
// exists to stop a looping agent from flooding a shared workspace, and a burst
// like that happens within minutes — counting a scope's ENTIRE history instead
// punished teams for working a long time: a real team workspace here reached
// 103 files accumulated since April and write_file started failing with
// "workspace file limit reached (103/100)" during ordinary work, while 48 files
// in subdirectories were never counted at all. Counting only recent writes keeps
// the burst protection (100 new files in 7 days is far past any real pace) and
// removes the permanent ceiling.
const scopeQuotaWindow = 7 * 24 * time.Hour

// CountRecentScopeFiles returns how many files at the top level of dir were
// modified within scopeQuotaWindow. Directories are skipped, matching the
// original quota's shape. An unreadable entry is counted as recent: failing
// closed on a stat error is safer than letting an unbounded loop through, and a
// caller that cannot read the directory at all gets 0 and proceeds, which is the
// pre-existing fail-open behaviour for a missing scope dir.
func CountRecentScopeFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-scopeQuotaWindow)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.ModTime().Before(cutoff) {
			count++
		}
	}
	return count
}

// WorkspaceDir returns the disk directory for a team workspace scope.
// - chatID="" → team root: {baseDir}/teams/{teamID}/         (shared mode)
// - chatID="x" → per-chat: {baseDir}/teams/{teamID}/{chatID}/ (isolated mode)
// baseDir should already be tenant-scoped (via config.TenantDataDir for non-master tenants).
// Creates directory with 0750 if not exists.
func WorkspaceDir(baseDir string, teamID uuid.UUID, chatID string) (string, error) {
	dir := filepath.Join(baseDir, "teams", teamID.String())
	if chatID != "" {
		dir = filepath.Join(dir, chatID)
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("failed to create workspace dir: %w", err)
	}
	return dir, nil
}

// IsSharedWorkspace returns true if the team's workspace_scope setting is "shared".
// Default (unset or "isolated") returns false.
func IsSharedWorkspace(settings json.RawMessage) bool {
	if settings == nil {
		return false
	}
	var s struct {
		WorkspaceScope string `json:"workspace_scope"`
	}
	if json.Unmarshal(settings, &s) != nil {
		return false
	}
	return s.WorkspaceScope == "shared"
}

// blockedExtensions lists executable file types that are not allowed in team workspaces.
var blockedExtensions = map[string]bool{
	".exe": true, ".sh": true, ".bat": true, ".cmd": true,
	".ps1": true, ".com": true, ".msi": true, ".scr": true,
}

// IsBlockedExtension returns true if the file extension is blocked for upload.
func IsBlockedExtension(ext string) bool {
	return blockedExtensions[ext]
}
