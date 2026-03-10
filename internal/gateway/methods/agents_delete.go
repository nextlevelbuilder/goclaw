package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- agents.delete ---
// Matching TS src/gateway/server-methods/agents.ts:347-398

func (m *AgentsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID     string `json:"agentId"`
		DeleteFiles bool   `json:"deleteFiles"`
	}
	params.DeleteFiles = true // default
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}
	if params.AgentID == "default" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCannotDeleteDefault)))
		return
	}

	var removedBindings int

	if m.agentStore != nil {
		// --- DB-backed: delete from store ---
		ctx := context.Background()
		ag, err := m.agentStore.GetByKey(ctx, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
			return
		}

		if err := m.agentStore.Delete(ctx, ag.ID); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "agent", fmt.Sprintf("%v", err))))
			return
		}

		m.agents.InvalidateAgent(params.AgentID)
		m.agents.Remove(params.AgentID)

		// Best-effort delete workspace
		if params.DeleteFiles && ag.Workspace != "" {
			os.RemoveAll(ag.Workspace)
		}
	} else {
		// --- Fallback: config.json ---
		spec, ok := m.cfg.Agents.List[params.AgentID]
		if !ok {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgAgentNotFound, params.AgentID)))
			return
		}

		delete(m.cfg.Agents.List, params.AgentID)

		var kept []config.AgentBinding
		for _, b := range m.cfg.Bindings {
			if b.AgentID == params.AgentID {
				removedBindings++
			} else {
				kept = append(kept, b)
			}
		}
		m.cfg.Bindings = kept

		m.agents.Remove(params.AgentID)

		if err := config.Save(m.cfgPath, m.cfg); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToSave, "config", err.Error())))
			return
		}

		if params.DeleteFiles && spec.Workspace != "" {
			ws := config.ExpandHome(spec.Workspace)
			os.RemoveAll(ws)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":              true,
		"agentId":         params.AgentID,
		"removedBindings": removedBindings,
	}))
}
