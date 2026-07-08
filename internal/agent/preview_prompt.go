package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// MCPPreviewLister returns MCP tool info from store configuration without
// requiring live MCP server connections. Satisfied by *mcp.Manager.
type MCPPreviewLister interface {
	ListToolsForAgent(ctx context.Context, agentID uuid.UUID, userID string) ([]MCPToolPreviewInfo, error)
}

// MCPToolPreviewInfo describes an MCP tool for preview purposes.
// Mirrors mcp.MCPToolPreviewInfo but declared here to avoid an import cycle.
type MCPToolPreviewInfo struct {
	// RegisteredName is the tool name including mcp_ prefix.
	RegisteredName string
	// Description is the tool description from server tool hints, if any.
	Description string
	// Parameters is the tool's cached JSON Schema for input parameters, if
	// one was captured at connect-time. nil when no schema is cached yet
	// (e.g. server never connected, or cached before schema capture existed).
	Parameters json.RawMessage
}

// PreviewDeps holds optional dependencies for building a preview system prompt.
// All fields are nil-safe — missing deps simply skip resolution for that section.
type PreviewDeps struct {
	AgentStore       store.AgentStore
	TeamStore        store.TeamStore
	AgentLinks       store.AgentLinkStore
	ProviderReg      *providers.Registry
	SkillAccessStore store.SkillAccessStore
	ToolLister       interface {
		List() []string
		Get(name string) (tools.Tool, bool)
		Aliases() map[string]string
	}
	// ToolPolicy is the gateway's live PolicyEngine, used to resolve the full
	// allow/deny/alsoAllow pipeline (including global deny) for preview tool
	// names. nil = skip policy filtering (only skill_manage gating and alias
	// exclusion apply).
	ToolPolicy   *tools.PolicyEngine
	SkillsLoader interface {
		BuildPinnedSummary(ctx context.Context, names []string) string
		BuildSummary(ctx context.Context, allowList []string) string
	}
	// MCPLister provides store-based MCP tool info for preview (nil = skip).
	// When set, MCP tool descriptions are populated from configured servers
	// even if those servers are not currently loaded in the tool registry.
	MCPLister MCPPreviewLister
	// DisabledTools is the per-tenant set of disabled tool names (nil/empty = none disabled).
	DisabledTools map[string]bool
	DataDir       string // for team workspace path construction
}

// PreviewResult holds the output of BuildPreviewPrompt.
type PreviewResult struct {
	Prompt   string
	ToolDefs []providers.ToolDefinition // tool definitions (schemas) as sent to the LLM
}

// BuildPreviewPrompt builds a system prompt for preview purposes.
// Reuses the same BuildSystemPrompt() as the LLM pipeline, resolving as many
// fields as possible from agent data + DB stores. Runtime-only fields
// (channel, peer kind, session context, credentials) are left at zero values —
// BuildSystemPrompt already nil-checks every field.
func BuildPreviewPrompt(ctx context.Context, ag *store.AgentData, mode PromptMode, userID string, deps PreviewDeps) PreviewResult {
	slog.Debug("BuildPreviewPrompt called", "agent_id", ag.ID, "mode", mode, "user_id", userID, "mcp_lister_nil", deps.MCPLister == nil)
	// --- Context files ---
	var contextFiles []bootstrap.ContextFile
	if deps.AgentStore != nil {
		agentFiles, _ := deps.AgentStore.GetAgentContextFiles(ctx, ag.ID)
		for _, f := range agentFiles {
			if f.Content == "" {
				continue
			}
			// Mode-aware context file filtering
			if allowlist := bootstrap.ModeAllowlist(string(mode)); allowlist != nil {
				if !allowlist[f.FileName] {
					continue
				}
			}
			contextFiles = append(contextFiles, bootstrap.ContextFile{Path: f.FileName, Content: f.Content})
		}
		// Merge per-user overrides if user_id provided.
		if userID != "" {
			contextFiles = mergePreviewUserFiles(ctx, deps.AgentStore, ag.ID, contextFiles, userID, mode)
		}
	}

	// --- Tool names ---
	var toolNames []string
	if deps.ToolLister != nil {
		toolNames = deps.ToolLister.List()
	} else {
		toolNames = fallbackPreviewToolNames
	}

	// --- skill_manage gating (matches loop_history.go:124-131) ---
	if !ag.ParseSkillEvolve() {
		filtered := make([]string, 0, len(toolNames))
		for _, n := range toolNames {
			if n != "skill_manage" {
				filtered = append(filtered, n)
			}
		}
		toolNames = filtered
	}

	// --- Resolve concrete *tools.Registry (needed by WouldAllow to expand
	// group:* specs like "group:mcp" in Allow/AlsoAllow). Falls back to nil
	// when deps.ToolLister is a narrower test mock that doesn't fully
	// implement tools.ToolExecutor — WouldAllow tolerates a nil registry by
	// skipping group expansion, matching prior behavior for such mocks.
	var toolRegistry *tools.Registry
	if executor, ok := deps.ToolLister.(tools.ToolExecutor); ok {
		toolRegistry = tools.ResolveConcreteRegistry(executor)
	}

	// --- Full agent tool policy (profile→allow→deny→alsoAllow, including global deny) ---
	// Uses the gateway's live PolicyEngine via WouldAllow, mirroring the real chat
	// loop's resolution exactly, modulo channel-specific filtering (genuinely
	// unavailable here — preview has no channel/session context).
	if deps.ToolPolicy != nil {
		toolPolicy := ag.ParseToolsConfig()
		filtered := make([]string, 0, len(toolNames))
		for _, n := range toolNames {
			if deps.ToolPolicy.WouldAllow(toolRegistry, n, ag.Provider, toolPolicy, nil) {
				filtered = append(filtered, n)
			}
		}
		toolNames = filtered
	} else if toolPolicy := ag.ParseToolsConfig(); toolPolicy != nil && len(toolPolicy.Deny) > 0 {
		// Fallback when no PolicyEngine is wired (e.g. caller didn't set
		// ToolPolicy): degrade to per-agent deny-only, matching the
		// pre-PolicyEngine preview behavior. Global deny is unavailable in
		// this fallback since it requires the PolicyEngine's global policy.
		denySet := make(map[string]bool, len(toolPolicy.Deny))
		for _, d := range toolPolicy.Deny {
			denySet[d] = true
		}
		filtered := make([]string, 0, len(toolNames))
		for _, n := range toolNames {
			if !denySet[n] {
				filtered = append(filtered, n)
			}
		}
		toolNames = filtered
	}

	// --- Alias exclusion (matches loop_history.go:136-146) ---
	if deps.ToolLister != nil {
		if aliasSet := deps.ToolLister.Aliases(); len(aliasSet) > 0 {
			filtered := make([]string, 0, len(toolNames))
			for _, n := range toolNames {
				if _, isAlias := aliasSet[n]; !isAlias {
					filtered = append(filtered, n)
				}
			}
			toolNames = filtered
		}
	}

	// --- Per-tenant disabled tools (matches loop_tool_filter.go:105-117) ---
	if len(deps.DisabledTools) > 0 {
		filtered := make([]string, 0, len(toolNames))
		for _, n := range toolNames {
			if !deps.DisabledTools[n] {
				filtered = append(filtered, n)
			}
		}
		toolNames = filtered
	}

	// --- MCP tool descriptions (matches loop_history_supplement.go:44-58) ---
	// mcpToolParams tracks real parameter schemas alongside descriptions, keyed
	// by the same RegisteredName. Populated when a real schema is available
	// (live registry tool, or cached schema from a prior live connection).
	// First populate from live registry (tools currently loaded in active sessions).
	var mcpToolDescs map[string]string
	var mcpToolParams map[string]map[string]any
	if deps.ToolLister != nil {
		descs := make(map[string]string)
		params := make(map[string]map[string]any)
		for _, name := range toolNames {
			if !strings.HasPrefix(name, "mcp_") || name == "mcp_tool_search" {
				continue
			}
			if tool, ok := deps.ToolLister.Get(name); ok {
				descs[name] = tool.Description()
				if p := tool.Parameters(); len(p) > 0 {
					params[name] = p
				}
			}
		}
		if len(descs) > 0 {
			mcpToolDescs = descs
		}
		if len(params) > 0 {
			mcpToolParams = params
		}
	}
	slog.Debug("preview_prompt.mcp_from_registry", "agent_id", ag.ID, "live_mcp_tools", len(mcpToolDescs))

	// Then supplement (or replace) with store-based MCP tools so that configured
	// MCP servers appear in the preview even when not currently loaded in the registry.
	slog.Debug("preview_prompt.mcp_lister_check", "agent_id", ag.ID, "mcp_lister_nil", deps.MCPLister == nil)
	if deps.MCPLister != nil {
		slog.Debug("preview_prompt: calling MCPLister.ListToolsForAgent", "agent_id", ag.ID, "user_id", userID)
		storeMCPTools, err := deps.MCPLister.ListToolsForAgent(ctx, ag.ID, userID)
		slog.Debug("preview_prompt.mcp_lister_result", "agent_id", ag.ID, "user_id", userID, "store_tools_count", len(storeMCPTools), "error", err)
		if err == nil && len(storeMCPTools) > 0 {
			if mcpToolDescs == nil {
				mcpToolDescs = make(map[string]string, len(storeMCPTools))
			}
			toolPolicyCfg := ag.ParseToolsConfig()
			for _, mt := range storeMCPTools {
				// MCP tool grant/access is already fully resolved by
				// ListToolsForAgent's own tool_allow/tool_deny per-server
				// logic above. Here we only need to catch the narrower case
				// of a literal-name deny (global or per-agent tools.deny
				// listing this exact tool name). We deliberately do NOT run
				// the full WouldAllow group-expansion pipeline: MCP tools are
				// only ever registered into ephemeral per-agent registry
				// clones at live-connection time (internal/mcp/manager_connect.go),
				// never into the shared/global registry used here, so
				// "group:mcp" (and any other group spec) can never resolve
				// correctly against toolRegistry in this connection-free
				// preview context. Passing reg=nil forces IsDenied to fall
				// back to a plain literal-name match with no group
				// expansion, which is exactly what's needed and is immune to
				// this class of registry-dependency bug.
				if deps.ToolPolicy != nil && deps.ToolPolicy.IsDenied(nil, mt.RegisteredName, toolPolicyCfg) {
					slog.Debug("preview_prompt.mcp_tool_denied_by_policy", "tool", mt.RegisteredName)
					continue
				}
				if _, alreadyPresent := mcpToolDescs[mt.RegisteredName]; !alreadyPresent {
					mcpToolDescs[mt.RegisteredName] = mt.Description
					slog.Debug("preview_prompt.mcp_tool_added", "tool", mt.RegisteredName, "has_desc", mt.Description != "")
				} else {
					slog.Debug("preview_prompt.mcp_tool_already_present", "tool", mt.RegisteredName)
				}
				// Cached parameter schema, when present, is only used when the
				// live registry did not already supply a real schema above.
				if _, alreadyHasParams := mcpToolParams[mt.RegisteredName]; !alreadyHasParams && len(mt.Parameters) > 0 {
					var schema map[string]any
					if jsonErr := json.Unmarshal(mt.Parameters, &schema); jsonErr == nil {
						if mcpToolParams == nil {
							mcpToolParams = make(map[string]map[string]any, len(storeMCPTools))
						}
						mcpToolParams[mt.RegisteredName] = schema
					} else {
						slog.Debug("preview_prompt.mcp_tool_params_unmarshal_failed", "tool", mt.RegisteredName, "error", jsonErr)
					}
				}
			}
		} else if err != nil {
			slog.Debug("preview_prompt.mcp_lister_error", "agent_id", ag.ID, "error", err)
		}
	}
	slog.Debug("preview_prompt.mcp_tool_descs_final", "agent_id", ag.ID, "total_mcp_tools", len(mcpToolDescs), "total_mcp_params", len(mcpToolParams))

	// --- Sandbox ---
	sandboxCfg := ag.ParseSandboxConfig()
	sandboxEnabled := sandboxCfg != nil && sandboxCfg.Mode != "" && sandboxCfg.Mode != "off"
	var sandboxContainerDir string
	if sandboxEnabled {
		sandboxContainerDir = "/workspace"
	}

	// --- Pinned skills ---
	var pinnedSummary string
	if pinnedSkills := ag.ParsePinnedSkills(); len(pinnedSkills) > 0 && deps.SkillsLoader != nil {
		pinnedSummary = deps.SkillsLoader.BuildPinnedSummary(ctx, pinnedSkills)
	}

	// --- Skills summary (BuildSummary + token count) ---
	var skillsSummary string
	if deps.SkillsLoader != nil {
		var skillAllowList []string
		if deps.SkillAccessStore != nil {
			if accessible, err := deps.SkillAccessStore.ListAccessible(ctx, ag.ID, userID); err == nil {
				skillAllowList = make([]string, 0, len(accessible))
				for _, sk := range accessible {
					skillAllowList = append(skillAllowList, sk.Slug)
				}
			} else {
				// On error: empty list (no skills). Preview is diagnostic; safer than showing all.
				skillAllowList = []string{}
			}
		}

		summary := deps.SkillsLoader.BuildSummary(ctx, skillAllowList)
		if summary != "" {
			tokens := tokencount.NewFallbackCounter().Count("claude-3", summary)
			if tokens <= skillInlineMaxTokens {
				skillsSummary = summary
			}
			// Over threshold → search-only mode (skillsSummary stays empty)
		}
	}

	// --- Provider contribution ---
	var providerContrib *providers.PromptContribution
	if deps.ProviderReg != nil && ag.Provider != "" {
		if p, err := deps.ProviderReg.Get(ctx, ag.Provider); err == nil {
			if pc, ok := p.(providers.PromptContributor); ok {
				providerContrib = pc.PromptContribution()
			}
		}
	}

	// --- Team + Delegation (none mode skips team entirely) ---
	orchMode := ResolveOrchestrationMode(ctx, ag.ID, deps.TeamStore, deps.AgentLinks)
	var isTeamCtx bool
	var teamMembers []store.TeamMemberData
	var teamWorkspace, teamGuidance string
	if mode != PromptNone && deps.TeamStore != nil {
		if team, err := deps.TeamStore.GetTeamForAgent(ctx, ag.ID); err == nil && team != nil {
			isTeamCtx = true
			if deps.DataDir != "" {
				teamWorkspace = filepath.Join(deps.DataDir, "teams", team.ID.String())
			}
			teamGuidance = defaultTeamGuidance()
			if members, err := deps.TeamStore.ListMembers(ctx, team.ID); err == nil {
				teamMembers = members
				// Inject virtual TEAM.md (same as pipeline resolver.go:190)
				contextFiles = append(contextFiles, bootstrap.ContextFile{
					Path:    bootstrap.TeamFile,
					Content: buildTeamMD(team, members, ag.ID),
				})
			}
		}
	}
	var delegateTargets []DelegateTargetEntry
	if deps.AgentLinks != nil && orchMode != ModeSpawn {
		if links, err := deps.AgentLinks.DelegateTargets(ctx, ag.ID); err == nil {
			for _, link := range links {
				delegateTargets = append(delegateTargets, DelegateTargetEntry{
					AgentKey:    link.TargetAgentKey,
					DisplayName: link.TargetDisplayName,
					Description: link.Description,
				})
			}
		}
	}

	// --- Tool definitions (schemas sent to LLM alongside the system prompt) ---
	var toolDefs []providers.ToolDefinition
	if deps.ToolLister != nil {
		for _, name := range toolNames {
			if tool, ok := deps.ToolLister.Get(name); ok {
				toolDefs = append(toolDefs, tools.ToProviderDef(tool))
			}
		}
		// Include alias definitions (LLM receives both canonical + aliases),
		// but only for aliases whose canonical tool survived the deny/gating
		// filters above (toolNames). Without this check, denied tools would
		// reappear in the schema via their alias name even though they were
		// correctly excluded from ToolNames/the main loop.
		toolNameSet := make(map[string]bool, len(toolNames))
		for _, n := range toolNames {
			toolNameSet[n] = true
		}
		for alias, canonical := range deps.ToolLister.Aliases() {
			if !toolNameSet[canonical] {
				continue
			}
			if tool, ok := deps.ToolLister.Get(canonical); ok {
				toolDefs = append(toolDefs, providers.ToolDefinition{
					Type: "function",
					Function: &providers.ToolFunctionSchema{
						Name:        alias,
						Description: tool.Description(),
						Parameters:  tool.Parameters(),
					},
				})
			}
		}
	}

	// --- MCP tool schemas (store-based preview, no live connection) ---
	// mcpToolParams carries the REAL cached parameter schema for a tool when
	// one is available (from a live registry tool, or from the connect-time
	// tool_cache captured in internal/mcp/manager_connect.go). This makes
	// preview parity with the live conversation path, which always sends
	// real schemas via BridgeTool.Parameters() (internal/mcp/bridge_tool.go).
	// Skip names already emitted via the main toolNames loop above (those
	// come from a live-loaded registry tool and already have a real schema).
	if len(mcpToolDescs) > 0 {
		alreadyEmitted := make(map[string]bool, len(toolNames))
		for _, n := range toolNames {
			alreadyEmitted[n] = true
		}
		mcpNames := make([]string, 0, len(mcpToolDescs))
		for name := range mcpToolDescs {
			if alreadyEmitted[name] {
				continue
			}
			mcpNames = append(mcpNames, name)
		}
		slices.Sort(mcpNames)
		for _, name := range mcpNames {
			params, ok := mcpToolParams[name]
			if !ok {
				// Genuine unknown-schema fallback: no live connection has ever
				// cached a real schema for this tool (e.g. server never
				// connected yet, or cache predates schema capture). This is
				// NOT the routine case — most tools reach here with a real
				// cached schema once the server has connected at least once.
				params = map[string]any{"type": "object"}
			}
			toolDefs = append(toolDefs, providers.ToolDefinition{
				Type: "function",
				Function: &providers.ToolFunctionSchema{
					Name:        name,
					Description: mcpToolDescs[name],
					Parameters:  params,
				},
			})
		}
	}

	// --- Build system prompt (same function as LLM pipeline) ---
	prompt := BuildSystemPrompt(SystemPromptConfig{
		AgentID:              ag.AgentKey,
		AgentUUID:            ag.ID.String(),
		DisplayName:          ag.DisplayName,
		Model:                ag.Model,
		Mode:                 mode,
		ToolNames:            toolNames,
		ContextFiles:         contextFiles,
		AgentType:            ag.AgentType,
		Workspace:            ag.Workspace,
		HasMemory:            true,
		HasSpawn:             slices.Contains(toolNames, "spawn"),
		HasSkillSearch:       slices.Contains(toolNames, "skill_search"),
		HasSkillManage:       ag.ParseSkillEvolve() && slices.Contains(toolNames, "skill_manage"),
		HasMCPToolSearch:     slices.Contains(toolNames, "mcp_tool_search"),
		HasKnowledgeGraph:    slices.Contains(toolNames, "knowledge_graph_search"),
		HasMemoryExpand:      slices.Contains(toolNames, "memory_expand"),
		SelfEvolve:           ag.ParseSelfEvolve(),
		ProviderType:         ag.Provider,
		ProviderContribution: providerContrib,
		ShellDenyGroups:      ag.ParseShellDenyGroups(),
		SandboxEnabled:       sandboxEnabled,
		SandboxContainerDir:  sandboxContainerDir,
		PinnedSkillsSummary:  pinnedSummary,
		SkillsSummary:        skillsSummary,
		MCPToolDescs:         mcpToolDescs,
		IsTeamContext:        isTeamCtx,
		TeamWorkspace:        teamWorkspace,
		TeamMembers:          teamMembers,
		TeamGuidance:         teamGuidance,
		DelegateTargets:      delegateTargets,
		OrchMode:             orchMode,
		// Runtime-only fields left at zero: Channel, ChannelType, ChatTitle,
		// PeerKind, OwnerIDs, ExtraPrompt, CredentialCLIContext, IsBootstrap,
		// SandboxWorkspaceAccess
	})
	slog.Debug("BuildPreviewPrompt: prompt preview built", "agent_id", ag.ID, "mcp_tools", len(mcpToolDescs), "prompt_len", len(prompt), "tool_defs", len(toolDefs))
	return PreviewResult{Prompt: prompt, ToolDefs: toolDefs}
}

// mergePreviewUserFiles overlays per-user files onto base agent-level files.
func mergePreviewUserFiles(ctx context.Context, as store.AgentStore, agentID uuid.UUID, base []bootstrap.ContextFile, userID string, mode PromptMode) []bootstrap.ContextFile {
	userFiles, err := as.GetUserContextFiles(ctx, agentID, userID)
	if err != nil || len(userFiles) == 0 {
		return base
	}
	userMap := make(map[string]string, len(userFiles))
	for _, uf := range userFiles {
		if uf.Content != "" {
			userMap[uf.FileName] = uf.Content
		}
	}
	if len(userMap) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base))
	var result []bootstrap.ContextFile
	for _, f := range base {
		name := filepath.Base(f.Path)
		if uc, ok := userMap[name]; ok {
			result = append(result, bootstrap.ContextFile{Path: f.Path, Content: uc})
		} else {
			result = append(result, f)
		}
		seen[name] = true
	}
	for _, uf := range userFiles {
		if seen[uf.FileName] || uf.Content == "" {
			continue
		}
		if allowlist := bootstrap.ModeAllowlist(string(mode)); allowlist != nil {
			if !allowlist[uf.FileName] {
				continue
			}
		}
		result = append(result, bootstrap.ContextFile{Path: uf.FileName, Content: uf.Content})
	}
	return result
}

// defaultTeamGuidance returns team member guidance for preview.
func defaultTeamGuidance() string {
	return "Use comment(type='blocker') to escalate blockers to the leader. " +
		"Use review to submit work for approval. " +
		"Use progress to report incremental status updates."
}

// fallbackPreviewToolNames used when tool registry is not available.
var fallbackPreviewToolNames = []string{
	"read_file", "write_file", "list_files", "edit", "exec",
	"memory_search", "memory_get", "spawn",
	"web_search", "web_fetch", "skill_search", "use_skill",
	"datetime", "cron",
}
