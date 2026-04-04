package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// RegisterWorkspace adds workspace RPC handlers to the method router.
func (m *TeamsMethods) RegisterWorkspace(router *gateway.MethodRouter) {
	router.Register(protocol.MethodTeamsWorkspaceList, m.handleWorkspaceList)
	router.Register(protocol.MethodTeamsWorkspaceRead, m.handleWorkspaceRead)
	router.Register(protocol.MethodTeamsWorkspaceDelete, m.handleWorkspaceDelete)
}

// resolveWorkspacePath resolves fileName within scopeDir and validates that the
// canonical path (after symlink resolution) stays inside the scope directory.
// Returns the resolved disk path or an error if the path escapes the boundary.
func resolveWorkspacePath(scopeDir, fileName string) (string, error) {
	diskPath := filepath.Clean(filepath.Join(scopeDir, fileName))

	// Resolve scope dir to canonical form.
	scopeReal, err := filepath.EvalSymlinks(filepath.Clean(scopeDir))
	if err != nil {
		scopeReal = filepath.Clean(scopeDir)
	}

	// Resolve target path — follow symlinks to detect escapes.
	diskReal, err := filepath.EvalSymlinks(diskPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("security.workspace_path_resolve_failed", "path", fileName, "error", err)
			return "", fmt.Errorf("invalid file_name")
		}
		// File doesn't exist yet — validate parent directory.
		parentReal, parentErr := filepath.EvalSymlinks(filepath.Dir(diskPath))
		if parentErr != nil {
			return "", fmt.Errorf("invalid file_name")
		}
		diskReal = filepath.Join(parentReal, filepath.Base(diskPath))
	}

	// Boundary check: canonical path must be inside canonical scope.
	if diskReal != scopeReal && !strings.HasPrefix(diskReal, scopeReal+string(filepath.Separator)) {
		slog.Warn("security.workspace_path_escape", "path", fileName, "resolved", diskReal, "scope", scopeReal)
		return "", fmt.Errorf("invalid file_name")
	}

	return diskPath, nil
}

// teamWorkspaceDir returns the base directory for a team's workspace files.
// Scoped to tenant via config.TenantTeamDir: {dataDir}/tenants/{slug}/teams/{teamID}/
// Master tenant: {dataDir}/teams/{teamID}/ (backward compat).
// If chatID is provided, further scopes to .../{chatID}/
func teamWorkspaceDir(ctx context.Context, dataDir string, teamID uuid.UUID, chatID string) string {
	tid := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	base := config.TenantTeamDir(dataDir, tid, slug, teamID)
	if chatID != "" {
		return filepath.Join(base, chatID)
	}
	return base
}

// workspaceFileEntry is the response shape for workspace file listing.
type workspaceFileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ChatID    string `json:"chat_id"`
	IsDir     bool   `json:"is_dir,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// --- Workspace List ---

type workspaceListParams struct {
	TeamID string `json:"team_id"`
	ChatID string `json:"chat_id"`
}

func (m *TeamsMethods) handleWorkspaceList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params workspaceListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "team_id")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	// Check if team uses shared workspace (no chatID scoping).
	shared := false
	if team, err := m.teamStore.GetTeam(ctx, teamID); err == nil {
		shared = tools.IsSharedWorkspace(team.Settings)
	}

	baseDir := teamWorkspaceDir(ctx, m.dataDir, teamID, "")
	var files []workspaceFileEntry

	if shared || params.ChatID != "" {
		// Shared mode: list team root directly. Scoped mode: list specific chatID.
		scopeDir := baseDir
		scopeChatID := ""
		if !shared && params.ChatID != "" {
			scopeDir = teamWorkspaceDir(ctx, m.dataDir, teamID, params.ChatID)
			scopeChatID = params.ChatID
		}
		files = walkDir(scopeDir, "", scopeChatID)
	} else {
		// Isolated + unscoped: list all chatID subdirectories with chatID as top-level folder.
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			// Directory doesn't exist = empty workspace.
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
				"files": []workspaceFileEntry{},
				"count": 0,
			}))
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			chatID := entry.Name()
			scopeDir := filepath.Join(baseDir, chatID)
			// Add the chatID directory itself as a top-level entry.
			files = append(files, workspaceFileEntry{
				Name:   chatID,
				Path:   scopeDir,
				ChatID: chatID,
				IsDir:  true,
			})
			// Prefix children with chatID so the tree nests under it.
			files = append(files, walkDir(scopeDir, chatID, chatID)...)
		}
	}

	if files == nil {
		files = []workspaceFileEntry{}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"files": files,
		"count": len(files),
	}))
}

// walkDir recursively lists files and directories, returning workspaceFileEntry slice
// with relative paths. prefix is the relative path prefix for nested entries.
func walkDir(baseDir, prefix, chatID string) []workspaceFileEntry {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	var files []workspaceFileEntry
	for _, entry := range entries {
		relPath := entry.Name()
		if prefix != "" {
			relPath = prefix + "/" + entry.Name()
		}
		if entry.IsDir() {
			files = append(files, workspaceFileEntry{
				Name:   relPath,
				Path:   filepath.Join(baseDir, entry.Name()),
				ChatID: chatID,
				IsDir:  true,
			})
			// Recurse into subdirectory.
			files = append(files, walkDir(filepath.Join(baseDir, entry.Name()), relPath, chatID)...)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, workspaceFileEntry{
			Name:      relPath,
			Path:      filepath.Join(baseDir, entry.Name()),
			Size:      info.Size(),
			ChatID:    chatID,
			UpdatedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return files
}

// --- Workspace Read ---

type workspaceReadParams struct {
	TeamID   string `json:"team_id"`
	ChatID   string `json:"chat_id"`
	FileName string `json:"file_name"`
}

func (m *TeamsMethods) handleWorkspaceRead(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params workspaceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" || params.FileName == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "team_id, file_name")))
		return
	}
	// Security: reject path traversal (allow "/" for nested paths, reject "\" and "..").
	if strings.Contains(params.FileName, "..") || strings.Contains(params.FileName, "\\") {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid file_name"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	// Shared workspace: read from team root. Isolated: require chatID.
	chatID := params.ChatID
	if team, err := m.teamStore.GetTeam(ctx, teamID); err == nil && tools.IsSharedWorkspace(team.Settings) {
		chatID = ""
	} else if chatID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "chat_id")))
		return
	}

	scopeDir := teamWorkspaceDir(ctx, m.dataDir, teamID, chatID)
	diskPath, pathErr := resolveWorkspacePath(scopeDir, params.FileName)
	if pathErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, pathErr.Error()))
		return
	}
	data, err := os.ReadFile(diskPath)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, fmt.Sprintf("file not found: %s", params.FileName)))
		return
	}

	content := string(data)
	if len(content) > 500000 {
		content = content[:500000] + "\n\n[...truncated]"
	}

	info, _ := os.Stat(diskPath)
	file := workspaceFileEntry{
		Name:   params.FileName,
		Path:   diskPath,
		Size:   int64(len(data)),
		ChatID: chatID,
	}
	if info != nil {
		file.UpdatedAt = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"file":    file,
		"content": content,
	}))
}

// --- Workspace Delete ---

type workspaceDeleteParams struct {
	TeamID   string `json:"team_id"`
	ChatID   string `json:"chat_id"`
	FileName string `json:"file_name"`
}

func (m *TeamsMethods) handleWorkspaceDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params workspaceDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" || params.FileName == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "team_id, file_name")))
		return
	}
	// Security: reject path traversal (allow "/" for nested paths, reject "\" and "..").
	if strings.Contains(params.FileName, "..") || strings.Contains(params.FileName, "\\") {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid file_name"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	// Shared workspace: delete from team root. Isolated: require chatID.
	chatID := params.ChatID
	if team, err := m.teamStore.GetTeam(ctx, teamID); err == nil && tools.IsSharedWorkspace(team.Settings) {
		chatID = ""
	} else if chatID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "chat_id")))
		return
	}

	scopeDir := teamWorkspaceDir(ctx, m.dataDir, teamID, chatID)
	diskPath, pathErr := resolveWorkspacePath(scopeDir, params.FileName)
	if pathErr != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, pathErr.Error()))
		return
	}
	if err := os.Remove(diskPath); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, fmt.Sprintf("file not found: %s", params.FileName)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"deleted": params.FileName,
	}))
}
