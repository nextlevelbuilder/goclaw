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
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// ResolverDeps holds shared dependencies for the managed-mode agent resolver.
type ResolverDeps struct {
	AgentStore  store.AgentStore
	ProviderReg *providers.Registry
	Bus         bus.EventPublisher
	Sessions    store.SessionStore
	Tools       *tools.Registry
	ToolPolicy  *tools.PolicyEngine
	Skills      *skills.Loader
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
		}

		if provider == nil {
			return nil, fmt.Errorf("no provider available for agent %s", agentKey)
		}

		// Load bootstrap files from DB
		contextFiles := bootstrap.LoadFromStore(ctx, deps.AgentStore, ag.ID)

		// Inject DELEGATION.md from delegation links (only if not already present in DB).
		// Uses DELEGATION.md (not AGENTS.md) to avoid collision with per-user AGENTS.md
		// which contains workspace instructions for open agents.
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
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.DelegationFile,
							Content: buildDelegateAgentsMD(targets),
						})
					} else if len(targets) > 15 {
						// Too many targets: instruct agent to use delegate_search tool
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.DelegationFile,
							Content: buildDelegateSearchInstruction(len(targets)),
						})
					}
				}
			}
		}

		// Inject TEAM.md for all team members (lead + members) so every agent
		// knows the team workflow: create/claim/complete tasks via team_tasks tool.
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
						contextFiles = append(contextFiles, bootstrap.ContextFile{
							Path:    bootstrap.TeamFile,
							Content: buildTeamMD(team, members, ag.ID),
						})
					}
				}
			}
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

		loop := NewLoop(LoopConfig{
			ID:                ag.AgentKey,
			AgentUUID:         ag.ID,
			AgentType:         ag.AgentType,
			Provider:          provider,
			Model:             ag.Model,
			ContextWindow:     contextWindow,
			MaxIterations:     maxIter,
			Workspace:         workspace,
			Bus:               deps.Bus,
			Sessions:          deps.Sessions,
			Tools:             toolsReg,
			ToolPolicy:        deps.ToolPolicy,
			AgentToolPolicy:   ag.ParseToolsConfig(),
			SkillsLoader:      deps.Skills,
			HasMemory:         hasMemory,
			IdentityName:      ag.DisplayName, // managed mode: use display name as identity
			ContextFiles:      contextFiles,
			EnsureUserFiles:   deps.EnsureUserFiles,
			ContextFileLoader: deps.ContextFileLoader,
			BootstrapCleanup:  deps.BootstrapCleanup,
			OnEvent:           deps.OnEvent,
			TraceCollector:    deps.TraceCollector,
			InjectionAction:   deps.InjectionAction,
			MaxMessageChars:        deps.MaxMessageChars,
			CompactionCfg:          compactionCfg,
			ContextPruningCfg:      contextPruningCfg,
			SandboxEnabled:         sandboxEnabled,
			SandboxContainerDir:    sandboxContainerDir,
			SandboxWorkspaceAccess: sandboxWorkspaceAccess,
			BuiltinToolSettings:    builtinSettings,
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
	sb.WriteString("You have the `delegate` tool available. Use it to delegate tasks to other specialized agents.\n")
	sb.WriteString("The agent list below is complete and authoritative — answer questions about available agents directly from it.\n")
	sb.WriteString("Only use `delegate` when you need to actually assign work, not to check who is available.\n\n")
	sb.WriteString("## Available Agents\n")

	for _, t := range targets {
		sb.WriteString(fmt.Sprintf("\n### %s", t.TargetAgentKey))
		if t.TargetDisplayName != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", t.TargetDisplayName))
		}
		sb.WriteString("\n")
		if t.TargetDescription != "" {
			sb.WriteString(t.TargetDescription + "\n")
		}
		sb.WriteString(fmt.Sprintf("→ `delegate(agent=\"%s\", task=\"describe the task\")`\n", t.TargetAgentKey))
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

You have the `+"`delegate`"+` and `+"`delegate_search`"+` tools available.
Do NOT look for delegation info on disk — it is provided here.

You have access to %d specialized agents. To find the right one:

1. `+"`delegate_search(query=\"your keywords\")`"+` — search agents by expertise
2. `+"`delegate(agent=\"agent-key\", task=\"describe the task\")`"+` — delegate the task

Example:
- User asks about billing → `+"`delegate_search(query=\"billing payment\")`"+` → `+"`delegate(agent=\"billing-agent\", task=\"...\")`"+`

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
		sb.WriteString("**MANDATORY**: ALWAYS use `team_tasks` to track work. NEVER call `delegate` without a task.\n\n")
		sb.WriteString("Every delegation MUST follow these 2 steps:\n")
		sb.WriteString("1. `team_tasks` action=create, subject=<brief title> → returns task_id\n")
		sb.WriteString("2. `delegate` agent=<member>, task=<instructions>, team_task_id=<the task_id from step 1>\n\n")
		sb.WriteString("The system ENFORCES this — delegation without team_task_id will be rejected.\n")
		sb.WriteString("The task auto-completes when delegation finishes.\n\n")
		sb.WriteString("`team_tasks` actions:\n")
		sb.WriteString("- action=list → active tasks (pending/in_progress/blocked), no results shown\n")
		sb.WriteString("- action=list, status=all → all tasks including completed\n")
		sb.WriteString("- action=get, task_id=<id> → full task detail with result\n")
		sb.WriteString("- action=search, query=<text> → search tasks by subject/description\n")
		sb.WriteString("- action=complete, task_id=<id>, result=<summary> → manually complete a task\n\n")
		sb.WriteString("Use `team_message` to send updates to team members.\n\n")
		sb.WriteString("For simple questions about team composition, answer directly from the member list above.\n")
	} else {
		sb.WriteString("As a member, when you receive a delegated task, just do the work.\n")
		sb.WriteString("Task completion is handled automatically by the system.\n\n")
		sb.WriteString("`team_tasks` actions:\n")
		sb.WriteString("- action=list → check team task board (active tasks)\n")
		sb.WriteString("- action=get, task_id=<id> → read a completed task's full result\n")
		sb.WriteString("- action=search, query=<text> → search tasks\n\n")
		sb.WriteString("Use `team_message` to send updates to your team lead.\n\n")
		sb.WriteString("For simple questions about team composition, answer directly from the member list above.\n")
	}

	return sb.String()
}
