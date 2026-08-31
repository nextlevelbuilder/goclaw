package tools

// Canonical orchestration tool names. These are the tools that let an agent hand
// work to other agents (team workflow, delegation, subagent spawn). They are the
// single source of truth for both the tool identities (each tool's Name() returns
// the matching constant) and the Team Work fail-safe block list, so the two can
// never drift apart.
const (
	ToolNameTeamTasks = "team_tasks"
	ToolNameDelegate  = "delegate"
	ToolNameSpawn     = "spawn"
)

// OrchestrationToolNames returns a fresh copy of the canonical orchestration tool
// names that the Team Work gate blocks when a turn fails safe to self. Returning a
// copy keeps callers from mutating the shared ordering.
func OrchestrationToolNames() []string {
	return []string{ToolNameTeamTasks, ToolNameDelegate, ToolNameSpawn}
}
