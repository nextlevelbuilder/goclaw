package teamworkclassify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type ProfileStores struct {
	Agents     store.AgentStore
	Teams      store.TeamStore
	AgentLinks store.AgentLinkStore
}

type BuildInputOptions struct {
	Mode        Mode
	Message     string
	AgentID     uuid.UUID
	ToolAllow   []string
	SkillFilter []string
	Embedder    Embedder
	ExtraSelf   []Profile
	ExtraCollab []Profile
}

func BuildInputFromStores(ctx context.Context, stores ProfileStores, opts BuildInputOptions) Input {
	input := Input{
		Mode:     opts.Mode,
		Message:  opts.Message,
		Embedder: opts.Embedder,
	}
	if opts.Mode == "" || opts.Mode == ModeSpawn || opts.AgentID == uuid.Nil {
		return input
	}
	if stores.Agents != nil {
		if ag, err := stores.Agents.GetByID(ctx, opts.AgentID); err == nil && ag != nil {
			input.CurrentAgent = Profile{
				Kind: "agent",
				Name: firstNonEmpty(ag.DisplayName, ag.AgentKey),
				Text: strings.TrimSpace(strings.Join([]string{
					ag.Frontmatter,
					ag.AgentDescription,
				}, "\n")),
			}
		}
	}
	input.SelfTools = append(input.SelfTools, effectiveSelfToolProfiles(opts.ToolAllow, opts.SkillFilter)...)
	input.SelfTools = append(input.SelfTools, opts.ExtraSelf...)

	if opts.Mode == ModeTeam && stores.Teams != nil {
		if team, err := stores.Teams.GetTeamForAgent(ctx, opts.AgentID); err == nil && team != nil {
			input.TeamRole = "member"
			if team.LeadAgentID == opts.AgentID {
				input.TeamRole = "lead"
				input.CanAssignTeamTasks = true
			}
			memberRequestCfg := parseMemberRequestRoutingConfig(team.Settings)
			input.MemberRequestsEnabled = memberRequestCfg.Enabled
			input.MemberRequestsAutoDispatch = memberRequestCfg.AutoDispatch
			input.Team = Profile{
				Kind: "team",
				Name: team.Name,
				Text: strings.TrimSpace(strings.Join([]string{
					team.Description,
					fmt.Sprintf("lead_agent: %s %s", team.LeadAgentKey, team.LeadDisplayName),
				}, "\n")),
			}
			if members, err := stores.Teams.ListMembers(ctx, team.ID); err == nil {
				for _, member := range members {
					if member.AgentID == opts.AgentID && !input.CanAssignTeamTasks {
						if strings.TrimSpace(member.Role) != "" {
							input.TeamRole = member.Role
						}
					}
					input.Members = append(input.Members, Profile{
						Kind: "team_member",
						Name: firstNonEmpty(member.DisplayName, member.AgentKey),
						Text: strings.TrimSpace(strings.Join([]string{
							"role: " + member.Role,
							"agent_key: " + member.AgentKey,
							member.Frontmatter,
						}, "\n")),
					})
				}
			}
			input.CollaborationTools = append(input.CollaborationTools, teamPermissionProfiles(input)...)
		}
	}

	if stores.AgentLinks != nil {
		if links, err := stores.AgentLinks.DelegateTargets(ctx, opts.AgentID); err == nil {
			for _, link := range links {
				input.Delegates = append(input.Delegates, Profile{
					Kind: "delegate",
					Name: firstNonEmpty(link.TargetDisplayName, link.TargetAgentKey),
					Text: strings.TrimSpace(strings.Join([]string{
						"agent_key: " + link.TargetAgentKey,
						"link_direction: " + link.Direction,
						"link_team: " + link.TeamName,
						"target_team: " + link.TargetTeamName,
						fmt.Sprintf("target_is_team_lead: %t", link.TargetIsTeamLead),
						link.Description,
						link.TargetDescription,
					}, "\n")),
				})
			}
			if opts.Mode == ModeDelegate && len(links) > 0 {
				input.CollaborationTools = append(input.CollaborationTools,
					Profile{Kind: "tool", Name: "delegate", Text: "delegate work to linked agents with matching expertise and receive their result"},
				)
			}
		}
	}
	input.CollaborationTools = append(input.CollaborationTools, opts.ExtraCollab...)
	return input
}

type memberRequestRoutingConfig struct {
	Enabled      bool
	AutoDispatch bool
}

func parseMemberRequestRoutingConfig(settings json.RawMessage) memberRequestRoutingConfig {
	var cfg memberRequestRoutingConfig
	if len(settings) == 0 {
		return cfg
	}
	var raw struct {
		MemberRequests *struct {
			Enabled      *bool `json:"enabled"`
			AutoDispatch *bool `json:"auto_dispatch"`
		} `json:"member_requests"`
	}
	if json.Unmarshal(settings, &raw) != nil || raw.MemberRequests == nil {
		return cfg
	}
	if raw.MemberRequests.Enabled != nil {
		cfg.Enabled = *raw.MemberRequests.Enabled
	}
	if raw.MemberRequests.AutoDispatch != nil {
		cfg.AutoDispatch = *raw.MemberRequests.AutoDispatch
	}
	return cfg
}

func teamPermissionProfiles(input Input) []Profile {
	role := strings.ToLower(strings.TrimSpace(input.TeamRole))
	if role == "" || role == "lead" || input.CanAssignTeamTasks {
		return []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "lead can search existing tasks, create tasks, assign work to team members, track progress, review and complete team work"},
			{Kind: "tool", Name: "ask_user", Text: "ask the user for decisions or missing information during team work"},
			{Kind: "capability", Name: "shared team workspace", Text: "coordinate multi-step work through shared files, task board, and team member results"},
		}
	}
	if input.MemberRequestsEnabled && input.MemberRequestsAutoDispatch {
		return []Profile{
			{Kind: "tool", Name: "team_tasks", Text: `member cannot assign general tasks; member may create task_type="request" to ask another teammate for help; requests auto-dispatch to the assignee`},
			{Kind: "capability", Name: "member request workflow", Text: "request help from teammates without lead-style assignment authority"},
		}
	}
	if input.MemberRequestsEnabled {
		return []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "member request tasks are enabled but auto-dispatch is disabled; requests stay pending for leader review and should not be used as immediate routed workflow"},
			{Kind: "capability", Name: "member limited team access", Text: "cannot coordinate new team work without the lead"},
		}
	}
	return []Profile{
		{Kind: "tool", Name: "team_tasks", Text: "member cannot create or assign general tasks; member request tasks are disabled; use comments/progress/current-task actions only"},
		{Kind: "capability", Name: "member limited team access", Text: "cannot coordinate new team work without the lead"},
	}
}

func effectiveSelfToolProfiles(toolAllow, skillFilter []string) []Profile {
	var profiles []Profile
	if len(toolAllow) > 0 {
		profiles = append(profiles, Profile{
			Kind: "tool_allow",
			Name: "channel allowed tools",
			Text: "effective channel/group allowed tools: " + strings.Join(toolAllow, ", "),
		})
	} else {
		profiles = append(profiles, Profile{
			Kind: "tool_allow",
			Name: "default tools",
			Text: "current agent may use its configured tools for direct small tasks",
		})
	}
	if len(skillFilter) > 0 {
		profiles = append(profiles, Profile{
			Kind: "skill_filter",
			Name: "topic skills",
			Text: "effective topic skill filter: " + strings.Join(skillFilter, ", "),
		})
	}
	return profiles
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
