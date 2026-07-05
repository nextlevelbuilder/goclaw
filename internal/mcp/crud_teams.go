package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerTeamsCRUDTools registers the goclaw_teams_* MCP tools backed by
// store.TeamStore. agents is used to resolve agent_key/UUID inputs for lead,
// members, and add/remove-member operations.
func registerTeamsCRUDTools(srv *mcpserver.MCPServer, teams store.TeamStore, agents store.AgentStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_teams_list",
		mcpgo.WithDescription("List teams visible to the caller."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsList(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_create",
		mcpgo.WithDescription("Create a new team."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Team name.")),
		mcpgo.WithString("lead", mcpgo.Required(), mcpgo.Description("Lead agent key or UUID.")),
		mcpgo.WithArray("members", mcpgo.Required(), mcpgo.Description("Member agent keys or UUIDs (at least 1).")),
		mcpgo.WithString("description", mcpgo.Description("Team description.")),
	), handleTeamsCreate(teams, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_get",
		mcpgo.WithDescription("Fetch a single team with its member list."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsGet(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_delete",
		mcpgo.WithDescription("Delete a team."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleTeamsDelete(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_update",
		mcpgo.WithDescription("Apply a partial update to a team's settings."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("name", mcpgo.Description("New team name.")),
		mcpgo.WithString("description", mcpgo.Description("New team description.")),
		mcpgo.WithObject("settings", mcpgo.Description("New team settings object (merged, not replaced).")),
	), handleTeamsUpdate(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_known_users",
		mcpgo.WithDescription("List user IDs known to have interacted with a team."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsKnownUsers(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_scopes",
		mcpgo.WithDescription("List distinct (channel, chatID) task scopes for a team."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsScopes(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_events_list",
		mcpgo.WithDescription("List audit events for a team's tasks."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum entries to return.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsEventsList(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_members_add",
		mcpgo.WithDescription("Add an agent to a team."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("agent", mcpgo.Required(), mcpgo.Description("Agent key or UUID to add.")),
		mcpgo.WithString("role", mcpgo.Enum("member", "reviewer"), mcpgo.Description("Member role; defaults to \"member\".")),
	), handleTeamsMembersAdd(teams, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_members_remove",
		mcpgo.WithDescription("Remove an agent from a team."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID to remove.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleTeamsMembersRemove(teams, agents))
}

func handleTeamsList(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list, err := teams.ListTeams(ctx)
		if err != nil {
			return toolError("teams.list", err)
		}
		return jsonToolResult(map[string]any{"teams": list, "count": len(list)})
	}
}

func handleTeamsCreate(teams store.TeamStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("teams.create", err)
		}
		lead, err := req.RequireString("lead")
		if err != nil {
			return toolError("teams.create", err)
		}
		members, err := req.RequireStringSlice("members")
		if err != nil {
			return toolError("teams.create", err)
		}
		if len(members) == 0 {
			return mcpgo.NewToolResultError("teams.create: at least 1 member is required"), nil
		}

		leadAgent, err := resolveAgentInfo(ctx, agents, lead)
		if err != nil {
			return toolError("teams.create", fmt.Errorf("lead agent: %w", err))
		}
		if existing, _ := teams.GetTeamForAgent(ctx, leadAgent.ID); existing != nil && existing.LeadAgentID == leadAgent.ID {
			return mcpgo.NewToolResultError(fmt.Sprintf("teams.create: agent %q already leads team %q", lead, existing.Name)), nil
		}

		memberAgents := make([]*store.AgentData, 0, len(members))
		for _, m := range members {
			ag, err := resolveAgentInfo(ctx, agents, m)
			if err != nil {
				return toolError("teams.create", fmt.Errorf("member agent %s: %w", m, err))
			}
			memberAgents = append(memberAgents, ag)
		}

		team := &store.TeamData{
			Name:        name,
			LeadAgentID: leadAgent.ID,
			Description: req.GetString("description", ""),
			Status:      store.TeamStatusActive,
		}
		if err := teams.CreateTeam(ctx, team); err != nil {
			return toolError("teams.create", err)
		}
		if err := teams.AddMember(ctx, team.ID, leadAgent.ID, store.TeamRoleLead); err != nil {
			return toolError("teams.create", err)
		}
		for _, ag := range memberAgents {
			if ag.ID == leadAgent.ID {
				continue
			}
			if err := teams.AddMember(ctx, team.ID, ag.ID, store.TeamRoleMember); err != nil {
				return toolError("teams.create", err)
			}
		}
		return jsonToolResult(map[string]any{"team": team})
	}
}

func handleTeamsGet(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.get", err)
		}
		team, err := teams.GetTeam(ctx, teamID)
		if err != nil {
			return toolError("teams.get", err)
		}
		members, err := teams.ListMembers(ctx, teamID)
		if err != nil {
			return toolError("teams.get", err)
		}
		return jsonToolResult(map[string]any{"team": team, "members": members})
	}
}

func handleTeamsDelete(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.delete", err)
		}
		if err := teams.DeleteTeam(ctx, teamID); err != nil {
			return toolError("teams.delete", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsUpdate(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.update", err)
		}
		if _, err := teams.GetTeam(ctx, teamID); err != nil {
			return toolError("teams.update", err)
		}

		updates := map[string]any{}
		if name := req.GetString("name", ""); name != "" {
			updates["name"] = name
		}
		args := req.GetArguments()
		if desc, ok := args["description"].(string); ok {
			updates["description"] = desc
		}
		if settings, ok := args["settings"]; ok {
			raw, err := json.Marshal(settings)
			if err != nil {
				return toolError("teams.update", err)
			}
			updates["settings"] = json.RawMessage(raw)
		}
		if len(updates) == 0 {
			return jsonToolResult(map[string]bool{"ok": true})
		}
		if err := teams.UpdateTeam(ctx, teamID, updates); err != nil {
			return toolError("teams.update", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsKnownUsers(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.known_users", err)
		}
		const defaultLimit = 100
		users, err := teams.KnownUserIDs(ctx, teamID, defaultLimit)
		if err != nil {
			return toolError("teams.known_users", err)
		}
		return jsonToolResult(map[string]any{"users": users})
	}
}

func handleTeamsScopes(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.scopes", err)
		}
		scopes, err := teams.ListTaskScopes(ctx, teamID)
		if err != nil {
			return toolError("teams.scopes", err)
		}
		return jsonToolResult(map[string]any{"scopes": scopes})
	}
}

func handleTeamsEventsList(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.events.list", err)
		}
		limit := int(req.GetFloat("limit", 0))
		offset := int(req.GetFloat("offset", 0))
		events, err := teams.ListTeamEvents(ctx, teamID, limit, offset)
		if err != nil {
			return toolError("teams.events.list", err)
		}
		if events == nil {
			events = []store.TeamTaskEventData{}
		}
		return jsonToolResult(map[string]any{"events": events, "count": len(events)})
	}
}

func handleTeamsMembersAdd(teams store.TeamStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.members.add", err)
		}
		agentRef, err := req.RequireString("agent")
		if err != nil {
			return toolError("teams.members.add", err)
		}
		team, err := teams.GetTeam(ctx, teamID)
		if err != nil {
			return toolError("teams.members.add", err)
		}
		ag, err := resolveAgentInfo(ctx, agents, agentRef)
		if err != nil {
			return toolError("teams.members.add", fmt.Errorf("invalid agent: %w", err))
		}
		if ag.ID == team.LeadAgentID {
			return mcpgo.NewToolResultError("teams.members.add: agent is already the team lead"), nil
		}
		role := req.GetString("role", store.TeamRoleMember)
		switch role {
		case store.TeamRoleMember, store.TeamRoleReviewer:
		default:
			return mcpgo.NewToolResultError("teams.members.add: role must be member or reviewer"), nil
		}
		if err := teams.AddMember(ctx, teamID, ag.ID, role); err != nil {
			return toolError("teams.members.add", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsMembersRemove(teams store.TeamStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.members.remove", err)
		}
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("teams.members.remove", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("teams.members.remove", fmt.Errorf("invalid agent_id: %w", err))
		}
		team, err := teams.GetTeam(ctx, teamID)
		if err != nil {
			return toolError("teams.members.remove", err)
		}
		if agentID == team.LeadAgentID {
			return mcpgo.NewToolResultError("teams.members.remove: cannot remove the team lead"), nil
		}
		if err := teams.RemoveMember(ctx, teamID, agentID); err != nil {
			return toolError("teams.members.remove", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

// parseTeamID extracts and parses the required team_id parameter shared by
// nearly all goclaw_teams_* and goclaw_teams_tasks_* tools.
func parseTeamID(req mcpgo.CallToolRequest) (uuid.UUID, error) {
	teamIDStr, err := req.RequireString("team_id")
	if err != nil {
		return uuid.Nil, err
	}
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid team_id: %w", err)
	}
	return teamID, nil
}
