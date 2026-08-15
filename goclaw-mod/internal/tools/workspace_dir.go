package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
