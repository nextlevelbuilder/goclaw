package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// ResolverDeps holds shared dependencies for the managed-mode agent resolver.
type ResolverDeps struct {
	AgentStore     store.AgentStore
	ProviderReg    *providers.Registry
	Bus            bus.EventPublisher
	Sessions       store.SessionStore
	Tools          *tools.Registry
	ToolPolicy     *tools.PolicyEngine
	Skills         *skills.Loader
	HasMemory      bool
	OnEvent        func(AgentEvent)
	TraceCollector *tracing.Collector

	// Per-user file seeding + dynamic context loading (managed mode)
	EnsureUserFiles   EnsureUserFilesFunc
	ContextFileLoader ContextFileLoaderFunc
	BootstrapCleanup  BootstrapCleanupFunc

	// Security
	InjectionAction string // "log", "warn", "block", "off"
	MaxMessageChars int

	// Global defaults (from config.json) — per-agent DB overrides take priority
	CompactionCfg          *config.CompactionConfig
	ContextPruningCfg      *config.ContextPruningConfig
	SandboxEnabled         bool
	SandboxContainerDir    string
	SandboxWorkspaceAccess string

	// Dynamic custom tools (managed mode)
	DynamicLoader *tools.DynamicToolLoader // nil if not managed

	// Inter-agent delegation (managed mode)
	AgentLinkStore store.AgentLinkStore // nil if not managed or no links

	// Agent teams (managed mode)
	TeamStore store.TeamStore // nil if not managed or no teams

	// Builtin tool settings (managed mode)
	BuiltinToolStore store.BuiltinToolStore // nil if not managed

	// MCP server store (managed mode) — for per-agent MCP tool loading
	MCPStore store.MCPServerStore // nil if not managed or no MCP

	// Skill access store (managed mode) — for per-agent skill visibility filtering
	SkillAccessStore store.SkillAccessStore // nil if not managed

	// Group file writer cache (managed mode)
	GroupWriterCache *store.GroupWriterCache
}

// NewManagedResolver creates a ResolverFunc that builds Loops from DB agent data.
// This is the core of managed mode: agents are defined in Postgres, not config.json.
func NewManagedResolver(deps ResolverDeps) ResolverFunc {
	return func(agentKey string) (Agent, error) {
		ctx := context.Background()

		// Support lookup by UUID (e.g. from cron jobs that store agent_id as UUID)
		var ag *store.AgentData
		var err error
		if id, parseErr := uuid.Parse(agentKey); parseErr == nil {
			ag, err = deps.AgentStore.GetByID(ctx, id)
		} else {
			ag, err = deps.AgentStore.GetByKey(ctx, agentKey)
		}
		if err != nil {
			return nil, fmt.Errorf("agent not found: %s", agentKey)
		}

		// Resolve provider
		provider, err := deps.ProviderReg.Get(ag.Provider)
		if err != nil {
			// Fallback to any available provider
			names := deps.ProviderReg.List()
			if len(names) == 0 {
				return nil, fmt.Errorf("no providers configured for agent %s", agentKey)
			}
			provider, _ = deps.ProviderReg.Get(names[0])
			slog.Warn("agent provider not found, using fallback",
				"agent", agentKey, "wanted", ag.Provider, "using", names[0])
			if tl := ag.ParseThinkingLevel(); tl != "" && tl != "off" {
				slog.Warn("agent thinking may not be supported by fallback provider",
					"agent", agentKey, "thinking_level", tl,
					"wanted_provider", ag.Provider, "fallback_provider", names[0])
			}
		}

		if provider == nil {
			return nil, fmt.Errorf("no provider available for agent %s", agentKey)
		}

		// Load bootstrap files from DB
		contextFiles := bootstrap.LoadFromStore(ctx, deps.AgentStore, ag.ID)

		// Inject DELEGATION.md from delegation links (only if not already present in DB).
		// Uses DELEGATION.md (not AGENTS.md) to avoid collision with per-user AGENTS.md
		// which contains workspace instructions for open agents.
		hasDelegation := false
		if deps.AgentLinkStore != nil {
			hasDelegationMD := false
			for _, cf := range contextFiles {
				if cf.Path == bootstrap.DelegationFile {
					hasDelegationMD = true
					break
				}
			}
			if !hasDelegationMD {
				if allTargets, err := deps.AgentLinkStore.DelegateTargets(ctx, ag.ID); err == nil && len(allTargets) > 0 {
					// Exclude auto-created team links — team members coordinate via
					// team_tasks/team_message, not delegate. Only explicitly created
					// links trigger DELEGATION.md.
					targets := filterManualLinks(allTargets)
					if len(targets) > 0 && len(targets) <= 15 {
						// Static list: all targets directly
						hasDelegation = true
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.DelegationFile,
							Content: buildDelegateAgentsMD(targets),
						})
					} else if len(targets) > 15 {
						// Too many targets: instruct agent to use delegate_search tool
						hasDelegation = true
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.DelegationFile,
							Content: buildDelegateSearchInstruction(len(targets)),
						})
					}
				}
			} else {
				hasDelegation = true
			}
		}

		// Inject TEAM.md for all team members (lead + members) so every agent
		// knows the team workflow: create/claim/complete tasks via team_tasks tool.
		hasTeam := false
		if deps.TeamStore != nil {
			hasTeamMD := false
			for _, cf := range contextFiles {
				if cf.Path == bootstrap.TeamFile {
					hasTeamMD = true
					break
				}
			}
			if !hasTeamMD {
				if team, err := deps.TeamStore.GetTeamForAgent(ctx, ag.ID); err == nil && team != nil {
					if members, err := deps.TeamStore.ListMembers(ctx, team.ID); err == nil {
						hasTeam = true
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.TeamFile,
							Content: buildTeamMD(team, members, ag.ID),
						})
					}
				}
			} else {
				hasTeam = true
			}
		}

		// Inject negative context so the model doesn't waste iterations probing
		// unavailable capabilities (team_tasks, delegate_search, etc.).
		// Note: team agents have delegation targets via team links (TEAM.md),
		// so only inject "no delegation" when both hasDelegation and hasTeam are false.
		if !hasTeam || (!hasDelegation && !hasTeam) {
			var notes []string
			if !hasTeam {
				notes = append(notes, "You are NOT part of any team. Do not use team_tasks or team_message tools.")
			}
			if !hasDelegation && !hasTeam {
				notes = append(notes, "You have NO delegation targets. Do not use spawn with agent parameter or delegate_search tools.")
			}
			contextFiles = append(contextFiles, bootstrap.ContextFile{
				Path:    bootstrap.AvailabilityFile,
				Content: strings.Join(notes, "\n"),
			})
		}

		contextWindow := ag.ContextWindow
		if contextWindow <= 0 {
			contextWindow = 200000
		}
		maxIter := ag.MaxToolIterations
		if maxIter <= 0 {
			maxIter = 20
		}

		// Per-agent config overrides (fallback to global defaults from config.json)
		compactionCfg := deps.CompactionCfg
		if c := ag.ParseCompactionConfig(); c != nil {
			compactionCfg = c
		}
		contextPruningCfg := deps.ContextPruningCfg
		if c := ag.ParseContextPruning(); c != nil {
			contextPruningCfg = c
		}
		sandboxEnabled := deps.SandboxEnabled
		sandboxContainerDir := deps.SandboxContainerDir
		sandboxWorkspaceAccess := deps.SandboxWorkspaceAccess
		if c := ag.ParseSandboxConfig(); c != nil {
			resolved := c.ToSandboxConfig()
			sandboxContainerDir = resolved.ContainerWorkdir()
			sandboxWorkspaceAccess = string(resolved.WorkspaceAccess)
		}

		// Expand ~ in workspace path and ensure directory exists
		workspace := ag.Workspace
		if workspace != "" {
			workspace = config.ExpandHome(workspace)
			if !filepath.IsAbs(workspace) {
				workspace, _ = filepath.Abs(workspace)
			}
			if err := os.MkdirAll(workspace, 0755); err != nil {
				slog.Warn("failed to create agent workspace directory", "workspace", workspace, "agent", agentKey, "error", err)
			}
		}

		// Per-agent custom tools (clone registry if agent has custom tools)
		toolsReg := deps.Tools
		if deps.DynamicLoader != nil {
			if agentReg, err := deps.DynamicLoader.LoadForAgent(ctx, deps.Tools, ag.ID); err != nil {
				slog.Warn("failed to load custom tools", "agent", agentKey, "error", err)
			} else if agentReg != nil {
				toolsReg = agentReg
			}
		}

		// Per-agent MCP servers: connect to granted MCP servers and register their tools.
		// Uses a per-agent MCP Manager that queries the MCPServerStore for accessible servers.
		//
		// IMPORTANT: Always clone the registry before MCP registration to prevent
		// cross-agent tool leaks. Without cloning, MCP BridgeTools registered for
		// one agent pollute the shared deps. Tools and become visible to ALL agents
		// (even those without MCP grants), because FilterTools reads from registry.List().
		hasMCPTools := false
		if deps.MCPStore != nil {
			if toolsReg == deps.Tools {
				toolsReg = deps.Tools.Clone()
			}
			mcpMgr := mcpbridge.NewManager(toolsReg, mcpbridge.WithStore(deps.MCPStore))
			if err := mcpMgr.LoadForAgent(ctx, ag.ID, ""); err != nil {
				slog.Warn("failed to load MCP servers for agent", "agent", agentKey, "error", err)
			} else {
				toolNames := mcpMgr.ToolNames()
				if len(toolNames) > 0 {
					hasMCPTools = true
					slog.Info("mcp.agent.tools_loaded", "agent", agentKey, "tools", len(toolNames))
				}
			}
		}

		// Per-agent memory: enabled if global memory manager exists AND
		// per-agent config doesn't explicitly disable it.
		hasMemory := deps.HasMemory
		if mc := ag.ParseMemoryConfig(); mc != nil && mc.Enabled != nil {
			if !*mc.Enabled {
				hasMemory = false
			}
		}

		// Load global builtin tool settings from DB (for settings cascade)
		var builtinSettings tools.BuiltinToolSettings
		if deps.BuiltinToolStore != nil {
			if allTools, err := deps.BuiltinToolStore.List(ctx); err == nil {
				builtinSettings = make(tools.BuiltinToolSettings, len(allTools))
				for _, t := range allTools {
					if len(t.Settings) > 0 && string(t.Settings) != "{}" {
						builtinSettings[t.Name] = []byte(t.Settings)
					}
				}
			}
		}

		// Managed mode: filter skills by visibility + agent grants.
		// Only public skills and explicitly granted internal skills appear in the system prompt.
		var skillAllowList []string
		if deps.SkillAccessStore != nil {
			if accessible, err := deps.SkillAccessStore.ListAccessible(ctx, ag.ID, ""); err == nil {
				skillAllowList = make([]string, 0, len(accessible))
				for _, sk := range accessible {
					skillAllowList = append(skillAllowList, sk.Slug)
				}
				slog.Debug("skill visibility filter", "agent", agentKey, "accessible", len(skillAllowList))
			} else {
				slog.Warn("failed to load accessible skills, falling back to all", "agent", agentKey, "error", err)
				// nil = fallback to all (better than blocking all skills)
			}
		}

		loop := NewLoop(LoopConfig{
			ID:                     ag.AgentKey,
			AgentUUID:              ag.ID,
			AgentType:              ag.AgentType,
			Provider:               provider,
			Model:                  ag.Model,
			ContextWindow:          contextWindow,
			MaxIterations:          maxIter,
			Workspace:              workspace,
			Bus:                    deps.Bus,
			Sessions:               deps.Sessions,
			Tools:                  toolsReg,
			ToolPolicy:             deps.ToolPolicy,
			AgentToolPolicy:        agentToolPolicyWithMCP(ag.ParseToolsConfig(), hasMCPTools),
			SkillsLoader:           deps.Skills,
			SkillAllowList:         skillAllowList,
			HasMemory:              hasMemory,
			ContextFiles:           contextFiles,
			EnsureUserFiles:        deps.EnsureUserFiles,
			ContextFileLoader:      deps.ContextFileLoader,
			BootstrapCleanup:       deps.BootstrapCleanup,
			OnEvent:                deps.OnEvent,
			TraceCollector:         deps.TraceCollector,
			InjectionAction:        deps.InjectionAction,
			MaxMessageChars:        deps.MaxMessageChars,
			CompactionCfg:          compactionCfg,
			ContextPruningCfg:      contextPruningCfg,
			SandboxEnabled:         sandboxEnabled,
			SandboxContainerDir:    sandboxContainerDir,
			SandboxWorkspaceAccess: sandboxWorkspaceAccess,
			BuiltinToolSettings:    builtinSettings,
			ThinkingLevel:          ag.ParseThinkingLevel(),
			GroupWriterCache:       deps.GroupWriterCache,
			TeamStore:              deps.TeamStore,
		})

		slog.Info("resolved agent from DB", "agent", agentKey, "model", ag.Model, "provider", ag.Provider)
		return loop, nil
	}
}

// InvalidateAgent removes an agent from the router cache, forcing re-resolution.
// Used when agent config is updated via API.
func (r *Router) InvalidateAgent(agentKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, agentKey)
	slog.Debug("invalidated agent cache", "agent", agentKey)
}

// InvalidateAll clears the entire agent cache, forcing all agents to re-resolve.
// Used when global tools change (custom tools reload).
func (r *Router) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents = make(map[string]*agentEntry)
	slog.Debug("invalidated all agent caches")
}

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
