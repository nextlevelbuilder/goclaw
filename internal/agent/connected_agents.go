package agent

import "strings"

// ConnectedAgentSummary is the minimal shape for rendering the agent's connected
// external agents (Claude Code, …) into the system prompt, so the agent KNOWS
// what it can delegate to instead of wrongly claiming nothing is connected.
type ConnectedAgentSummary struct {
	Name     string
	Provider string
	Mode     string
}

// buildConnectedAgentsSection renders the "connected agents you can delegate to"
// block. Empty → no section.
func buildConnectedAgentsSection(agents []ConnectedAgentSummary) []string {
	if len(agents) == 0 {
		return nil
	}
	lines := []string{
		"## Connected agents",
		"You can hand a self-contained task to these connected specialists via the `delegate_external` tool. They ARE connected and ready — never tell the user they aren't connected:",
	}
	for _, a := range agents {
		name := a.Name
		if name == "" {
			name = a.Provider
		}
		hint := ""
		if strings.EqualFold(a.Provider, "claude_code") || strings.EqualFold(a.Provider, "claude") || strings.EqualFold(a.Provider, "claudecode") {
			hint = " — software-engineering specialist (writing / refactoring / debugging code)"
		}
		lines = append(lines, "- "+name+hint)
	}
	lines = append(lines,
		"A connected agent runs in an ISOLATED sandbox with no access to your integrations, credentials, or repos — so give it a complete, self-contained task, and do integration/repo work with your own tools rather than delegating it.")
	return lines
}
