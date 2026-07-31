package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func registerAllMethods(server *gateway.Server, agents *agent.Router, sessStore store.SessionStore, cronStore store.CronStore, pairingStore store.PairingStore, cfg *config.Config, cfgPath, workspace, dataDir string, msgBus *bus.MessageBus, execApprovalMgr *tools.ExecApprovalManager, agentStore store.AgentStore, skillStore store.SkillStore, configSecretsStore store.ConfigSecretsStore, teamStore store.TeamStore, contextFileInterceptor *tools.ContextFileInterceptor, logTee *gateway.LogTee, heartbeatStore store.HeartbeatStore, configPermStore store.ConfigPermissionStore, sysConfigStore store.SystemConfigStore, tenantStore store.TenantStore, skillTenantCfgStore store.SkillTenantConfigStore, audioMgr *audio.Manager, reminderStore store.ReminderStore, subagentTaskStore store.SubagentTaskStore, cliConnStore store.CLIConnectionStore, workflowStore store.WorkflowStore, cliChat *methods.CLIChat) (*methods.PairingMethods, *methods.HeartbeatMethods, *methods.ChatMethods) {
	router := server.Router()

	// Phase 1: Core methods
	chatMethods := methods.NewChatMethods(agents, sessStore, cfg, server.RateLimiter(), msgBus)
	chatMethods.SetAudioManager(audioMgr) // Wire TTS auto-apply for WS responses
	// Interactive CLI chat: chat.send early-branches to this for a "cli:" session
	// key. Nil on a deployment without a sandbox / connection catalogue, in which
	// case such a key gets a clear "not available" error.
	chatMethods.SetCLIChat(cliChat)
	chatMethods.Register(router)
	methods.NewAgentsMethods(agents, cfg, cfgPath, workspace, agentStore, contextFileInterceptor, msgBus).Register(router)
	sessionsMethods := methods.NewSessionsMethods(sessStore, agents, msgBus, cfg)
	// Migration 000065 wiring: lets sessions.preview JOIN persisted spawn
	// ToolCalls back to their structured subagent task rows so the website
	// rebuilds the nested mini-chat after page reload. nil-safe at the
	// callsite — preview falls back to history-only when the store is absent.
	if subagentTaskStore != nil {
		sessionsMethods.SetSubagentTaskStore(subagentTaskStore)
	}
	sessionsMethods.Register(router)
	configMethods := methods.NewConfigMethods(cfg, cfgPath, configSecretsStore, msgBus)
	if sysConfigStore != nil {
		configMethods.SetSystemConfigSync(func(ctx context.Context, c *config.Config) {
			// Only sync config for the current tenant (from request context)
			seedConfigForContext(ctx, sysConfigStore, c, false) // onlyMissing=false → upsert
			// Trigger readback via bus event with fresh context (request ctx may be canceled)
			if msgBus != nil {
				freshCtx := store.WithTenantID(context.Background(), store.TenantIDFromContext(ctx))
				msgBus.Broadcast(bus.Event{Name: bus.TopicSystemConfigChanged, Payload: freshCtx})
			}
		})
	}
	configMethods.Register(router)

	// Phase 2: Skills (uses SkillStore interface — PG or File).
	// tenantStore lets handleList resolve the caller's role for the
	// per-user visibility filter (see ListSkillsForUser).
	methods.NewSkillsMethods(skillStore, skillTenantCfgStore, tenantStore).Register(router)

	// Phase 2: Cron (store created externally, shared with gateway)
	methods.NewCronMethods(cronStore, msgBus, cfg).Register(router)

	// Phase 2: Reminders — DB-backed inbox for cron-delivered messages
	if reminderStore != nil {
		methods.NewRemindersMethods(reminderStore).Register(router)
	}

	// Phase 2: Heartbeat
	heartbeatMethods := methods.NewHeartbeatMethods(heartbeatStore, msgBus)
	// Wire cache-aware resolver so heartbeat can accept agent_key or UUID
	// without a DB roundtrip on the hot path when the agent is router-cached.
	heartbeatMethods.SetAgentRouter(agents)
	heartbeatMethods.Register(router)

	// Phase 2: Config permissions
	cfgPerms := methods.NewConfigPermissionsMethods(configPermStore, agentStore)
	cfgPerms.SetAgentRouter(agents)
	cfgPerms.Register(router)

	// Phase 2: Pairing (store created externally, shared with channel manager).
	// OnApprove callback is set later by the caller after channel manager is created.
	pairingMethods := methods.NewPairingMethods(pairingStore, msgBus, server.RateLimiter())
	pairingMethods.Register(router)

	// Phase 2: Usage (queries SessionStore for real token data)
	methods.NewUsageMethods(sessStore).Register(router)

	// Phase 2: Exec approval (always registered — returns empty when manager is nil)
	methods.NewExecApprovalMethods(execApprovalMgr, msgBus).Register(router)

	// Phase 2: Tenant-level CLI connections (migration 000082). Always
	// registered — connections.list returns an empty list and the writes report
	// "not available" when the store is nil.
	connectionsMethods := methods.NewConnectionsMethods(cliConnStore, msgBus)
	// connections.chat.open reports whether an interactive conversation can
	// actually be served on this deployment, rather than handing back a session
	// key that chat.send would then refuse.
	connectionsMethods.SetCLIChat(cliChat)
	connectionsMethods.Register(router)

	// Phase 2: Authored workflows (migration 000083). Always registered — the
	// handlers report "not available on this deployment" when the store is nil,
	// which is what a build without the migration applied looks like.
	methods.NewWorkflowsMethods(workflowStore).Register(router)

	// Phase 2: Send (outbound message routing)
	methods.NewSendMethods(msgBus).Register(router)

	// Phase 3: Live log tailing
	methods.NewLogsMethods(logTee).Register(router)

	slog.Info("registered all RPC methods",
		"phase1", []string{"chat", "agents", "sessions", "config"},
		"phase2", []string{"skills", "cron", "heartbeat", "pairing", "usage", "exec_approval", "send"},
	)

	return pairingMethods, heartbeatMethods, chatMethods
}
