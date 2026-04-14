package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func registerAllMethods(server *gateway.Server, agents *agent.Router, sessStore store.SessionStore, cronStore store.CronStore, pairingStore store.PairingStore, cfg *config.Config, cfgPath, workspace, dataDir string, msgBus *bus.MessageBus, execApprovalMgr *tools.ExecApprovalManager, agentStore store.AgentStore, skillStore store.SkillStore, configSecretsStore store.ConfigSecretsStore, teamStore store.TeamStore, contextFileInterceptor *tools.ContextFileInterceptor, logTee *gateway.LogTee, heartbeatStore store.HeartbeatStore, configPermStore store.ConfigPermissionStore, sysConfigStore store.SystemConfigStore, tenantStore store.TenantStore, skillTenantCfgStore store.SkillTenantConfigStore) (*methods.PairingMethods, *methods.HeartbeatMethods, *methods.ChatMethods) {
	router := server.Router()

	// Phase 1: Core methods
	chatMethods := methods.NewChatMethods(agents, sessStore, cfg, server.RateLimiter(), msgBus)
	chatMethods.Register(router)
	methods.NewAgentsMethods(agents, cfg, cfgPath, workspace, agentStore, contextFileInterceptor, msgBus).Register(router)
	methods.NewSessionsMethods(sessStore, msgBus, cfg).Register(router)
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

	// Phase 2: Skills (uses SkillStore interface — PG or File)
	methods.NewSkillsMethods(skillStore, skillTenantCfgStore).Register(router)

	// Phase 2: Cron (store created externally, shared with gateway)
	methods.NewCronMethods(cronStore, msgBus, cfg).Register(router)

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
