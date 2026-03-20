package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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
			sb.WriteString("- **Check task board first** — call `team_tasks(action=\"search\", query=\"<keywords>\")` to find similar tasks before creating. This uses semantic search and saves tokens vs listing all. The system blocks creation if you skip this step\n")
			sb.WriteString("- **Create ALL tasks upfront** in one batch, then announce — then STOP. Do NOT create one task, wait for it to finish, then create the next\n")
			sb.WriteString("- Delegation is NOT completion — do NOT say \"done\"/\"xong\"/\"finished\" after delegating. Only report completion when ALL task results have been delivered\n")
			sb.WriteString("- Results arrive automatically — do NOT present partial results\n")
			sb.WriteString("- **Prefer delegation** — delegate tasks to members, do NOT do the work yourself. If you need research, create a research task. If you need code, create a coding task. Only use tools like web_search yourself for quick clarifications needed to plan tasks (e.g., checking a member's capability), NOT for producing deliverables\n")
			sb.WriteString("- **Do NOT block on completed tasks** — pass completed task's result in the description instead of using blocked_by\n")

			sb.WriteString("\n## Task Planning\n\n")
			sb.WriteString("**CRITICAL: Create the full task graph in ONE batch.** Do NOT create→wait→create sequentially.\n\n")
			sb.WriteString("Each task = ONE deliverable. Complex requests need 3+ tasks.\n\n")
			sb.WriteString("1. Identify ALL distinct deliverables (research, writing, design, code...)\n")
			sb.WriteString("2. Create independent tasks FIRST → get their UUIDs from the response\n")
			sb.WriteString("3. THEN create dependent tasks with `blocked_by=[UUID]` — the system auto-dispatches when blockers complete\n")
			sb.WriteString("   `blocked_by` only accepts real UUIDs returned by previous create calls. Never use placeholders.\n\n")
			sb.WriteString("Same member → sequential (higher priority first). Different members → parallel.\n\n")
			sb.WriteString("**Anti-pattern (WRONG):** create task A → wait for A to finish → create task B → wait...\n")
			sb.WriteString("**Correct pattern:** create A → create B → create C(blocked_by=[A.id, B.id]) → announce → STOP. You must create tasks first to get their UUIDs, then use those UUIDs in blocked_by of dependent tasks.\n\n")
			sb.WriteString("**Example:** User: \"research X and make infographic\"\n")
			sb.WriteString("→ WRONG: web_search(X) yourself → summarize → delegate only infographic\n")
			sb.WriteString("→ RIGHT: task 1 \"Research X\" (assignee=researcher) → task 2 \"Create infographic about X\" (assignee=artist, blocked_by=[task1.id])\n")

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
			sb.WriteString("- Do NOT say \"done\", \"xong\", \"finished\", or any completion language after delegating — delegation is NOT completion\n")
			sb.WriteString("- Do NOT phrase delegation as a question or request (\"nha?\", \"nhé?\", \"okay?\"). State it declaratively\n")
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

		memberCfg := tools.ParseMemberRequestConfig(team.Settings)
		if memberCfg.Enabled {
			sb.WriteString("\n## Requesting Help\n\n")
			sb.WriteString("Need help from another teammate? Create a request:\n")
			sb.WriteString("```\nteam_tasks(action=\"create\", task_type=\"request\", subject=\"...\", assignee=\"agent-key\")\n```\n")
		} else {
			sb.WriteString("\n## Communication\n\n")
			sb.WriteString("Use `team_tasks(action=\"comment\")` to report issues or ask questions on your current task.\n")
		}
	}

	return sb.String()
}

// agentToolPolicyForTeam applies team-specific tool policy adjustments.
// Currently a no-op — team_message tool was removed; members communicate via task comments.
func agentToolPolicyForTeam(policy *config.ToolPolicySpec, _ bool) *config.ToolPolicySpec {
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
