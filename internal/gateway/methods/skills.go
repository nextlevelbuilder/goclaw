package methods

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SkillsMethods handles skills.list, skills.get, skills.update.
type SkillsMethods struct {
	store store.SkillStore
}

func NewSkillsMethods(s store.SkillStore) *SkillsMethods {
	return &SkillsMethods{store: s}
}

func (m *SkillsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodSkillsList, m.handleList)
	router.Register(protocol.MethodSkillsGet, m.handleGet)
	router.Register(protocol.MethodSkillsUpdate, m.handleUpdate)
}

func (m *SkillsMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	allSkills := m.store.ListSkills()

	result := make([]map[string]any, 0, len(allSkills))
	for _, s := range allSkills {
		entry := map[string]any{
			"name":        s.Name,
			"slug":        s.Slug,
			"description": s.Description,
			"source":      s.Source,
			"version":     s.Version,
			"is_system":   s.IsSystem,
			"enabled":     s.Enabled,
		}
		if s.ID != "" {
			entry["id"] = s.ID
		}
		if s.Visibility != "" {
			entry["visibility"] = s.Visibility
		}
		if len(s.Tags) > 0 {
			entry["tags"] = s.Tags
		}
		if s.Status != "" {
			entry["status"] = s.Status
		}
		if s.Author != "" {
			entry["author"] = s.Author
		}
		if len(s.MissingDeps) > 0 {
			entry["missing_deps"] = s.MissingDeps
		}
		result = append(result, entry)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"skills": result,
	}))
}

func (m *SkillsMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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

	info, ok := m.store.GetSkill(params.Name)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "skill", params.Name)))
		return
	}

	content, _ := m.store.LoadSkill(params.Name)

	resp := map[string]any{
		"name":        info.Name,
		"slug":        info.Slug,
		"description": info.Description,
		"source":      info.Source,
		"content":     content,
		"version":     info.Version,
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
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}

// skillUpdater is an optional interface for stores that support skill updates (e.g. PGSkillStore).
type skillUpdater interface {
	UpdateSkill(id uuid.UUID, updates map[string]any) error
}

func (m *SkillsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Name    string         `json:"name"`
		ID      string         `json:"id"`
		Updates map[string]any `json:"updates"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.Name == "" && params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name or id")))
		return
	}

	// Check if the store supports updates (PGSkillStore does, FileSkillStore doesn't)
	updater, ok := m.store.(skillUpdater)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgSkillsUpdateNotSupported)))
		return
	}

	// Resolve skill ID
	var skillID uuid.UUID
	if params.ID != "" {
		parsed, err := uuid.Parse(params.ID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "skill")))
			return
		}
		skillID = parsed
	} else {
		// Look up by name — use GetSkill which returns path info, but we need DB ID
		// For PGSkillStore, the name is the slug
		info, exists := m.store.GetSkill(params.Name)
		if !exists {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "skill", params.Name)))
			return
		}
		// Try to parse Path as UUID (PGSkillStore stores DB ID in Path field for managed skills)
		parsed, err := uuid.Parse(info.Path)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCannotResolveSkillID)))
			return
		}
		skillID = parsed
	}

	if params.Updates == nil || len(params.Updates) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "updates")))
		return
	}

	if err := updater.UpdateSkill(skillID, params.Updates); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	m.store.BumpVersion()

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}
