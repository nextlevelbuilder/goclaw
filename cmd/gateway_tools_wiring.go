package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/workstation"
	"github.com/nextlevelbuilder/goclaw/internal/workstation/security"
)

// wireExtraTools registers cron, heartbeat, session, message tools and aliases
// onto the tool registry after setupToolRegistry() and setupSkillsSystem() have run.
// Returns the heartbeat tool (needed for later wiring) and the hasMemory flag.
func wireExtraTools(
	pgStores *store.Stores,
	toolsReg *tools.Registry,
	msgBus *bus.MessageBus,
	workspace string,
	dataDir string,
	agentCfg config.AgentDefaults,
	globalSkillsDir string,
	builtinSkillsDir string,
	cronCommandEnabled bool,
) (heartbeatTool *tools.HeartbeatTool, hasMemory bool) {
	// web_search: tenant-scoped resolve requires stores + msgBus — register here.
	toolsReg.Register(tools.NewWebSearchTool(pgStores.ConfigSecrets, msgBus))
	slog.Info("web_search tool registered (tenant-scoped resolve)")

	// DateTime tool (precise time for cron scheduling, memory timestamps, etc.)
	toolsReg.Register(tools.NewDateTimeTool())
	toolsReg.Register(tools.NewWaitTool())

	// Cron tool (agent-facing)
	cronTool := tools.NewCronTool(pgStores.Cron)
	cronTool.SetProviderStore(pgStores.Providers)
	cronTool.SetCommandEnabled(cronCommandEnabled)
	toolsReg.Register(cronTool)
	slog.Info("cron tool registered")

	// Heartbeat tool (agent-facing)
	heartbeatTool = tools.NewHeartbeatTool(pgStores.Heartbeats, pgStores.ConfigPermissions)
	heartbeatTool.SetAgentStore(pgStores.Agents)
	toolsReg.Register(heartbeatTool)
	slog.Info("heartbeat tool registered")

	// Session tools (list, status, history, send)
	toolsReg.Register(tools.NewSessionsListTool())
	toolsReg.Register(tools.NewSessionStatusTool())
	toolsReg.Register(tools.NewSessionsHistoryTool())
	toolsReg.Register(tools.NewSessionsSendTool())

	// Message tool (send to channels)
	toolsReg.Register(tools.NewMessageTool(workspace, agentCfg.RestrictToWorkspace))
	// Send file tool (deliver existing workspace file as attachment)
	toolsReg.Register(tools.NewSendFileTool(workspace, agentCfg.RestrictToWorkspace))
	// Group members tool (list members in group chats)
	toolsReg.Register(tools.NewListGroupMembersTool())
	slog.Info("session + message + send_file tools registered")

	// Register legacy tool aliases (backward-compat names from policy.go).
	for alias, canonical := range tools.LegacyToolAliases() {
		toolsReg.RegisterAlias(alias, canonical)
	}

	// Register Claude Code tool aliases so Claude Code skills work without modification.
	for alias, canonical := range map[string]string{
		"Read":       "read_file",
		"Write":      "write_file",
		"Edit":       "edit",
		"Bash":       "exec",
		"WebFetch":   "web_fetch",
		"WebSearch":  "web_search",
		"Agent":      "spawn",
		"Skill":      "use_skill",
		"ToolSearch": "mcp_tool_search",
	} {
		toolsReg.RegisterAlias(alias, canonical)
	}
	slog.Info("tool aliases registered", "count", len(toolsReg.Aliases()))

	// Allow read_file and list_files to access skills directories and CLI workspaces.
	homeDir, _ := os.UserHomeDir()
	skillsAllowPaths := []string{globalSkillsDir, builtinSkillsDir, filepath.Join(dataDir, "tenants")}
	if homeDir != "" {
		skillsAllowPaths = append(skillsAllowPaths, filepath.Join(homeDir, ".agents", "skills"))
	}
	if pgStores.Skills != nil {
		skillsAllowPaths = append(skillsAllowPaths, pgStores.Skills.Dirs()...)
	}
	if readTool, ok := toolsReg.Get("read_file"); ok {
		if pa, ok := readTool.(tools.PathAllowable); ok {
			pa.AllowPaths(skillsAllowPaths...)
			pa.AllowPaths(filepath.Join(dataDir, "cli-workspaces"))
		}
	}
	if listTool, ok := toolsReg.Get("list_files"); ok {
		if pa, ok := listTool.(tools.PathAllowable); ok {
			pa.AllowPaths(skillsAllowPaths...)
		}
	}
	if sendFileTool, ok := toolsReg.Get("send_file"); ok {
		if pa, ok := sendFileTool.(tools.PathAllowable); ok {
			pa.AllowPaths(skillsAllowPaths...)
		}
	}

	// User-configured allowed paths (config agents.defaults.allowed_paths, for
	// cross-drive access on Windows and shared dirs outside the workspace).
	// Applied via a helper so cmd/gateway.go can re-apply it after
	// ApplySystemConfigs overlays system_configs['allowed_paths'] — see
	// applyUserAllowedPaths. Paths are validated per-request in resolvePath for
	// tenant isolation.
	applyUserAllowedPaths(toolsReg, agentCfg.AllowedPaths)

	// Memory tools are PG-backed; always available.
	hasMemory = true

	// Wire SessionStoreAware + BusAware on session tools
	for _, name := range []string{"sessions_list", "session_status", "sessions_history", "sessions_send"} {
		if t, ok := toolsReg.Get(name); ok {
			if sa, ok := t.(tools.SessionStoreAware); ok {
				sa.SetSessionStore(pgStores.Sessions)
			}
			if ba, ok := t.(tools.BusAware); ok {
				ba.SetMessageBus(msgBus)
			}
		}
	}
	// Wire BusAware on message tool
	if t, ok := toolsReg.Get("message"); ok {
		if ba, ok := t.(tools.BusAware); ok {
			ba.SetMessageBus(msgBus)
		}
	}

	return heartbeatTool, hasMemory
}

// fsAllowPathTools are the filesystem tools that honour user-configured allowed
// paths beyond the agent workspace (config agents.defaults.allowed_paths).
var fsAllowPathTools = []string{"read_file", "list_files", "write_file", "edit", "send_file"}

// applyUserAllowedPaths grants the user-configured allowed paths to the
// filesystem tools. ExpandHome resolves a leading "~".
//
// Called twice during startup: once while tools are wired (from config.json),
// and again from cmd/gateway.go after ApplySystemConfigs overlays
// system_configs['allowed_paths']. Tool wiring runs before that overlay, so
// without the re-apply DB-driven allowed paths never reach the tools — the
// AllowPaths analogue of the rate-limiter re-apply (#1111). Safe to call
// repeatedly: AllowPaths is additive and the prefix check is membership-based,
// so a duplicated prefix is harmless.
func applyUserAllowedPaths(toolsReg *tools.Registry, allowedPaths []string) {
	var paths []string
	for _, p := range allowedPaths {
		if expanded := config.ExpandHome(p); expanded != "" {
			paths = append(paths, expanded)
		}
	}
	if len(paths) == 0 {
		return
	}
	for _, name := range fsAllowPathTools {
		if t, ok := toolsReg.Get(name); ok {
			if pa, ok := t.(tools.PathAllowable); ok {
				pa.AllowPaths(paths...)
			}
		}
	}
}

// wireWorkstationTools registers workstation_exec and claude_remote tools (Standard edition only).
// Phase 6: wires the real AllowlistChecker permission check replacing the deny-all sentinel.
// Phase 7: wires the activity sink for exec audit logging.
//
// Security model (argv-exec, no sh -c):
//   - C1 fix: cmd is the binary name (argv[0]), not a shell command string — no shell injection possible.
//   - C2 fix: NFKC normalization applied before any check — collapses Unicode lookalikes.
//   - Default-deny: AllowlistChecker rejects any cmd not in workstation's allowlist.
//   - Rate limit: 30 exec/min per agent+workstation, 300/hr per workstation.
//
// Also subscribes to workstation update/delete events to keep BackendCache and
// AllowlistChecker cache consistent with the database.
func wireWorkstationTools(
	pgStores *store.Stores,
	toolsReg *tools.Registry,
	domainBus eventbus.DomainEventBus,
) func() {
	if edition.Current().Name != "standard" {
		return func() {}
	}
	if pgStores.Workstations == nil || pgStores.WorkstationLinks == nil {
		slog.Warn("workstation tools skipped: workstation stores not initialised")
		return func() {}
	}

	backendCache := workstation.NewBackendCache(pgStores.Workstations, 10*time.Minute)

	workstationExecTool := tools.NewWorkstationExecTool(
		pgStores.Workstations,
		pgStores.WorkstationLinks,
		backendCache,
		domainBus,
	)
	claudeRemoteTool := tools.NewClaudeRemoteTool(workstationExecTool)

	// Phase 6: wire real permission checker (AllowlistChecker + rate limiter).
	if pgStores.WorkstationPermissions != nil {
		allowlistChecker := security.NewAllowlistChecker(pgStores.WorkstationPermissions, 30*time.Second)
		rateLimiter := security.NewWorkstationRateLimiter()

		workstationExecTool.SetPermCheck(func(ctx context.Context, ws *store.Workstation, cmd string, args []string, env map[string]string) error {
			// Rate limit check first (cheap, no DB).
			agentID := store.AgentIDFromContext(ctx).String()
			if !rateLimiter.Allow(ws.TenantID, ws.ID, agentID) {
				locale := store.LocaleFromContext(ctx)
				return fmt.Errorf("%s", i18n.T(locale, i18n.MsgWorkstationRateLimit))
			}
			// Env blocklist check — rejects forbidden/sensitive env keys.
			if err := allowlistChecker.CheckEnv(ctx, ws, env); err != nil {
				return err
			}
			// Allowlist + input validation (NFKC normalize, NUL/CRLF, binary match).
			return allowlistChecker.Check(ctx, ws, cmd, args)
		})
		slog.Info("workstation tools registered (Standard edition; Phase 6 AllowlistChecker active)")

		// Invalidate allowlist cache on permission changes.
		if domainBus != nil {
			domainBus.Subscribe(eventbus.EventWorkstationPermChanged, func(_ context.Context, e eventbus.DomainEvent) error {
				if id, err := uuid.Parse(e.SourceID); err == nil {
					allowlistChecker.Invalidate(id)
					slog.Debug("workstation allowlist cache invalidated", "workstation_id", id)
				}
				return nil
			})
		}
	} else {
		slog.Warn("workstation tools registered with deny-all: WorkstationPermissions store not initialised")
	}

	toolsReg.Register(workstationExecTool)
	toolsReg.Register(claudeRemoteTool)

	// Subscribe to workstation update/delete events to evict stale BackendCache entries.
	if domainBus != nil {
		domainBus.Subscribe(eventbus.EventWorkstationUpdated, func(_ context.Context, e eventbus.DomainEvent) error {
			if id, err := uuid.Parse(e.SourceID); err == nil {
				backendCache.Invalidate(id)
				slog.Debug("workstation backend cache invalidated on update", "workstation_id", id)
			}
			return nil
		})
		domainBus.Subscribe(eventbus.EventWorkstationDeleted, func(_ context.Context, e eventbus.DomainEvent) error {
			if id, err := uuid.Parse(e.SourceID); err == nil {
				backendCache.Invalidate(id)
				slog.Debug("workstation backend cache invalidated on delete", "workstation_id", id)
			}
			return nil
		})

		// Phase 7: wire activity audit sink (persists exec done events + nightly prune).
		if pgStores.WorkstationActivity != nil {
			stopSink := workstation.WireActivitySink(domainBus, pgStores.WorkstationActivity)
			slog.Info("workstation activity audit sink registered")
			return func() {
				stopSink()
				pgStores.WorkstationActivity.Stop()
			}
		}
	}
	return func() {}
}
