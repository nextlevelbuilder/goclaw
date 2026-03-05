package tools

import (
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Tool groups map group names to tool names.
var toolGroups = map[string][]string{
	"memory":     {"memory_search", "memory_get"},
	"web":        {"web_search", "web_fetch"},
	"fs":         {"read_file", "write_file", "list_files", "edit"},
	"runtime":    {"exec"},
	"sessions":   {"sessions_list", "sessions_history", "sessions_send", "spawn", "session_status"},
	"ui":         {"browser"},
	"automation": {"cron"},
	"messaging":  {"message", "create_forum_topic"},
	"delegation": {"handoff", "delegate_search", "evaluate_loop"},
	"team":       {"team_tasks", "team_message"},
	// Composite group: all goclaw native tools (excludes MCP/custom plugins).
	"goclaw": {
		"read_file", "write_file", "list_files", "edit", "exec",
		"web_search", "web_fetch", "browser",
		"memory_search", "memory_get",
		"sessions_list", "sessions_history", "sessions_send", "spawn", "session_status",
		"cron", "message", "create_forum_topic",
		"read_image", "create_image", "skill_search", "tts",
		"handoff", "delegate_search", "evaluate_loop",
		"team_tasks", "team_message",
	},
}

// ownerOnlyTools are tools that only the instance owner can execute.
// Matching TS OWNER_ONLY_TOOL_NAMES.
var ownerOnlyTools = map[string]bool{
	"whatsapp_login": true,
}

// RegisterToolGroup adds or replaces a dynamic tool group.
// Used by the MCP manager to register "mcp" and "mcp:{serverName}" groups.
func RegisterToolGroup(name string, members []string) {
	toolGroups[name] = members
}

// UnregisterToolGroup removes a dynamic tool group.
func UnregisterToolGroup(name string) {
	delete(toolGroups, name)
}

// Tool profiles define preset allow sets.
var toolProfiles = map[string][]string{
	"minimal":   {"session_status"},
	"coding":    {"group:fs", "group:runtime", "group:sessions", "group:memory", "group:web", "read_image", "create_image", "skill_search"},
	"messaging": {"group:messaging", "group:web", "sessions_list", "sessions_history", "sessions_send", "session_status", "read_image", "skill_search"},
	"full":      {}, // empty = no restrictions
}

// Tool aliases map alternative names to canonical names.
var toolAliases = map[string]string{
	"bash":           "exec",
	"apply-patch":    "apply_patch",
	"edit_file":      "edit",
	"sessions_spawn": "spawn",
}

// Subagent deny lists — tools subagents cannot use.
var subagentDenyList = []string{
	"exec", // subagents should not shell out — main agent can still exec
	"gateway", "agents_list", "whatsapp_login", "session_status",
	"cron", "memory_search", "memory_get", "sessions_send",
}

// Leaf subagent deny — additional restrictions at max spawn depth.
var leafSubagentDenyList = []string{
	"sessions_list", "sessions_history", "spawn",
}

// PolicyEngine evaluates tool access based on layered config policies.
type PolicyEngine struct {
	globalPolicy *config.ToolsConfig
}

// NewPolicyEngine creates a policy engine from global config.
func NewPolicyEngine(cfg *config.ToolsConfig) *PolicyEngine {
	return &PolicyEngine{globalPolicy: cfg}
}

// FilterTools returns only the tools allowed by the policy for the given context.
// It evaluates the 7-step pipeline and returns filtered provider definitions.
func (pe *PolicyEngine) FilterTools(
	registry *Registry,
	agentID string,
	providerName string,
	agentToolPolicy *config.ToolPolicySpec,
	groupToolAllow []string,
	isSubagent bool,
	isLeafAgent bool,
) []providers.ToolDefinition {
	allTools := registry.List()
	allowed := pe.evaluate(allTools, providerName, agentToolPolicy, groupToolAllow)

	// Apply subagent restrictions
	if isSubagent {
		allowed = subtractSet(allowed, subagentDenyList)
	}
	if isLeafAgent {
		allowed = subtractSet(allowed, leafSubagentDenyList)
	}

	// Resolve aliases and build definitions
	var defs []providers.ToolDefinition
	for _, name := range allowed {
		canonical := resolveAlias(name)
		if tool, ok := registry.Get(canonical); ok {
			defs = append(defs, ToProviderDef(tool))
		}
	}

	slog.Debug("tool policy applied",
		"agent", agentID,
		"provider", providerName,
		"total_tools", len(allTools),
		"allowed", len(defs),
		"is_subagent", isSubagent,
	)

	return defs
}

// evaluate runs the 7-step policy pipeline.
func (pe *PolicyEngine) evaluate(
	allTools []string,
	providerName string,
	agentToolPolicy *config.ToolPolicySpec,
	groupToolAllow []string,
) []string {
	g := pe.globalPolicy

	// Step 1: Global profile
	allowed := pe.applyProfile(allTools, g.Profile)

	// Step 2: Provider-level profile override
	if g.ByProvider != nil {
		if pp, ok := g.ByProvider[providerName]; ok && pp.Profile != "" {
			allowed = pe.applyProfile(allTools, pp.Profile)
		}
	}

	// Step 3: Global allow list (restricts to only these)
	if len(g.Allow) > 0 {
		allowed = intersectWithSpec(allowed, g.Allow)
	}

	// Step 4: Provider-level allow override
	if g.ByProvider != nil {
		if pp, ok := g.ByProvider[providerName]; ok && len(pp.Allow) > 0 {
			allowed = intersectWithSpec(allowed, pp.Allow)
		}
	}

	// Step 5: Per-agent allow
	if agentToolPolicy != nil && len(agentToolPolicy.Allow) > 0 {
		allowed = intersectWithSpec(allowed, agentToolPolicy.Allow)
	}

	// Step 6: Per-agent per-provider allow
	if agentToolPolicy != nil && agentToolPolicy.ByProvider != nil {
		if pp, ok := agentToolPolicy.ByProvider[providerName]; ok && len(pp.Allow) > 0 {
			allowed = intersectWithSpec(allowed, pp.Allow)
		}
	}

	// Step 7: Group-level allow
	if len(groupToolAllow) > 0 {
		allowed = intersectWithSpec(allowed, groupToolAllow)
	}

	// Apply global deny
	if len(g.Deny) > 0 {
		allowed = subtractSpec(allowed, g.Deny)
	}

	// Apply agent deny
	if agentToolPolicy != nil && len(agentToolPolicy.Deny) > 0 {
		allowed = subtractSpec(allowed, agentToolPolicy.Deny)
	}

	// Apply alsoAllow (additive — adds back tools without removing existing)
	if len(g.AlsoAllow) > 0 {
		allowed = unionWithSpec(allowed, allTools, g.AlsoAllow)
	}
	if agentToolPolicy != nil && len(agentToolPolicy.AlsoAllow) > 0 {
		allowed = unionWithSpec(allowed, allTools, agentToolPolicy.AlsoAllow)
	}

	return allowed
}

// applyProfile returns tools allowed by a named profile.
// "full" or empty profile = all tools allowed.
func (pe *PolicyEngine) applyProfile(allTools []string, profile string) []string {
	if profile == "" || profile == "full" {
		return copySlice(allTools)
	}

	spec, ok := toolProfiles[profile]
	if !ok {
		slog.Warn("unknown tool profile, using full", "profile", profile)
		return copySlice(allTools)
	}

	return expandSpec(allTools, spec)
}

// --- Set operations with group expansion ---

// expandSpec expands a spec list (which may contain "group:xxx") into concrete tool names,
// filtered against available tools.
func expandSpec(available []string, spec []string) []string {
	expanded := make(map[string]bool)
	for _, s := range spec {
		if strings.HasPrefix(s, "group:") {
			groupName := strings.TrimPrefix(s, "group:")
			if members, ok := toolGroups[groupName]; ok {
				for _, m := range members {
					expanded[m] = true
				}
			}
		} else {
			expanded[s] = true
		}
	}

	var result []string
	for _, t := range available {
		if expanded[t] {
			result = append(result, t)
		}
	}
	return result
}

// intersectWithSpec keeps only tools in `current` that match the spec (with group expansion).
func intersectWithSpec(current []string, spec []string) []string {
	expanded := make(map[string]bool)
	for _, s := range spec {
		if strings.HasPrefix(s, "group:") {
			groupName := strings.TrimPrefix(s, "group:")
			if members, ok := toolGroups[groupName]; ok {
				for _, m := range members {
					expanded[m] = true
				}
			}
		} else {
			expanded[s] = true
		}
	}

	var result []string
	for _, t := range current {
		if expanded[t] {
			result = append(result, t)
		}
	}
	return result
}

// subtractSpec removes tools matching the spec (with group expansion) from current.
func subtractSpec(current []string, spec []string) []string {
	denied := make(map[string]bool)
	for _, s := range spec {
		if strings.HasPrefix(s, "group:") {
			groupName := strings.TrimPrefix(s, "group:")
			if members, ok := toolGroups[groupName]; ok {
				for _, m := range members {
					denied[m] = true
				}
			}
		} else {
			denied[s] = true
		}
	}

	var result []string
	for _, t := range current {
		if !denied[t] {
			result = append(result, t)
		}
	}
	return result
}

// subtractSet removes exact tool names from current.
func subtractSet(current []string, deny []string) []string {
	denied := make(map[string]bool, len(deny))
	for _, d := range deny {
		denied[d] = true
	}
	var result []string
	for _, t := range current {
		if !denied[t] {
			result = append(result, t)
		}
	}
	return result
}

// unionWithSpec adds tools matching spec (from allTools) to current set.
func unionWithSpec(current []string, allTools []string, spec []string) []string {
	existing := make(map[string]bool, len(current))
	for _, t := range current {
		existing[t] = true
	}

	toAdd := expandSpec(allTools, spec)
	for _, t := range toAdd {
		if !existing[t] {
			current = append(current, t)
			existing[t] = true
		}
	}
	return current
}

func resolveAlias(name string) string {
	if canonical, ok := toolAliases[name]; ok {
		return canonical
	}
	return name
}

func copySlice(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	return c
}
