package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ManagedToolsMethods handles managed-tools.list, managed-tools.get.
type ManagedToolsMethods struct {
	store store.ManagedToolStore
}

func NewManagedToolsMethods(s store.ManagedToolStore) *ManagedToolsMethods {
	return &ManagedToolsMethods{store: s}
}

func (m *ManagedToolsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodManagedToolsList, m.handleList)
	router.Register(protocol.MethodManagedToolsGet, m.handleGet)
}

func (m *ManagedToolsMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	allTools := m.store.ListManagedTools()

	result := make([]map[string]any, 0, len(allTools))
	for _, t := range allTools {
		entry := map[string]any{
			"name":        t.Name,
			"slug":        t.Slug,
			"description": t.Description,
			"version":     t.Version,
			"is_system":   t.IsSystem,
			"enabled":     t.Enabled,
		}
		if t.ID != "" {
			entry["id"] = t.ID
		}
		if t.Visibility != "" {
			entry["visibility"] = t.Visibility
		}
		if len(t.Tags) > 0 {
			entry["tags"] = t.Tags
		}
		if t.Status != "" {
			entry["status"] = t.Status
		}
		if t.Runtime != nil {
			entry["runtime"] = *t.Runtime
		}
		if t.EntryPoint != nil {
			entry["entry_point"] = *t.EntryPoint
		}
		result = append(result, entry)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"tools": result,
	}))
}

func (m *ManagedToolsMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Name string `json:"name"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}

	info, ok := m.store.GetManagedTool(params.Name)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "managed tool", params.Name)))
		return
	}

	resp := map[string]any{
		"name":        info.Name,
		"slug":        info.Slug,
		"description": info.Description,
		"version":     info.Version,
		"enabled":     info.Enabled,
	}
	if info.ID != "" {
		resp["id"] = info.ID
	}
	if info.Visibility != "" {
		resp["visibility"] = info.Visibility
	}
	if len(info.Tags) > 0 {
		resp["tags"] = info.Tags
	}
	if info.Runtime != nil {
		resp["runtime"] = *info.Runtime
	}
	if info.EntryPoint != nil {
		resp["entry_point"] = *info.EntryPoint
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}
