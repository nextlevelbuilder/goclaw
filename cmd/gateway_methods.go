package cmd

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func registerAllMethods(server *gateway.Server, agents *agent.Router, sessStore store.SessionStore, cronStore store.CronStore, pairingStore store.PairingStore, cfg *config.Config, cfgPath, workspace, dataDir string, msgBus *bus.MessageBus, execApprovalMgr *tools.ExecApprovalManager, agentStore store.AgentStore, skillStore store.SkillStore, configSecretsStore store.ConfigSecretsStore, teamStore store.TeamStore, contextFileInterceptor *tools.ContextFileInterceptor, logTee *gateway.LogTee, heartbeatStore store.HeartbeatStore) (*methods.PairingMethods, *methods.HeartbeatMethods) {
	router := server.Router()

	// Phase 1: Core methods
	methods.NewChatMethods(agents, sessStore, server.RateLimiter()).Register(router)
	methods.NewAgentsMethods(agents, cfg, cfgPath, workspace, agentStore, contextFileInterceptor, msgBus).Register(router)
	methods.NewSessionsMethods(sessStore, msgBus).Register(router)
	methods.NewConfigMethods(cfg, cfgPath, configSecretsStore, msgBus).Register(router)

	// Phase 2: Skills (uses SkillStore interface — PG or File)
	methods.NewSkillsMethods(skillStore).Register(router)

	// Phase 2: Cron (store created externally, shared with gateway)
	methods.NewCronMethods(cronStore, msgBus).Register(router)

	// Phase 2: Heartbeat
	heartbeatMethods := methods.NewHeartbeatMethods(heartbeatStore, msgBus)
	heartbeatMethods.Register(router)

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

	// Phase 4: Delegation history
	if teamStore != nil {
		methods.NewDelegationsMethods(teamStore).Register(router)
	}

	slog.Info("registered all RPC methods",
		"phase1", []string{"chat", "agents", "sessions", "config"},
		"phase2", []string{"skills", "cron", "heartbeat", "pairing", "usage", "exec_approval", "send"},
	)

	return pairingMethods, heartbeatMethods
}
