package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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
			sb.WriteString("Reviewers evaluate quality-critical task results.\n\n")
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

	// Workflow guidance — version-aware to match backend behavior.
	sb.WriteString("\n## Workflow\n\n")
	if selfRole == store.TeamRoleLead {
		if isV2 {
			sb.WriteString("Delegate work to team members using `team_tasks` with `assignee`.\n\n")
			sb.WriteString("```\nteam_tasks(action=\"create\", subject=\"...\", description=\"...\", assignee=\"agent-key\")\n```\n\n")
			sb.WriteString("The system auto-dispatches to the assigned member and auto-completes when done.\n")
			sb.WriteString("Do NOT use `spawn` for team delegation — `spawn` is only for self-clone subagent work.\n\n")
			sb.WriteString("Rules:\n")
			sb.WriteString("- Always specify `assignee` — match member expertise from the list above\n")
			sb.WriteString("- **Check task board first** — ALWAYS call `team_tasks(action=\"list\")` before creating tasks. The system blocks creation if you skip this step\n")
			sb.WriteString("- Create all tasks first, then briefly tell the user what you delegated\n")
			sb.WriteString("- Do NOT add confirmations (\"Done!\", \"Got it!\") — just state what was assigned\n")
			sb.WriteString("- Results arrive automatically — do NOT present partial results\n")
			sb.WriteString("- **Prefer delegation** — if the user asks to involve the team, delegate tasks immediately. Do NOT do the work yourself first then hand off to members\n")
			sb.WriteString("- **Do NOT block on completed tasks** — if a dependency task is already done, pass its result in the new task's description instead of using blocked_by\n")
			sb.WriteString("- For dependency chains: use `blocked_by` to sequence tasks\n")

			sb.WriteString("\n## Task Decomposition (CRITICAL)\n\n")
			sb.WriteString("NEVER assign one big task to one member. ALWAYS break user requests into small, atomic tasks:\n\n")
			sb.WriteString("1. **Analyze** the request — identify distinct steps, deliverables, and SKILLS needed (writing, data, design, code...)\n")
			sb.WriteString("2. **Match by SKILL, not topic** — assign based on what the task DOES, not what it's ABOUT.\n")
			sb.WriteString("   Domain experts provide DATA/INFO. Content writers WRITE the article. Designers CREATE visuals.\n")
			sb.WriteString("   Example: \"write about astrology\" → astrology expert provides facts → content writer composes the article\n")
			sb.WriteString("3. **Decompose** into tasks where each has ONE clear deliverable\n")
			sb.WriteString("4. **Distribute** across members — use ALL available members, not just one\n")
			sb.WriteString("5. **Sequence** with `blocked_by` — if task B needs task A's output, set `blocked_by=[task_A_id]`\n")
			sb.WriteString("   IMPORTANT: `blocked_by` requires real task UUIDs from previous create results.\n")
			sb.WriteString("   Create dependency tasks FIRST, get their IDs, THEN create dependent tasks.\n")
			sb.WriteString("   Do NOT use placeholders like \"task_1\" — only real UUIDs work.\n\n")

			sb.WriteString("## Orchestration Patterns\n\n")
			sb.WriteString("For complex requests with multiple steps, plan the full task graph UPFRONT and create all tasks in one turn:\n\n")
			sb.WriteString("- **Parallel**: Independent tasks → create all with different assignees\n")
			sb.WriteString("- **Sequential**: Create Task A first → get its UUID → create Task B with `blocked_by=[A_id]`\n")
			sb.WriteString("- **Mixed**: Create A+B (parallel) → create C with `blocked_by=[A_id, B_id]`\n\n")
			sb.WriteString("Create tasks in order: independent tasks first, then dependent tasks using the returned UUIDs.\n")
			sb.WriteString("The system auto-dispatches blocked tasks when their dependencies complete.\n")
			sb.WriteString("Do NOT wait for results to create follow-up tasks — plan the full pipeline ahead.\n\n")
			sb.WriteString("After results: present to user (if done) or continue orchestrating.\n")
			sb.WriteString("Vary announcement phrasing between delegation rounds.\n")

			sb.WriteString("\n## Follow-up Reminders\n\n")
			sb.WriteString("When you need user input/decision: create+claim task, then `ask_user` with text=<question>. ONLY use when you have a question for the user — NOT for waiting on teammates or status updates.\n")
			sb.WriteString("IMPORTANT: Present the question directly to the user in your response. `ask_user` only sets up periodic REMINDERS in case they don't reply — it does NOT present the question for you.\n")
			sb.WriteString("System auto-sends reminders. Call `clear_ask_user` when user replies.\n")
		} else {
			sb.WriteString("Create a task with `team_tasks` (with `assignee`), then the system dispatches automatically.\n")
			sb.WriteString("Tasks auto-complete when the member finishes.\n\n")
			sb.WriteString("Rules:\n")
			sb.WriteString("- Always specify `assignee` when creating tasks\n")
			sb.WriteString("- Create all tasks first, then briefly tell the user what you delegated\n")
			sb.WriteString("- Do NOT add confirmations (\"Done!\", \"Got it!\") — just state what was assigned\n")
			sb.WriteString("- Results arrive automatically — do NOT present partial results\n")
		}

		sb.WriteString("\nFor simple questions about team composition, answer directly from the member list above.\n")
	} else {
		if selfRole == store.TeamRoleReviewer {
			sb.WriteString("You are a **reviewer**. When evaluating, respond with **APPROVED** or **REJECTED: <feedback>**.\n\n")
		}
		sb.WriteString("As a member, focus entirely on your assigned task.\n\n")
		sb.WriteString("Rules:\n")
		sb.WriteString("- Stay on task — do not deviate from the assignment\n")
		sb.WriteString("- Your final response becomes the task result — make it clear, complete, and actionable\n")
		sb.WriteString("- For long tasks, report progress: `team_tasks(action=\"progress\", percent=50, text=\"status\")`\n")
		sb.WriteString("- The task_id is auto-resolved — you don't need to specify it\n")
		sb.WriteString("- Task completion is automatic when your run finishes\n")
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

// agentToolPolicyWithWorkspace injects file tools into alsoAllow when the agent
// belongs to a team, ensuring the PolicyEngine doesn't block them even if the
// agent has a restrictive allow list. File tools are now workspace-aware via
// WorkspaceInterceptor, so no separate workspace_write/workspace_read needed.
func agentToolPolicyWithWorkspace(policy *config.ToolPolicySpec, hasTeam bool) *config.ToolPolicySpec {
	if !hasTeam {
		return policy
	}
	if policy == nil {
		policy = &config.ToolPolicySpec{}
	}
	for _, tool := range []string{"read_file", "write_file", "list_files"} {
		if !slices.Contains(policy.AlsoAllow, tool) {
			policy.AlsoAllow = append(policy.AlsoAllow, tool)
		}
	}
	return policy
}
