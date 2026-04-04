package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// allowedAgentFiles is the list of files exposed via agents.files.* RPCs.
// TOOLS.md excluded — not applicable.
var allowedAgentFiles = []string{
	bootstrap.AgentsFile, bootstrap.SoulFile, bootstrap.IdentityFile,
	bootstrap.UserFile, bootstrap.UserPredefinedFile, bootstrap.BootstrapFile, bootstrap.MemoryJSONFile,
	bootstrap.HeartbeatFile,
}

// --- agents.files.list ---
// Matching TS src/gateway/server-methods/agents.ts:399-422

func (m *AgentsMethods) handleFilesList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params agentParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}

	if m.agentStore != nil {
		// --- DB-backed: list from store ---

		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
			return
		}

		dbFiles, err := m.agentStore.GetAgentContextFiles(ctx, ag.ID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "files")))
			return
		}

		// Build a map for quick lookup
		dbMap := make(map[string]store.AgentContextFileData, len(dbFiles))
		for _, f := range dbFiles {
			dbMap[f.FileName] = f
		}

		files := make([]map[string]any, 0, len(allowedAgentFiles))
		for _, name := range allowedAgentFiles {
			if f, ok := dbMap[name]; ok {
				files = append(files, map[string]any{
					"name":    name,
					"missing": false,
					"size":    len(f.Content),
				})
			} else {
				files = append(files, map[string]any{
					"name":    name,
					"missing": true,
				})
			}
		}

		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"agentId": params.AgentID,
			"files":   files,
		}))
		return
	}
}

// --- agents.files.get ---
// Matching TS src/gateway/server-methods/agents.ts:423-473

func (m *AgentsMethods) handleFilesGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
		Name    string `json:"name"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}
	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if !isAllowedFile(params.Name) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "file not allowed: "+params.Name)))
		return
	}

	if m.agentStore != nil {
		// --- DB-backed: read from store ---

		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
			return
		}

		dbFiles, err := m.agentStore.GetAgentContextFiles(ctx, ag.ID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "files")))
			return
		}

		for _, f := range dbFiles {
			if f.FileName == params.Name {
				client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
					"agentId": params.AgentID,
					"file": map[string]any{
						"name":    params.Name,
						"missing": false,
						"size":    len(f.Content),
						"content": f.Content,
					},
				}))
				return
			}
		}

		// File not found in DB
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"agentId": params.AgentID,
			"file": map[string]any{
				"name":    params.Name,
				"missing": true,
			},
		}))
		return
	}
}

// --- agents.files.set ---
// Matching TS src/gateway/server-methods/agents.ts:474-515

func (m *AgentsMethods) handleFilesSet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID   string `json:"agentId"`
		Name      string `json:"name"`
		Content   string `json:"content"`
		Propagate bool   `json:"propagate"` // push change to all existing user instances
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		params.AgentID = "default"
	}
	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if !isAllowedFile(params.Name) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "file not allowed: "+params.Name)))
		return
	}

	if m.agentStore != nil {
		// --- DB-backed: write to store ---

		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
			return
		}

		if err := m.agentStore.SetAgentContextFile(ctx, ag.ID, params.Name, params.Content); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "file", err.Error())))
			return
		}

		// Propagate to all existing user instances if requested (#294).
		var propagated int
		if params.Propagate {
			n, err := m.agentStore.PropagateContextFile(ctx, ag.ID, params.Name)
			if err != nil {
				slog.Warn("agents.files.set: propagation failed", "agent", params.AgentID, "file", params.Name, "error", err)
			} else {
				propagated = n
			}
		}

		// Invalidate both caches so the new content is served immediately
		// without waiting for the ContextFileInterceptor's 5-minute TTL to expire.
		m.agents.InvalidateAgent(params.AgentID)
		if m.interceptor != nil {
			m.interceptor.InvalidateAgent(ag.ID)
		}

		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"agentId": params.AgentID,
			"file": map[string]any{
				"name":    params.Name,
				"missing": false,
				"size":    len(params.Content),
				"content": params.Content,
			},
			"propagated": propagated,
		}))
		return
	}
}

// --- Helpers ---

func isAllowedFile(name string) bool {
	return slices.Contains(allowedAgentFiles, name)
}
