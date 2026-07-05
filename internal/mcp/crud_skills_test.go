package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSkillsList_And_Get(t *testing.T) {
	skills := newFakeSkillStore()
	skills.skills["writer"] = store.SkillInfo{Name: "writer", Slug: "writer", ID: uuid.New().String()}
	srv := newTestMCPServer()
	registerSkillCRUDTools(srv, skills)

	list := callTool(t, srv, "goclaw_skills_list", map[string]any{})
	require.False(t, toolIsError(list))
	assert.Contains(t, toolResultText(list), "writer")

	got := callTool(t, srv, "goclaw_skills_get", map[string]any{"name": "writer"})
	require.False(t, toolIsError(got))
	assert.Contains(t, toolResultText(got), "writer")

	notFound := callTool(t, srv, "goclaw_skills_get", map[string]any{"name": "missing"})
	assert.True(t, toolIsError(notFound))
}

func TestSkillsUpdate_RequiresNameOrID(t *testing.T) {
	manage := newFakeSkillManageStore()
	srv := newTestMCPServer()
	registerSkillCRUDTools(srv, manage)
	registerSkillUpdateCRUDTool(srv, manage, manage)

	missing := callTool(t, srv, "goclaw_skills_update", map[string]any{"updates": map[string]any{"visibility": "tenant"}})
	assert.True(t, toolIsError(missing))
}

func TestSkillsUpdate_ByID_HappyPath(t *testing.T) {
	manage := newFakeSkillManageStore()
	skillID := uuid.New()
	srv := newTestMCPServer()
	registerSkillCRUDTools(srv, manage)
	registerSkillUpdateCRUDTool(srv, manage, manage)

	result := callTool(t, srv, "goclaw_skills_update", map[string]any{
		"id":      skillID.String(),
		"updates": map[string]any{"visibility": "tenant"},
	})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Equal(t, "tenant", manage.updateCalls[skillID]["visibility"])
	assert.Equal(t, 1, manage.bumpCount)
}

func TestSkillsUpdate_MissingUpdatesField(t *testing.T) {
	manage := newFakeSkillManageStore()
	srv := newTestMCPServer()
	registerSkillCRUDTools(srv, manage)
	registerSkillUpdateCRUDTool(srv, manage, manage)

	result := callTool(t, srv, "goclaw_skills_update", map[string]any{"id": uuid.New().String()})
	assert.True(t, toolIsError(result))
}

func TestSkillsUpdate_ByName_SkillNotFound(t *testing.T) {
	manage := newFakeSkillManageStore()
	srv := newTestMCPServer()
	registerSkillCRUDTools(srv, manage)
	registerSkillUpdateCRUDTool(srv, manage, manage)

	result := callTool(t, srv, "goclaw_skills_update", map[string]any{
		"name":    "missing",
		"updates": map[string]any{"visibility": "tenant"},
	})
	assert.True(t, toolIsError(result))
}
