package methods

import (
	"context"
	"encoding/json"
	"os"

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

// --- Workspace List ---

type workspaceListParams struct {
	TeamID  string `json:"team_id"`
	Channel string `json:"channel"`
	ChatID  string `json:"chat_id"`
}

func (m *TeamsMethods) handleWorkspaceList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

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

	files, err := m.teamStore.ListWorkspaceFiles(ctx, teamID, params.Channel, params.ChatID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Check disk existence for each file.
	type fileWithStatus struct {
		store.TeamWorkspaceFileData
		Missing bool `json:"missing,omitempty"`
	}
	var result []fileWithStatus
	for _, f := range files {
		fws := fileWithStatus{TeamWorkspaceFileData: f}
		if _, statErr := os.Stat(f.FilePath); os.IsNotExist(statErr) {
			fws.Missing = true
		}
		result = append(result, fws)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"files": result,
		"count": len(result),
	}))
}

// --- Workspace Read ---

type workspaceReadParams struct {
	TeamID   string `json:"team_id"`
	Channel  string `json:"channel"`
	ChatID   string `json:"chat_id"`
	FileName string `json:"file_name"`
}

func (m *TeamsMethods) handleWorkspaceRead(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params workspaceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" || params.FileName == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "team_id, file_name")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	file, err := m.teamStore.GetWorkspaceFile(ctx, teamID, params.Channel, params.ChatID, params.FileName)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	// Read content from disk.
	var content string
	data, readErr := os.ReadFile(file.FilePath)
	if readErr == nil {
		content = string(data)
		if len(content) > 500000 {
			content = content[:500000] + "\n\n[...truncated]"
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"file":    file,
		"content": content,
	}))
}

// --- Workspace Delete ---

type workspaceDeleteParams struct {
	TeamID   string `json:"team_id"`
	Channel  string `json:"channel"`
	ChatID   string `json:"chat_id"`
	FileName string `json:"file_name"`
}

func (m *TeamsMethods) handleWorkspaceDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params workspaceDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TeamID == "" || params.FileName == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "team_id, file_name")))
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid team_id"))
		return
	}

	filePath, err := m.teamStore.DeleteWorkspaceFile(ctx, teamID, params.Channel, params.ChatID, params.FileName)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	// Clean up disk file.
	_ = os.Remove(filePath)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"deleted": params.FileName,
	}))
}
