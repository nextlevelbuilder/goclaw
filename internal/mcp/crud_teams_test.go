package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestTeamsCreate_HappyPath(t *testing.T) {
	teams := newFakeTeamStore()
	agents := newFakeAgentStore()
	leadID := uuid.New()
	memberID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: leadID}, AgentKey: "lead"})
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: memberID}, AgentKey: "member"})

	srv := newTestMCPServer()
	registerTeamsCRUDTools(srv, teams, agents)

	result := callTool(t, srv, "goclaw_teams_create", map[string]any{
		"name":    "eng-team",
		"lead":    "lead",
		"members": []any{"member"},
	})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Contains(t, toolResultText(result), "eng-team")
	assert.Len(t, teams.teams, 1)
}

func TestTeamsCreate_RequiresAtLeastOneMember(t *testing.T) {
	teams := newFakeTeamStore()
	agents := newFakeAgentStore()
	srv := newTestMCPServer()
	registerTeamsCRUDTools(srv, teams, agents)

	result := callTool(t, srv, "goclaw_teams_create", map[string]any{
		"name": "eng-team", "lead": "lead", "members": []any{},
	})
	assert.True(t, toolIsError(result))
}

func TestTeamsCreate_UnknownLeadAgent(t *testing.T) {
	teams := newFakeTeamStore()
	agents := newFakeAgentStore()
	srv := newTestMCPServer()
	registerTeamsCRUDTools(srv, teams, agents)

	result := callTool(t, srv, "goclaw_teams_create", map[string]any{
		"name": "eng-team", "lead": "missing-lead", "members": []any{"m"},
	})
	assert.True(t, toolIsError(result))
}

func TestTeamsGet_And_Delete(t *testing.T) {
	teams := newFakeTeamStore()
	agents := newFakeAgentStore()
	teamID := uuid.New()
	teams.teams[teamID] = &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "eng-team"}
	srv := newTestMCPServer()
	registerTeamsCRUDTools(srv, teams, agents)

	got := callTool(t, srv, "goclaw_teams_get", map[string]any{"team_id": teamID.String()})
	require.False(t, toolIsError(got))
	assert.Contains(t, toolResultText(got), "eng-team")

	invalidID := callTool(t, srv, "goclaw_teams_get", map[string]any{"team_id": "not-a-uuid"})
	assert.True(t, toolIsError(invalidID))

	deleted := callTool(t, srv, "goclaw_teams_delete", map[string]any{"team_id": teamID.String()})
	require.False(t, toolIsError(deleted))

	getAfterDelete := callTool(t, srv, "goclaw_teams_get", map[string]any{"team_id": teamID.String()})
	assert.True(t, toolIsError(getAfterDelete))
}

func TestTeamsMembersAdd_RejectsAddingLeadAgain(t *testing.T) {
	teams := newFakeTeamStore()
	agents := newFakeAgentStore()
	teamID := uuid.New()
	leadID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: leadID}, AgentKey: "lead"})
	teams.teams[teamID] = &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, LeadAgentID: leadID}
	srv := newTestMCPServer()
	registerTeamsCRUDTools(srv, teams, agents)

	result := callTool(t, srv, "goclaw_teams_members_add", map[string]any{"team_id": teamID.String(), "agent": "lead"})
	assert.True(t, toolIsError(result))
}

func TestTeamsMembersRemove_RejectsRemovingLead(t *testing.T) {
	teams := newFakeTeamStore()
	agents := newFakeAgentStore()
	teamID := uuid.New()
	leadID := uuid.New()
	agents.add(&store.AgentData{BaseModel: store.BaseModel{ID: leadID}, AgentKey: "lead"})
	teams.teams[teamID] = &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, LeadAgentID: leadID}
	srv := newTestMCPServer()
	registerTeamsCRUDTools(srv, teams, agents)

	result := callTool(t, srv, "goclaw_teams_members_remove", map[string]any{"team_id": teamID.String(), "agent_id": "lead"})
	assert.True(t, toolIsError(result))
}
