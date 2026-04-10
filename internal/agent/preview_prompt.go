package agent

import (
	"context"
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
	SkillsLoader interface {
		BuildPinnedSummary(ctx context.Context, names []string) string
		BuildSummary(ctx context.Context, allowList []string) string
	}
	DataDir string // for team workspace path construction
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

	// --- Basic agent tool policy: deny-only (Allow/AlsoAllow/ByProvider not applied).
	// Full PolicyEngine requires runtime state (provider name, channel) not available in preview. ---
	if toolPolicy := ag.ParseToolsConfig(); toolPolicy != nil && len(toolPolicy.Deny) > 0 {
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

	// --- MCP tool descriptions (matches loop_history_supplement.go:44-58) ---
	var mcpToolDescs map[string]string
	if deps.ToolLister != nil {
		descs := make(map[string]string)
		for _, name := range toolNames {
			if !strings.HasPrefix(name, "mcp_") || name == "mcp_tool_search" {
				continue
			}
			if tool, ok := deps.ToolLister.Get(name); ok {
				descs[name] = tool.Description()
			}
		}
		if len(descs) > 0 {
			mcpToolDescs = descs
		}
	}

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
		// Include alias definitions (LLM receives both canonical + aliases)
		for alias, canonical := range deps.ToolLister.Aliases() {
			if tool, ok := deps.ToolLister.Get(canonical); ok {
				toolDefs = append(toolDefs, providers.ToolDefinition{
					Type: "function",
					Function: providers.ToolFunctionSchema{
						Name:        alias,
						Description: tool.Description(),
						Parameters:  tool.Parameters(),
					},
				})
			}
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
