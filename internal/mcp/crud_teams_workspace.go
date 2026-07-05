package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// registerTeamsWorkspaceCRUDTools registers the goclaw_teams_workspace_*
// MCP tools backed by the team workspace directory on disk. Mirrors
// internal/gateway/methods/teams_workspace.go (path resolution, symlink
// escape checks, shared-vs-isolated workspace mode).
func registerTeamsWorkspaceCRUDTools(srv *mcpserver.MCPServer, teams store.TeamStore, cfg *config.Config) {
	srv.AddTool(mcpgo.NewTool("goclaw_teams_workspace_list",
		mcpgo.WithDescription("List files in a team's workspace directory."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Chat ID scope; empty lists shared/root or all chat scopes.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsWorkspaceList(teams, cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_workspace_read",
		mcpgo.WithDescription("Read a file from a team's workspace directory."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Chat ID scope; required unless the team uses a shared workspace.")),
		mcpgo.WithString("file_name", mcpgo.Required(), mcpgo.Description("File name, relative to the workspace scope.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsWorkspaceRead(teams, cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_workspace_delete",
		mcpgo.WithDescription("Delete a file from a team's workspace directory."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Chat ID scope; required unless the team uses a shared workspace.")),
		mcpgo.WithString("file_name", mcpgo.Required(), mcpgo.Description("File name, relative to the workspace scope.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleTeamsWorkspaceDelete(teams, cfg))
}

// teamWorkspaceDir mirrors internal/gateway/methods/teams_workspace.go's
// unexported helper of the same name — duplicated because this MCP surface
// does not depend on internal/gateway/methods (see resolveAgentUUID doc).
// Tenant scoping uses store.TenantIDFromContext/TenantSlugFromContext same
// as the WS surface; MCP callers without an enriched context resolve to the
// master tenant.
func teamWorkspaceDir(ctx context.Context, dataDir string, teamID uuid.UUID, chatID string) string {
	tid := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	base := config.TenantTeamDir(dataDir, tid, slug, teamID)
	if chatID != "" {
		return filepath.Join(base, chatID)
	}
	return base
}

// resolveWorkspacePath mirrors internal/gateway/methods/teams_workspace.go's
// unexported helper of the same name (path traversal / symlink escape guard).
func resolveWorkspacePath(scopeDir, fileName string) (string, error) {
	diskPath := filepath.Clean(filepath.Join(scopeDir, fileName))

	scopeReal, err := filepath.EvalSymlinks(filepath.Clean(scopeDir))
	if err != nil {
		scopeReal = filepath.Clean(scopeDir)
	}

	diskReal, err := filepath.EvalSymlinks(diskPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("security.workspace_path_resolve_failed", "path", fileName, "error", err)
			return "", fmt.Errorf("invalid file_name")
		}
		parentReal, parentErr := filepath.EvalSymlinks(filepath.Dir(diskPath))
		if parentErr != nil {
			return "", fmt.Errorf("invalid file_name")
		}
		diskReal = filepath.Join(parentReal, filepath.Base(diskPath))
	}

	if diskReal != scopeReal && !strings.HasPrefix(diskReal, scopeReal+string(filepath.Separator)) {
		slog.Warn("security.workspace_path_escape", "path", fileName, "resolved", diskReal, "scope", scopeReal)
		return "", fmt.Errorf("invalid file_name")
	}

	return diskPath, nil
}

type teamWorkspaceFileEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ChatID    string `json:"chat_id"`
	IsDir     bool   `json:"is_dir,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func walkTeamWorkspaceDir(baseDir, prefix, chatID string) []teamWorkspaceFileEntry {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	var files []teamWorkspaceFileEntry
	for _, entry := range entries {
		relPath := entry.Name()
		if prefix != "" {
			relPath = prefix + "/" + entry.Name()
		}
		if entry.IsDir() {
			files = append(files, teamWorkspaceFileEntry{
				Name: relPath, Path: filepath.Join(baseDir, entry.Name()), ChatID: chatID, IsDir: true,
			})
			files = append(files, walkTeamWorkspaceDir(filepath.Join(baseDir, entry.Name()), relPath, chatID)...)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, teamWorkspaceFileEntry{
			Name: relPath, Path: filepath.Join(baseDir, entry.Name()), Size: info.Size(), ChatID: chatID,
			UpdatedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return files
}

func handleTeamsWorkspaceList(teams store.TeamStore, cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.workspace.list", err)
		}
		chatID := req.GetString("chat_id", "")
		dataDir := cfg.ResolvedDataDir()

		shared := false
		if team, err := teams.GetTeam(ctx, teamID); err == nil {
			shared = tools.IsSharedWorkspace(team.Settings)
		}

		baseDir := teamWorkspaceDir(ctx, dataDir, teamID, "")
		var files []teamWorkspaceFileEntry

		if shared || chatID != "" {
			scopeDir := baseDir
			scopeChatID := ""
			if !shared && chatID != "" {
				scopeDir = teamWorkspaceDir(ctx, dataDir, teamID, chatID)
				scopeChatID = chatID
			}
			files = walkTeamWorkspaceDir(scopeDir, "", scopeChatID)
		} else {
			entries, err := os.ReadDir(baseDir)
			if err != nil {
				return jsonToolResult(map[string]any{"files": []teamWorkspaceFileEntry{}, "count": 0})
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				cid := entry.Name()
				scopeDir := filepath.Join(baseDir, cid)
				files = append(files, teamWorkspaceFileEntry{Name: cid, Path: scopeDir, ChatID: cid, IsDir: true})
				files = append(files, walkTeamWorkspaceDir(scopeDir, cid, cid)...)
			}
		}
		if files == nil {
			files = []teamWorkspaceFileEntry{}
		}
		return jsonToolResult(map[string]any{"files": files, "count": len(files)})
	}
}

func handleTeamsWorkspaceRead(teams store.TeamStore, cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.workspace.read", err)
		}
		fileName, err := req.RequireString("file_name")
		if err != nil {
			return toolError("teams.workspace.read", err)
		}
		if strings.Contains(fileName, "..") || strings.Contains(fileName, "\\") {
			return mcpgo.NewToolResultError("teams.workspace.read: invalid file_name"), nil
		}

		chatID := req.GetString("chat_id", "")
		if team, err := teams.GetTeam(ctx, teamID); err == nil && tools.IsSharedWorkspace(team.Settings) {
			chatID = ""
		} else if chatID == "" {
			return mcpgo.NewToolResultError("teams.workspace.read: chat_id is required"), nil
		}

		scopeDir := teamWorkspaceDir(ctx, cfg.ResolvedDataDir(), teamID, chatID)
		diskPath, pathErr := resolveWorkspacePath(scopeDir, fileName)
		if pathErr != nil {
			return toolError("teams.workspace.read", pathErr)
		}
		data, err := os.ReadFile(diskPath)
		if err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("teams.workspace.read: file not found: %s", fileName)), nil
		}

		const maxContentLen = 500000
		content := string(data)
		if len(content) > maxContentLen {
			content = content[:maxContentLen] + "\n\n[...truncated]"
		}

		info, _ := os.Stat(diskPath)
		file := teamWorkspaceFileEntry{Name: fileName, Path: diskPath, Size: int64(len(data)), ChatID: chatID}
		if info != nil {
			file.UpdatedAt = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
		return jsonToolResult(map[string]any{"file": file, "content": content})
	}
}

func handleTeamsWorkspaceDelete(teams store.TeamStore, cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.workspace.delete", err)
		}
		fileName, err := req.RequireString("file_name")
		if err != nil {
			return toolError("teams.workspace.delete", err)
		}
		if strings.Contains(fileName, "..") || strings.Contains(fileName, "\\") {
			return mcpgo.NewToolResultError("teams.workspace.delete: invalid file_name"), nil
		}

		chatID := req.GetString("chat_id", "")
		if team, err := teams.GetTeam(ctx, teamID); err == nil && tools.IsSharedWorkspace(team.Settings) {
			chatID = ""
		} else if chatID == "" {
			return mcpgo.NewToolResultError("teams.workspace.delete: chat_id is required"), nil
		}

		scopeDir := teamWorkspaceDir(ctx, cfg.ResolvedDataDir(), teamID, chatID)
		diskPath, pathErr := resolveWorkspacePath(scopeDir, fileName)
		if pathErr != nil {
			return toolError("teams.workspace.delete", pathErr)
		}
		if err := os.Remove(diskPath); err != nil {
			return mcpgo.NewToolResultError(fmt.Sprintf("teams.workspace.delete: file not found: %s", fileName)), nil
		}
		return jsonToolResult(map[string]string{"deleted": fileName})
	}
}
