package agent

import (
	"slices"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// bootstrapToolAllowlist is the set of tools available during bootstrap onboarding.
// Only write_file (and its alias Write) are needed to save USER.md and clear BOOTSTRAP.md.
var bootstrapToolAllowlist = map[string]bool{
	"write_file": true,
	"Write":      true,
}

// modeCoreToolAllowlist is the minimal set of tools sent for minimal/none prompt
// modes (heartbeat and other lightweight sessions). These sessions rarely call the
// full ~30-tool surface, so restricting to a core set saves the token cost of
// shipping every tool schema on each turn.
var modeCoreToolAllowlist = map[string]bool{
	"read_file":     true,
	"write_file":    true,
	"memory_search": true,
	"exec":          true,
	"datetime":      true,
}

// modeTaskToolDenylist is the set of heavyweight tools stripped from task prompt
// mode (subagent/cron). These sessions don't need media generation, browser
// automation, or sub-orchestration, so their schemas are wasted tokens.
var modeTaskToolDenylist = map[string]bool{
	"create_image": true,
	"create_audio": true,
	"create_video": true,
	"browser":      true,
	"spawn":        true,
	"team_tasks":   true,
}

// modeAiClawToolAllowlist is the focused tool set for ai-claw product mode.
// ai-claw agents are company chat assistants backed by MCP integrations; they
// need file/memory/web/skill/media-read tools but not the full ~30-tool surface
// (no media generation, browser, cron, heartbeat, sub-orchestration, sessions).
// Allowlist (not denylist) keeps token cost low and predictable.
var modeAiClawToolAllowlist = map[string]bool{
	"read_file":       true,
	"write_file":      true,
	"edit":            true,
	"list_files":      true,
	"exec":            true,
	"memory_search":   true,
	"memory_get":      true,
	"datetime":        true,
	"web_search":      true,
	"web_fetch":       true,
	"mcp_tool_search": true,
	"read_image":      true,
	"read_document":   true,
	"skill_search":    true,
	"use_skill":       true,
}

// filterBootstrapTools returns only the bootstrap-allowed tools from the full tool list.
func filterBootstrapTools(toolNames []string) []string {
	var filtered []string
	for _, name := range toolNames {
		if bootstrapToolAllowlist[name] {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// filteredToolNames returns tool names after applying policy filters.
// Used for system prompt so denied tools don't appear in ## Tooling section.
func (l *Loop) filteredToolNames() []string {
	if l.toolPolicy == nil {
		return l.tools.List()
	}
	defs := l.toolPolicy.FilterTools(l.tools, l.id, l.provider.Name(), l.agentToolPolicy, nil, false, false)
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Function.Name
	}
	return names
}

// filteredToolNamesForChannel returns tool names after applying both policy
// and ChannelAware filters. Tools that implement ChannelAware and don't list
// the current channelType are excluded — keeps the system prompt Tooling
// section consistent with the actual tool definitions sent to the LLM.
func (l *Loop) filteredToolNamesForChannel(channelType string) []string {
	names := l.filteredToolNames()
	if channelType == "" {
		return names
	}
	filtered := names[:0:0]
	for _, name := range names {
		if tool, ok := l.tools.Get(name); ok {
			if ca, ok := tool.(tools.ChannelAware); ok {
				if !slices.Contains(ca.RequiredChannelTypes(), channelType) {
					continue
				}
			}
		}
		filtered = append(filtered, name)
	}
	return filtered
}
