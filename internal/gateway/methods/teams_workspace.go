package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// RegisterWorkspace adds workspace RPC handlers to the method router.
func (m *TeamsMethods) RegisterWorkspace(router *gateway.MethodRouter) {
	router.Register(protocol.MethodTeamsWorkspaceList, m.handleWorkspaceList)
	router.Register(protocol.MethodTeamsWorkspaceRead, m.handleWorkspaceRead)
	router.Register(protocol.MethodTeamsWorkspaceDelete, m.handleWorkspaceDelete)
}

// teamWorkspaceDir returns the base directory for a team's workspace files.
// Pattern: {dataDir}/teams/{teamID}/
// If chatID is provided, scopes to {dataDir}/teams/{teamID}/{chatID}/
func teamWorkspaceDir(dataDir string, teamID uuid.UUID, chatID string) string {
	if chatID != "" {
		return filepath.Join(dataDir, "teams", teamID.String(), chatID)
	}
	return filepath.Join(dataDir, "teams", teamID.String())
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

	baseDir := teamWorkspaceDir(m.dataDir, teamID, "")
	var files []workspaceFileEntry

	if params.ChatID != "" {
		// Scoped: list files in specific chatID directory.
		scopeDir := teamWorkspaceDir(m.dataDir, teamID, params.ChatID)
		files = listDirFiles(scopeDir, params.ChatID)
	} else {
		// Unscoped: list all chatID subdirectories and their files.
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
			files = append(files, listDirFiles(scopeDir, chatID)...)
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

// listDirFiles lists non-directory files in a directory, returning workspaceFileEntry slice.
func listDirFiles(dir, chatID string) []workspaceFileEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []workspaceFileEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, workspaceFileEntry{
			Name:      entry.Name(),
			Path:      filepath.Join(dir, entry.Name()),
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
	// Security: reject path traversal.
	if strings.Contains(params.FileName, "..") || strings.ContainsAny(params.FileName, "/\\") {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid file_name"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	chatID := params.ChatID
	if chatID == "" {
		chatID = "_default"
	}

	diskPath := filepath.Join(teamWorkspaceDir(m.dataDir, teamID, chatID), params.FileName)
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
	// Security: reject path traversal.
	if strings.Contains(params.FileName, "..") || strings.ContainsAny(params.FileName, "/\\") {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid file_name"))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	chatID := params.ChatID
	if chatID == "" {
		chatID = "_default"
	}

	diskPath := filepath.Join(teamWorkspaceDir(m.dataDir, teamID, chatID), params.FileName)
	if err := os.Remove(diskPath); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, fmt.Sprintf("file not found: %s", params.FileName)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"deleted": params.FileName,
	}))
}
