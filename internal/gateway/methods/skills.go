package methods

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SkillsMethods handles skills.list, skills.get, skills.update.
// Uses store.SkillStore interface — PGSkillStore in managed mode, FileSkillStore in standalone.
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

	result := make([]map[string]interface{}, 0, len(allSkills))
	for _, s := range allSkills {
		entry := map[string]interface{}{
			"name":        s.Name,
			"slug":        s.Slug,
			"description": s.Description,
			"source":      s.Source,
		}
		if s.ID != "" {
			entry["id"] = s.ID
		}
		result = append(result, entry)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"skills": result,
	}))
}

func (m *SkillsMethods) handleGet(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Name string `json:"name"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "name is required"))
		return
	}

	info, ok := m.store.GetSkill(params.Name)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "skill not found: "+params.Name))
		return
	}

	content, _ := m.store.LoadSkill(params.Name)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"name":        info.Name,
		"description": info.Description,
		"source":      info.Source,
		"content":     content,
	}))
}

// skillUpdater is an optional interface for stores that support skill updates (e.g. PGSkillStore).
type skillUpdater interface {
	UpdateSkill(id uuid.UUID, updates map[string]interface{}) error
}

func (m *SkillsMethods) handleUpdate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Name    string                 `json:"name"`
		ID      string                 `json:"id"`
		Updates map[string]interface{} `json:"updates"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.Name == "" && params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "name or id is required"))
		return
	}

	// Check if the store supports updates (PGSkillStore does, FileSkillStore doesn't)
	updater, ok := m.store.(skillUpdater)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "skills.update not supported in standalone mode"))
		return
	}

	// Resolve skill ID
	var skillID uuid.UUID
	if params.ID != "" {
		parsed, err := uuid.Parse(params.ID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid skill ID"))
			return
		}
		skillID = parsed
	} else {
		// Look up by name — use GetSkill which returns path info, but we need DB ID
		// For PGSkillStore, the name is the slug
		info, exists := m.store.GetSkill(params.Name)
		if !exists {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "skill not found: "+params.Name))
			return
		}
		// Try to parse Path as UUID (PGSkillStore stores DB ID in Path field for managed skills)
		parsed, err := uuid.Parse(info.Path)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "cannot resolve skill ID for file-based skill"))
			return
		}
		skillID = parsed
	}

	if params.Updates == nil || len(params.Updates) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "updates is required"))
		return
	}

	if err := updater.UpdateSkill(skillID, params.Updates); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	m.store.BumpVersion()

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}
