package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
)

// ExportTeamByID fetches team metadata by team_id directly (not via lead_agent_id).
func ExportTeamByID(ctx context.Context, db *sql.DB, teamID uuid.UUID) (*TeamExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var t TeamExport
	err = db.QueryRowContext(ctx,
		"SELECT name, COALESCE(description,''), status, COALESCE(settings,'{}')"+
			" FROM agent_teams WHERE id = $1"+tc,
		append([]any{teamID}, tcArgs...)...,
	).Scan(&t.Name, &t.Description, &t.Status, &t.Settings)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTeamMemberAgents returns all agents (id + agent_key) in a team.
func GetTeamMemberAgents(ctx context.Context, db *sql.DB, teamID uuid.UUID) ([]struct {
	ID       uuid.UUID
	AgentKey string
}, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 2, "m")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT a.id, a.agent_key"+
			" FROM agent_team_members m"+
			" JOIN agents a ON a.id = m.agent_id"+
			" WHERE m.team_id = $1"+tc,
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []struct {
		ID       uuid.UUID
		AgentKey string
	}
	for rows.Next() {
		var item struct {
			ID       uuid.UUID
			AgentKey string
		}
		if err := rows.Scan(&item.ID, &item.AgentKey); err != nil {
			slog.Warn("export.team.member_agents.scan", "error", err)
			continue
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ExportTeamLinksForTeam returns agent_links where both source and target are members of the team.
func ExportTeamLinksForTeam(ctx context.Context, db *sql.DB, teamID uuid.UUID) ([]AgentLinkExport, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 2, "l")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT sa.agent_key, ta.agent_key, l.direction, COALESCE(l.description,'')"+
			" FROM agent_links l"+
			" JOIN agents sa ON sa.id = l.source_agent_id"+
			" JOIN agents ta ON ta.id = l.target_agent_id"+
			" WHERE l.source_agent_id IN (SELECT agent_id FROM agent_team_members WHERE team_id = $1)"+
			" AND l.target_agent_id IN (SELECT agent_id FROM agent_team_members WHERE team_id = $1)"+tc,
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentLinkExport
	for rows.Next() {
		var l AgentLinkExport
		if err := rows.Scan(&l.SourceAgentKey, &l.TargetAgentKey, &l.Direction, &l.Description); err != nil {
			slog.Warn("export.team.links.scan", "error", err)
			continue
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ExportTeamPreviewCountsByID returns task/member/link counts for a team by team_id.
func ExportTeamPreviewCountsByID(ctx context.Context, db *sql.DB, teamID uuid.UUID) (tasks, members, links int, err error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return
	}
	args := append([]any{teamID}, tcArgs...)

	_ = db.QueryRowContext(ctx,
		"SELECT"+
			" (SELECT COUNT(*) FROM team_tasks WHERE team_id = $1"+tc+"),"+
			" (SELECT COUNT(*) FROM agent_team_members WHERE team_id = $1"+tc+"),"+
			" (SELECT COUNT(*) FROM agent_links"+
			"   WHERE source_agent_id IN (SELECT agent_id FROM agent_team_members WHERE team_id = $1"+tc+")"+
			"   AND target_agent_id IN (SELECT agent_id FROM agent_team_members WHERE team_id = $1"+tc+")"+tc+")",
		args...,
	).Scan(&tasks, &members, &links)
	return
}

// ExportTeamMembersNonLead returns all non-lead members by team_id (no lead exclusion needed when doing standalone team export).
func ExportTeamMembersNonLead(ctx context.Context, db *sql.DB, teamID uuid.UUID, leadAgentID uuid.UUID) ([]TeamMemberExport, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 3, "m")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT a.agent_key, m.role"+
			" FROM agent_team_members m"+
			" JOIN agents a ON a.id = m.agent_id"+
			" WHERE m.team_id = $1 AND m.agent_id != $2"+tc,
		append([]any{teamID, leadAgentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TeamMemberExport
	for rows.Next() {
		var m TeamMemberExport
		if err := rows.Scan(&m.AgentKey, &m.Role); err != nil {
			slog.Warn("export.team.members_non_lead.scan", "error", err)
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ExportTeamLeadAgentID returns the lead agent_id for a team, or uuid.Nil if not found.
func ExportTeamLeadAgentID(ctx context.Context, db *sql.DB, teamID uuid.UUID) (uuid.UUID, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return uuid.Nil, err
	}
	var leadID uuid.UUID
	err = db.QueryRowContext(ctx,
		"SELECT lead_agent_id FROM agent_teams WHERE id = $1"+tc,
		append([]any{teamID}, tcArgs...)...,
	).Scan(&leadID)
	if err == sql.ErrNoRows {
		return uuid.Nil, nil
	}
	return leadID, err
}

// ExportTeamAgentConfig returns agent metadata (id, agent_key, display_name, provider, model, etc.)
// as a raw JSON-serializable map using marshalAgentConfigExport helper. Returned as json.RawMessage.
func ExportTeamAgentBasicInfo(ctx context.Context, db *sql.DB, agentID uuid.UUID) (agentKey string, err error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return "", err
	}
	err = db.QueryRowContext(ctx,
		"SELECT agent_key FROM agents WHERE id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	).Scan(&agentKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return agentKey, err
}

// ExportTeamMembersAll returns ALL members of a team (including lead) with agent_key resolution.
func ExportTeamMembersAll(ctx context.Context, db *sql.DB, teamID uuid.UUID) ([]TeamMemberExport, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 2, "m")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT a.agent_key, m.role"+
			" FROM agent_team_members m"+
			" JOIN agents a ON a.id = m.agent_id"+
			" WHERE m.team_id = $1"+tc,
		append([]any{teamID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TeamMemberExport
	for rows.Next() {
		var m TeamMemberExport
		if err := rows.Scan(&m.AgentKey, &m.Role); err != nil {
			slog.Warn("export.team.all_members.scan", "error", err)
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// exportTeamAgentFullData returns a fully-serializable agent config for team export.
func ExportTeamAgentJSON(ctx context.Context, db *sql.DB, agentID uuid.UUID) (json.RawMessage, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	var (
		agentKey          string
		displayName       string
		frontmatter       string
		provider          string
		model             string
		contextWindow     int
		maxToolIterations int
		agentType         string
		status            string
		toolsConfig       []byte
		sandboxConfig     []byte
		subagentsConfig   []byte
		memoryConfig      []byte
		compactionConfig  []byte
		contextPruning    []byte
		otherConfig       []byte
	)

	err = db.QueryRowContext(ctx,
		"SELECT agent_key, COALESCE(display_name,''), COALESCE(frontmatter,''),"+
			" COALESCE(provider,''), COALESCE(model,''),"+
			" context_window, max_tool_iterations, COALESCE(agent_type,'open'), COALESCE(status,'active'),"+
			" tools_config, sandbox_config, subagents_config, memory_config, compaction_config,"+
			" context_pruning, other_config"+
			" FROM agents WHERE id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	).Scan(
		&agentKey, &displayName, &frontmatter,
		&provider, &model,
		&contextWindow, &maxToolIterations, &agentType, &status,
		&toolsConfig, &sandboxConfig, &subagentsConfig, &memoryConfig, &compactionConfig,
		&contextPruning, &otherConfig,
	)
	if err != nil {
		return nil, err
	}

	type exportableAgent struct {
		AgentKey          string          `json:"agent_key"`
		DisplayName       string          `json:"display_name,omitempty"`
		Frontmatter       string          `json:"frontmatter,omitempty"`
		Provider          string          `json:"provider"`
		Model             string          `json:"model"`
		ContextWindow     int             `json:"context_window"`
		MaxToolIterations int             `json:"max_tool_iterations"`
		AgentType         string          `json:"agent_type"`
		Status            string          `json:"status"`
		ToolsConfig       json.RawMessage `json:"tools_config,omitempty"`
		SandboxConfig     json.RawMessage `json:"sandbox_config,omitempty"`
		SubagentsConfig   json.RawMessage `json:"subagents_config,omitempty"`
		MemoryConfig      json.RawMessage `json:"memory_config,omitempty"`
		CompactionConfig  json.RawMessage `json:"compaction_config,omitempty"`
		ContextPruning    json.RawMessage `json:"context_pruning,omitempty"`
		OtherConfig       json.RawMessage `json:"other_config,omitempty"`
	}
	raw, err := json.Marshal(exportableAgent{
		AgentKey:          agentKey,
		DisplayName:       displayName,
		Frontmatter:       frontmatter,
		Provider:          provider,
		Model:             model,
		ContextWindow:     contextWindow,
		MaxToolIterations: maxToolIterations,
		AgentType:         agentType,
		Status:            status,
		ToolsConfig:       toolsConfig,
		SandboxConfig:     sandboxConfig,
		SubagentsConfig:   subagentsConfig,
		MemoryConfig:      memoryConfig,
		CompactionConfig:  compactionConfig,
		ContextPruning:    contextPruning,
		OtherConfig:       otherConfig,
	})
	return raw, err
}
