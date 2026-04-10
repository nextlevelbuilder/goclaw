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
