package cmd

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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
) (heartbeatTool *tools.HeartbeatTool, hasMemory bool) {
	// DateTime tool (precise time for cron scheduling, memory timestamps, etc.)
	toolsReg.Register(tools.NewDateTimeTool())

	// Cron tool (agent-facing)
	toolsReg.Register(tools.NewCronTool(pgStores.Cron))
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
	// Group members tool (list members in group chats)
	toolsReg.Register(tools.NewListGroupMembersTool())
	slog.Info("session + message tools registered")

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
