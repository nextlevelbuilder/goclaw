package agent

import (
	"fmt"
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
func buildTeamMD(team *store.TeamData, members []store.TeamMemberData, selfID uuid.UUID) string {
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

	// Workflow guidance
	sb.WriteString("\n## Workflow\n\n")
	if selfRole == store.TeamRoleLead {
		sb.WriteString("**ONE delegation = ONE spawn call.** The system auto-creates a tracking task for each delegation.\n")
		sb.WriteString("When delegating to multiple agents, call `spawn` once per agent.\n\n")
		sb.WriteString("Example (2 agents):\n")
		sb.WriteString("```\n")
		sb.WriteString("spawn agent=artist, task=\"Create illustration for...\", label=\"Create illustration\"\n")
		sb.WriteString("spawn agent=writer, task=\"Write caption for...\", label=\"Write caption\"\n")
		sb.WriteString("```\n\n")
		sb.WriteString("The `label` parameter sets the task title on the board (keep it short).\n")
		sb.WriteString("Each task auto-completes when its delegation finishes.\n")
		sb.WriteString("Do NOT respond with text before spawning — call all spawns first, then tell the user.\n\n")
		sb.WriteString("When multiple delegations run in parallel, the system collects ALL results and delivers\n")
		sb.WriteString("them to you in a single combined notification. Do NOT present partial results.\n\n")
		sb.WriteString("Advanced: For dependency chains, use `team_tasks` action=create with blocked_by,\n")
		sb.WriteString("then `spawn` with team_task_id=<the created id>.\n\n")
		sb.WriteString("## Orchestration Patterns\n\n")
		sb.WriteString("You can orchestrate multiple rounds — not just one-shot parallel delegation:\n")
		sb.WriteString("- **Sequential**: A finishes → review result → delegate to B with A's output as context\n")
		sb.WriteString("- **Iterative**: A produces draft → delegate to B for review → delegate back to A with feedback\n")
		sb.WriteString("- **Mixed**: A+B in parallel → review both → delegate to C combining their outputs\n\n")
		sb.WriteString("After receiving delegation results, decide: present to user (if done) or continue orchestrating.\n\n")
		sb.WriteString("**Communication**: When updating the user, distinguish between:\n")
		sb.WriteString("- First delegation round → \"assigning to team\" / notifying who is working on what\n")
		sb.WriteString("- Follow-up rounds (after receiving results) → \"updating tasks\" / sharing progress and next steps\n")
		sb.WriteString("Never repeat the same announcement phrasing for follow-up delegations.\n\n")
		sb.WriteString("`team_tasks` actions:\n")
		sb.WriteString("- action=list → active tasks (pending/in_progress/blocked), no results shown\n")
		sb.WriteString("- action=list, status=all → all tasks including completed\n")
		sb.WriteString("- action=get, task_id=<id> → full task detail with result\n")
		sb.WriteString("- action=search, query=<text> → search tasks by subject/description\n")
		sb.WriteString("- action=complete, task_id=<id>, result=<summary> → manually complete a task\n")
		sb.WriteString("- action=cancel, task_id=<id>, reason=<why> → cancel a pending task that is no longer needed\n\n")
		sb.WriteString("Use `team_message` to send updates to team members.\n\n")
		sb.WriteString("For simple questions about team composition, answer directly from the member list above.\n")
	} else {
		sb.WriteString("As a member, when you receive a delegated task, just do the work.\n")
		sb.WriteString("Task completion is handled automatically by the system.\n\n")
		sb.WriteString("For long-running tasks, send progress updates to your lead:\n")
		sb.WriteString("`team_message` action=send, to=<lead_key>, text=<progress update>\n\n")
		sb.WriteString("`team_tasks` actions:\n")
		sb.WriteString("- action=list → check team task board (active tasks)\n")
		sb.WriteString("- action=get, task_id=<id> → read a completed task's full result\n")
		sb.WriteString("- action=search, query=<text> → search tasks\n\n")
		sb.WriteString("Use `team_message` to send updates to your team lead.\n\n")
		sb.WriteString("For simple questions about team composition, answer directly from the member list above.\n")
	}

	return sb.String()
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
	for _, a := range policy.AlsoAllow {
		if a == "group:mcp" {
			return policy
		}
	}
	policy.AlsoAllow = append(policy.AlsoAllow, "group:mcp")
	return policy
}
