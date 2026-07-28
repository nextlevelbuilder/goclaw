package teamworkclassify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type ProfileStores struct {
	Agents            store.AgentStore
	Teams             store.TeamStore
	AgentLinks        store.AgentLinkStore
	PinnedSkills      PinnedSkillsSummaryBuilder
	MCP               store.MCPAgentGrantBatchStore
	BuiltinTools      store.BuiltinToolStore
	TenantToolConfigs store.BuiltinToolTenantConfigStore
	ToolPolicy        *tools.PolicyEngine
	ToolRegistry      *tools.Registry
}

// PinnedSkillsSummaryBuilder matches the canonical pinned-skill renderer used
// by the agent system prompt. Keeping this interface narrow lets classifiers
// consume the same context without depending on the concrete skills loader.
type PinnedSkillsSummaryBuilder interface {
	BuildPinnedSummary(ctx context.Context, pinnedNames []string) string
}

type BuildInputOptions struct {
	Mode           Mode
	Message        string
	RecentContext  string
	RecentMessages []providers.Message
	AgentID        uuid.UUID
	// TeamID binds team-mode roster/tool resolution to one authoritative team.
	// When omitted, interactive classification keeps the legacy agent-based lookup.
	TeamID      uuid.UUID
	ToolAllow   []string
	SkillFilter []string
	Embedder    Embedder
	ExtraSelf   []Profile
	ExtraCollab []Profile
	// Timeout is the optional per-stage LLM deadline for the classifier pipeline.
	// Zero keeps the package defaults (defaultArbiterTimeout / defaultPlannerTimeout).
	// The gates pass the tenant's resolved teamworkconfig value so a slow agent
	// model can be given room instead of degrading at an arbitrary stage.
	Timeout time.Duration
}

func BuildInputFromStores(ctx context.Context, stores ProfileStores, opts BuildInputOptions) Input {
	input := Input{
		Mode:          opts.Mode,
		Message:       opts.Message,
		RecentContext: firstNonEmpty(opts.RecentContext, BuildRecentContext(opts.RecentMessages, opts.Message)),
		Embedder:      opts.Embedder,
		Timeout:       opts.Timeout,
		ToolAllow:     append([]string(nil), opts.ToolAllow...),
	}
	if opts.Mode == "" || opts.Mode == ModeSpawn || opts.AgentID == uuid.Nil {
		return input
	}
	var tenantID uuid.UUID
	if stores.Agents != nil {
		if ag, err := stores.Agents.GetByID(ctx, opts.AgentID); err == nil && ag != nil {
			tenantID = ag.TenantID
			input.CurrentAgent = profileFromAgent(*ag, "agent", "", "")
			input.PinnedSkillNames = ag.ParsePinnedSkills()
			if len(input.PinnedSkillNames) > 0 {
				if stores.PinnedSkills == nil {
					input.PinnedSkillsWarning = "pinned skill loader unavailable"
				} else {
					input.PinnedSkillsContext = stores.PinnedSkills.BuildPinnedSummary(ctx, input.PinnedSkillNames)
					if strings.TrimSpace(input.PinnedSkillsContext) == "" {
						input.PinnedSkillsWarning = "pinned skills could not be resolved"
					}
				}
			}
		}
	}
	input.SelfTools = append(input.SelfTools, effectiveSelfToolProfiles(opts.ToolAllow, opts.SkillFilter)...)
	input.SelfTools = append(input.SelfTools, opts.ExtraSelf...)

	if opts.Mode == ModeTeam && stores.Teams != nil {
		if team, err := resolveInputTeam(ctx, stores.Teams, opts); err == nil && team != nil {
			input.CoordinatorAgentID = team.LeadAgentID
			input.CoordinatorAgentKey = team.LeadAgentKey
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
						Kind:                 "team_member",
						Name:                 firstNonEmpty(member.DisplayName, member.AgentKey),
						AgentID:              member.AgentID,
						AgentKey:             member.AgentKey,
						DisplayName:          member.DisplayName,
						TeamRole:             member.Role,
						CapabilitiesStatus:   DataStatusUnknown,
						AvailableToolsStatus: DataStatusUnknown,
						ExpertiseSummary: strings.TrimSpace(strings.Join([]string{
							member.Frontmatter,
							member.AgentDescription,
						}, "\n")),
						Text: strings.TrimSpace(strings.Join([]string{
							"role: " + member.Role,
							"agent_key: " + member.AgentKey,
							member.AgentDescription,
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
					Kind:                 "delegate",
					Name:                 firstNonEmpty(link.TargetDisplayName, link.TargetAgentKey),
					AgentID:              link.TargetAgentID,
					AgentKey:             link.TargetAgentKey,
					DisplayName:          link.TargetDisplayName,
					CapabilitiesStatus:   DataStatusUnknown,
					AvailableToolsStatus: DataStatusUnknown,
					ExpertiseSummary: strings.TrimSpace(strings.Join([]string{
						link.TargetDescription,
						link.Description,
					}, "\n")),
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
	enrichRosterProfiles(ctx, stores, &input, tenantID)
	input.CurrentAgent.TeamRole = input.TeamRole
	input.CollaborationTools = append(input.CollaborationTools, opts.ExtraCollab...)
	return input
}

func resolveInputTeam(ctx context.Context, teams store.TeamStore, opts BuildInputOptions) (*store.TeamData, error) {
	if opts.TeamID != uuid.Nil {
		return teams.GetTeam(ctx, opts.TeamID)
	}
	return teams.GetTeamForAgent(ctx, opts.AgentID)
}

func BuildRecentContext(messages []providers.Message, currentMessage string) string {
	if len(messages) == 0 {
		return ""
	}
	current := strings.TrimSpace(currentMessage)
	const maxMessages = 10
	const maxCharsPerMessage = 4000
	var selected []providers.Message
	for i := len(messages) - 1; i >= 0 && len(selected) < maxMessages; i-- {
		msg := messages[i]
		if msg.Transient {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || (current != "" && content == current) {
			continue
		}
		selected = append(selected, msg)
	}
	if len(selected) == 0 {
		return ""
	}
	var b strings.Builder
	for i := len(selected) - 1; i >= 0; i-- {
		role := strings.ToLower(strings.TrimSpace(selected[i].Role))
		content := truncateContextMessage(strings.TrimSpace(selected[i].Content), maxCharsPerMessage)
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
	}
	return b.String()
}

func truncateContextMessage(value string, max int) string {
	runes := []rune(value)
	if max <= 0 || len(runes) <= max {
		return value
	}
	head := max / 2
	tail := max - head
	return string(runes[:head]) + "\n...[middle omitted]...\n" + string(runes[len(runes)-tail:])
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
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
			{Kind: "tool", Name: "team_tasks", Text: `member cannot assign general tasks; member may create task_type="request" for the canonical coordinator; requests stay durable until explicit lead approval expands the validated workflow`},
			{Kind: "capability", Name: "member approval workflow", Text: "request multi-agent work through the canonical lead approval path"},
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
