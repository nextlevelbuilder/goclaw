package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// filterManualLinks removes auto-created team links from delegation targets.
// Team members coordinate via team_tasks/team_message, not delegate.
func filterManualLinks(targets []store.AgentLinkData) []store.AgentLinkData {
	var filtered []store.AgentLinkData
	for _, t := range targets {
		if t.TeamID == nil {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// buildDelegateAgentsMD generates DELEGATION.md content listing available delegation targets.
func buildDelegateAgentsMD(targets []store.AgentLinkData) string {
	var sb strings.Builder
	sb.WriteString("# Agent Delegation\n\n")
	sb.WriteString("Use `spawn` with the `agent` parameter to delegate tasks to other specialized agents.\n")
	sb.WriteString("The agent list below is complete and authoritative — answer questions about available agents directly from it.\n")
	sb.WriteString("Only delegate when you need to actually assign work, not to check who is available.\n\n")
	sb.WriteString("## Available Agents\n")

	for _, t := range targets {
		sb.WriteString(fmt.Sprintf("\n### %s", t.TargetAgentKey))
		if t.TargetDisplayName != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", t.TargetDisplayName))
		}
		if t.TargetIsTeamLead && t.TargetTeamName != "" {
			sb.WriteString(fmt.Sprintf(" [Team Lead: %s]", t.TargetTeamName))
		}
		sb.WriteString("\n")
		if t.TargetDescription != "" {
			sb.WriteString(t.TargetDescription + "\n")
		}
		sb.WriteString(fmt.Sprintf("→ `spawn(agent=\"%s\", task=\"describe the task\")`\n", t.TargetAgentKey))
	}

	sb.WriteString("\n## When to Delegate\n\n")
	sb.WriteString("- The task clearly falls under another agent's expertise\n")
	sb.WriteString("- You lack the tools or knowledge to handle it well\n")
	sb.WriteString("- The user explicitly asks to involve another agent\n")

	sb.WriteString("\n## Important\n\n")
	sb.WriteString("- Do NOT use `handoff` to delegate tasks. Use `spawn` instead.\n")
	sb.WriteString("- `handoff` transfers the ENTIRE conversation — the user will talk directly to the other agent.\n")
	sb.WriteString("- Only use `handoff` when the user explicitly asks to be transferred/switched to another agent.\n")

	return sb.String()
}

// buildDelegateSearchInstruction generates DELEGATION.md content that instructs the agent
// to use delegate_search tool instead of listing all targets (used when >15 targets).
func buildDelegateSearchInstruction(targetCount int) string {
	return fmt.Sprintf(`# Agent Delegation

You have the `+"`spawn`"+` tool (with `+"`agent`"+` parameter) and `+"`delegate_search`"+` tool available.
Do NOT look for delegation info on disk — it is provided here.

You have access to %d specialized agents. To find the right one:

1. `+"`delegate_search(query=\"your keywords\")`"+` — search agents by expertise
2. `+"`spawn(agent=\"agent-key\", task=\"describe the task\")`"+` — delegate the task

Example:
- User asks about billing → `+"`delegate_search(query=\"billing payment\")`"+` → `+"`spawn(agent=\"billing-agent\", task=\"...\")`"+`

Do NOT guess agent keys. Always search first.
`, targetCount)
}

// buildTeamMD generates compact TEAM.md content for an agent that is part of a team.
// Kept minimal — tool descriptions already live in tool Parameters()/Description().
// isV2 controls whether advanced sections (orchestration, followup, review) are rendered.
func buildTeamMD(team *store.TeamData, members []store.TeamMemberData, selfID uuid.UUID, isV2 bool) string {
	var sb strings.Builder
	sb.WriteString("# Team: " + team.Name + "\n")
	if team.Description != "" {
		sb.WriteString(team.Description + "\n")
	}

	// Determine self role
	selfRole := store.TeamRoleMember
	for _, m := range members {
		if m.AgentID == selfID {
			selfRole = m.Role
			break
		}
	}
	sb.WriteString(fmt.Sprintf("Role: %s\n\n", selfRole))

	// Members (including self)
	sb.WriteString("## Members\n")
	sb.WriteString("This is the complete and authoritative list of your team. Do NOT use tools to verify this.\n\n")
	for _, m := range members {
		if m.AgentID == selfID {
			sb.WriteString(fmt.Sprintf("- **you** (%s)", m.Role))
		} else if m.DisplayName != "" {
			sb.WriteString(fmt.Sprintf("- **%s** `%s` (%s)", m.DisplayName, m.AgentKey, m.Role))
		} else {
			sb.WriteString(fmt.Sprintf("- **%s** (%s)", m.AgentKey, m.Role))
		}
		if m.Frontmatter != "" {
			sb.WriteString(": " + m.Frontmatter)
		}
		sb.WriteString("\n")
	}

	// Reviewers section (visible to leads)
	if selfRole == store.TeamRoleLead {
		var reviewers []store.TeamMemberData
		for _, m := range members {
			if m.Role == store.TeamRoleReviewer {
				reviewers = append(reviewers, m)
			}
		}
		if len(reviewers) > 0 {
			sb.WriteString("\n## Reviewers\n")
			sb.WriteString("Use reviewers as evaluators in `evaluate_loop` for quality-critical tasks.\n\n")
			for _, r := range reviewers {
				if r.DisplayName != "" {
					sb.WriteString(fmt.Sprintf("- **%s** `%s`", r.DisplayName, r.AgentKey))
				} else {
					sb.WriteString(fmt.Sprintf("- **%s**", r.AgentKey))
				}
				if r.Frontmatter != "" {
					sb.WriteString(": " + r.Frontmatter)
				}
				sb.WriteString("\n")
			}
		}
	}

	// Workflow guidance
	sb.WriteString("\n## Workflow\n\n")
	if selfRole == store.TeamRoleLead {
		sb.WriteString("**ONE delegation = ONE spawn call.** System auto-creates a tracking task per delegation.\n")
		sb.WriteString("The `label` parameter sets the task title (keep it short). Tasks auto-complete when delegation finishes.\n\n")
		sb.WriteString("Rules:\n")
		sb.WriteString("- Call all spawns first, then briefly tell the user what you delegated\n")
		sb.WriteString("- Do NOT add confirmations (\"Done!\", \"Got it!\") — just state what was assigned\n")
		sb.WriteString("- Parallel results arrive in a single combined notification — do NOT present partial results\n")
		sb.WriteString("- For dependency chains: `team_tasks` create with blocked_by, then `spawn` with team_task_id\n")

		if isV2 {
			sb.WriteString("\n## Orchestration Patterns\n\n")
			sb.WriteString("- **Sequential**: A finishes → review → delegate to B with A's output\n")
			sb.WriteString("- **Iterative**: A drafts → B reviews → A revises with feedback\n")
			sb.WriteString("- **Mixed**: A+B parallel → review both → C combines outputs\n\n")
			sb.WriteString("After results: present to user (if done) or continue orchestrating.\n")
			sb.WriteString("Vary announcement phrasing between delegation rounds.\n")

			sb.WriteString("\n## Follow-up Reminders\n\n")
			sb.WriteString("When waiting for user reply: create+claim task, then `await_reply` with text=<reminder>.\n")
			sb.WriteString("System auto-sends reminders. Call `clear_followup` when user replies.\n")
		}

		sb.WriteString("\nFor simple questions about team composition, answer directly from the member list above.\n")
	} else {
		if selfRole == store.TeamRoleReviewer {
			sb.WriteString("You are a **reviewer**. When evaluating, respond with **APPROVED** or **REJECTED: <feedback>**.\n\n")
		}
		sb.WriteString("As a member, just do the delegated work. Task completion is automatic.\n")
		sb.WriteString("For long-running tasks, send progress updates via `team_message` action=send.\n")
	}

	return sb.String()
}

// agentToolPolicyForTeam denies team_message for team leads.
// Leads should use spawn (which auto-announces results back) instead of team_message
// (one-way notification that leaks raw responses to the output channel).
func agentToolPolicyForTeam(policy *config.ToolPolicySpec, isLead bool) *config.ToolPolicySpec {
	if !isLead {
		return policy
	}
	if policy == nil {
		policy = &config.ToolPolicySpec{}
	}
	if slices.Contains(policy.Deny, "team_message") {
		return policy
	}
	policy.Deny = append(policy.Deny, "team_message")
	return policy
}

// agentToolPolicyWithMCP injects "group:mcp" into the agent's alsoAllow list
// when MCP tools are loaded, ensuring the PolicyEngine doesn't block them.
func agentToolPolicyWithMCP(policy *config.ToolPolicySpec, hasMCP bool) *config.ToolPolicySpec {
	if !hasMCP {
		return policy
	}
	if policy == nil {
		policy = &config.ToolPolicySpec{}
	}
	// Check if group:mcp is already present
	if slices.Contains(policy.AlsoAllow, "group:mcp") {
		return policy
	}
	policy.AlsoAllow = append(policy.AlsoAllow, "group:mcp")
	return policy
}

// agentToolPolicyWithWorkspace injects workspace_write and workspace_read into
// alsoAllow when the agent belongs to a team, ensuring the PolicyEngine doesn't
// block them even if the agent has a restrictive allow list.
func agentToolPolicyWithWorkspace(policy *config.ToolPolicySpec, hasTeam bool) *config.ToolPolicySpec {
	if !hasTeam {
		return policy
	}
	if policy == nil {
		policy = &config.ToolPolicySpec{}
	}
	for _, tool := range []string{"workspace_write", "workspace_read"} {
		if !slices.Contains(policy.AlsoAllow, tool) {
			policy.AlsoAllow = append(policy.AlsoAllow, tool)
		}
	}
	return policy
}
