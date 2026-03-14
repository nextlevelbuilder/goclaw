package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Workspace limits shared across read/write tools.
const defaultQuotaMB = 500

// workspaceDir returns the disk directory for a team workspace scope.
// Pattern: {dataDir}/teams/{teamID}/{chatID}/
// chatID is the system-derived userID (stable across WS reconnects).
// Creates directory with 0750 if not exists.
func workspaceDir(dataDir string, teamID uuid.UUID, _, chatID string) (string, error) {
	if chatID == "" {
		chatID = "_default"
	}
	dir := filepath.Join(dataDir, "teams", teamID.String(), chatID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("failed to create workspace dir: %w", err)
	}
	return dir, nil
}

// validFileName matches alphanumeric + "-_." only.
var validFileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,253}[a-zA-Z0-9._]$`)

// sanitizeFileName validates file name: max 255 chars, no path separators,
// no null bytes, no "..", alphanumeric + "-_." only.
func sanitizeFileName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("file_name is required")
	}
	if len(name) > 255 {
		return "", fmt.Errorf("file_name exceeds 255 characters")
	}
	if strings.Contains(name, "\x00") {
		return "", fmt.Errorf("file_name contains null bytes")
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("file_name contains path traversal")
	}
	if strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("file_name contains path separators")
	}
	// Allow single-char names like "a" or "1"
	if len(name) == 1 {
		if (name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= '0' && name[0] <= '9') {
			return name, nil
		}
		return "", fmt.Errorf("file_name must be alphanumeric with hyphens, underscores, and dots only")
	}
	if !validFileName.MatchString(name) {
		return "", fmt.Errorf("file_name must be alphanumeric with hyphens, underscores, and dots only")
	}
	return name, nil
}

// blockedExtensions lists executable file types that are not allowed.
var blockedExtensions = map[string]bool{
	".exe": true, ".sh": true, ".bat": true, ".cmd": true,
	".ps1": true, ".com": true, ".msi": true, ".scr": true,
}

// mimeTypes maps file extensions to MIME types (package-level to avoid re-allocation).
var mimeTypes = map[string]string{
	".txt":  "text/plain",
	".md":   "text/markdown",
	".json": "application/json",
	".csv":  "text/csv",
	".xml":  "text/xml",
	".html": "text/html",
	".css":  "text/css",
	".js":   "application/javascript",
	".ts":   "application/typescript",
	".py":   "text/x-python",
	".go":   "text/x-go",
	".rs":   "text/x-rust",
	".java": "text/x-java",
	".yaml": "application/x-yaml",
	".yml":  "application/x-yaml",
	".toml": "application/toml",
	".sql":  "application/sql",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".svg":  "image/svg+xml",
	".pdf":  "application/pdf",
	".zip":  "application/zip",
	".tar":  "application/x-tar",
	".gz":   "application/gzip",
}

// inferMimeType returns MIME type from file extension.
// Blocks executable types.
func inferMimeType(fileName string) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if blockedExtensions[ext] {
		return "", fmt.Errorf("executable file type %q is not allowed", ext)
	}
	if mt, ok := mimeTypes[ext]; ok {
		return mt, nil
	}
	return "application/octet-stream", nil
}

// isBinaryMime returns true if the MIME type represents binary content.
func isBinaryMime(mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return false
	}
	switch mimeType {
	case "application/json", "application/javascript", "application/typescript",
		"application/x-yaml", "application/toml", "application/sql",
		"application/xml":
		return false
	}
	return true
}

// resolveWorkspaceScope resolves the workspace scope (channel, chatID) for tools.
// Scope is (team_id, userID) where userID is the system-derived stable user ID.
// channel is always "" (kept for signature compatibility); chatID = userID.
// Priority: WorkspaceChatID context key (set during delegation) > store.UserIDFromContext
func resolveWorkspaceScope(ctx context.Context) (channel, chatID string) {
	chatID = WorkspaceChatIDFromCtx(ctx)
	if chatID == "" {
		chatID = store.UserIDFromContext(ctx)
	}
	return "", chatID
}

// workspaceSettings parses workspace-related fields from team settings JSON once.
type workspaceSettings struct {
	WorkspaceScope     string           `json:"workspace_scope"`
	WorkspaceQuotaMB   *int             `json:"workspace_quota_mb"`
	WorkspaceTemplates []workspaceTempl `json:"workspace_templates"`
}

type workspaceTempl struct {
	FileName string `json:"file_name"`
	Content  string `json:"content"`
}

func parseWorkspaceSettings(raw json.RawMessage) workspaceSettings {
	var ws workspaceSettings
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &ws)
	}
	return ws
}

func (ws workspaceSettings) isShared() bool {
	return ws.WorkspaceScope == "shared"
}

func (ws workspaceSettings) quotaMB(defaultMB int) int {
	if ws.WorkspaceQuotaMB != nil {
		return *ws.WorkspaceQuotaMB
	}
	return defaultMB
}

// resolveWorkspaceScopeFromArgs resolves scope (channel, chatID) from tool args + context.
// Returns error result string if scope=team but team setting is isolated.
func resolveWorkspaceScopeFromArgs(ctx context.Context, args map[string]any, ws workspaceSettings) (channel, chatID, errMsg string) {
	scope, _ := args["scope"].(string)
	if scope == "" {
		scope = "channel"
	}
	channel, chatID = resolveWorkspaceScope(ctx)
	if scope == "team" {
		if !ws.isShared() {
			return "", "", "team workspace_scope is 'isolated' — retry with scope='channel' (or omit scope) to use your per-user workspace. To enable shared team workspace, ask an admin to set workspace_scope='shared' in team settings."
		}
		chatID = ""
	}
	return "", chatID, ""
}

// validTags is the set of allowed tag values.
var validTags = map[string]bool{
	"deliverable": true,
	"handoff":     true,
	"reference":   true,
	"draft":       true,
}
