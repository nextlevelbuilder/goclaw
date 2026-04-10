package cmd

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/heartbeat"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// makeHeartbeatRunFn creates a function that routes a heartbeat run through the scheduler's cron lane.
func makeHeartbeatRunFn(sched *scheduler.Scheduler) func(ctx context.Context, req agent.RunRequest) <-chan scheduler.RunOutcome {
	return func(ctx context.Context, req agent.RunRequest) <-chan scheduler.RunOutcome {
		return sched.Schedule(ctx, scheduler.LaneCron, req)
	}
}

// startCronAndHeartbeat starts the cron service and heartbeat ticker, wires the heartbeat
// wake function to the tool + RPC methods, and sets the adaptive token estimate function.
// Returns the heartbeat ticker (needed by lifecycle for shutdown).
func startCronAndHeartbeat(
	pgStores *store.Stores,
	server *gateway.Server,
	sched *scheduler.Scheduler,
	msgBus *bus.MessageBus,
	providerRegistry *providers.Registry,
	channelMgr *channels.Manager,
	cfg *config.Config,
	heartbeatTool *tools.HeartbeatTool,
	heartbeatMethods *methods.HeartbeatMethods,
) *heartbeat.Ticker {
	// Start cron service with job handler (routes through scheduler's cron lane)
	pgStores.Cron.SetOnJob(makeCronJobHandler(sched, msgBus, cfg, channelMgr, pgStores.Sessions, pgStores.Agents))
	pgStores.Cron.SetOnEvent(func(event store.CronEvent) {
		server.BroadcastEvent(*protocol.NewEvent(protocol.EventCron, event))
	})
	if err := pgStores.Cron.Start(); err != nil {
		slog.Warn("cron service failed to start", "error", err)
	}

	// Start heartbeat ticker (routes through scheduler's cron lane)
	heartbeatTicker := heartbeat.NewTicker(heartbeat.TickerConfig{
		Store:         pgStores.Heartbeats,
		Agents:        pgStores.Agents,
		Sessions:      pgStores.Sessions,
		ProviderStore: pgStores.Providers,
		ProviderReg:   providerRegistry,
		MsgBus:        msgBus,
		Sched:         sched,
		RunAgent:      makeHeartbeatRunFn(sched),
	})
	heartbeatTicker.SetOnEvent(func(event store.HeartbeatEvent) {
		server.BroadcastEvent(*protocol.NewEvent(protocol.EventHeartbeat, event))
	})
	heartbeatTicker.Start()

	// Wire heartbeat wake function to tool + RPC + cron wakeMode
	heartbeatTool.SetWakeFn(heartbeatTicker.Wake)
	heartbeatMethods.SetWakeFn(heartbeatTicker.Wake)
	heartbeatMethods.SetAgentStore(pgStores.Agents)
	heartbeatMethods.SetProviderStore(pgStores.Providers)
	cronHeartbeatWakeFn = func(agentID string) {
		if id, err := uuid.Parse(agentID); err == nil {
			heartbeatTicker.Wake(id)
		}
	}

	// Adaptive throttle: reduce per-session concurrency when nearing the summary threshold.
	sched.SetTokenEstimateFunc(func(sessionKey string) (int, int) {
		bctx := context.Background()
		history := pgStores.Sessions.GetHistory(bctx, sessionKey)
		lastPT, lastMC := pgStores.Sessions.GetLastPromptTokens(bctx, sessionKey)
		tokens := agent.EstimateTokensWithCalibration(history, lastPT, lastMC)
		cw := pgStores.Sessions.GetContextWindow(bctx, sessionKey)
		if cw <= 0 {
			cw = config.DefaultContextWindow
		}
		return tokens, cw
	})

	return heartbeatTicker
}
