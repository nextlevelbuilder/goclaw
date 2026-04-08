package cmd

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/heartbeat"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tasks"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// lifecycleDeps bundles the extra parameters needed by runLifecycle that are not in gatewayDeps.
type lifecycleDeps struct {
	sched             *scheduler.Scheduler
	heartbeatTicker   *heartbeat.Ticker
	quotaChecker      *channels.QuotaChecker
	webFetchTool      *tools.WebFetchTool
	ttsTool           *tools.TtsTool
	sandboxMgr        sandbox.Manager
	postTurn          tools.PostTurnProcessor
	subagentMgr       *tools.SubagentManager
	consumerTeamStore store.TeamStore
	auditCh           chan bus.AuditEventPayload
	sigCh             chan os.Signal
}

// runLifecycle wires config-reload subscribers, starts consumers, task recovery,
// the signal handler goroutine, and finally starts the gateway server.
// This is the last phase of runGateway() — called after all setup is complete.
func (d *gatewayDeps) runLifecycle(
	ctx context.Context,
	cancel context.CancelFunc,
	deps lifecycleDeps,
) {
	// Reload quota config on config changes via pub/sub.
	if deps.quotaChecker != nil {
		d.msgBus.Subscribe("quota-config-reload", func(evt bus.Event) {
			if evt.Name != bus.TopicConfigChanged {
				return
			}
			updatedCfg, ok := evt.Payload.(*config.Config)
			if !ok || updatedCfg.Gateway.Quota == nil {
				return
			}
			config.MergeChannelGroupQuotas(updatedCfg)
			deps.quotaChecker.UpdateConfig(*updatedCfg.Gateway.Quota)
			slog.Info("quota config reloaded via pub/sub")
		})
	}

	// Reload cron default timezone on config changes via pub/sub.
	d.msgBus.Subscribe("cron-config-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		d.pgStores.Cron.SetDefaultTimezone(updatedCfg.Cron.DefaultTimezone)
	})

	// Reload web_fetch domain policy on config changes via pub/sub.
	d.msgBus.Subscribe("webfetch-config-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		deps.webFetchTool.UpdatePolicy(updatedCfg.Tools.WebFetch.Policy, updatedCfg.Tools.WebFetch.AllowedDomains, updatedCfg.Tools.WebFetch.BlockedDomains)
	})

	// Reload TTS providers on config changes via pub/sub.
	d.msgBus.Subscribe("tts-config-reload", func(evt bus.Event) {
		if evt.Name != bus.TopicConfigChanged {
			return
		}
		updatedCfg, ok := evt.Payload.(*config.Config)
		if !ok {
			return
		}
		if d.pgStores.ConfigSecrets != nil {
			if secrets, err := d.pgStores.ConfigSecrets.GetAll(context.Background()); err == nil && len(secrets) > 0 {
				updatedCfg.ApplyDBSecrets(secrets)
			}
		}
		newMgr := setupTTS(updatedCfg)
		if newMgr == nil {
			return
		}
		deps.ttsTool.UpdateManager(newMgr)
		slog.Info("tts config reloaded", "provider", newMgr.PrimaryProvider(), "auto", string(newMgr.AutoMode()))
	})

	// Log orphaned providers on agent deletion. Auto-delete is unsafe because
	// providers can be referenced by heartbeats (FK), OAuth tokens, media chains.
	d.msgBus.Subscribe("agent-deleted-provider-log", func(evt bus.Event) {
		if evt.Name != bus.TopicAgentDeleted {
			return
		}
		payload, ok := evt.Payload.(bus.AgentDeletedPayload)
		if !ok || payload.Provider == "" {
			return
		}
		slog.Info("agent deleted, provider may be orphaned — verify via UI",
			"agent", payload.AgentKey, "provider", payload.Provider)
	})

	// Contact collector: auto-collect user info from channels with in-memory dedup cache.
	var contactCollector *store.ContactCollector
	if d.pgStores.Contacts != nil {
		contactCollector = store.NewContactCollector(d.pgStores.Contacts, cache.NewInMemoryCache[bool]())
		d.channelMgr.SetContactCollector(contactCollector)
	}

	go consumeInboundMessages(ctx, d.msgBus, d.agentRouter, d.cfg, deps.sched, d.channelMgr, deps.consumerTeamStore, deps.quotaChecker, d.pgStores.Sessions, d.pgStores.Agents, contactCollector, deps.postTurn, deps.subagentMgr)

	// Task recovery ticker: re-dispatches stale/pending team tasks on startup and periodically.
	var taskTicker *tasks.TaskTicker
	if d.pgStores.Teams != nil {
		taskTicker = tasks.NewTaskTicker(d.pgStores.Teams, d.pgStores.Agents, d.msgBus, d.cfg.Gateway.TaskRecoveryIntervalSec)
		taskTicker.Start()
	}

	go func() {
		sig := <-deps.sigCh
		slog.Info("graceful shutdown initiated", "signal", sig)

		// Broadcast shutdown event
		d.server.BroadcastEvent(*protocol.NewEvent(protocol.EventShutdown, nil))

		// Stop channels, cron, heartbeat, and task ticker
		d.channelMgr.StopAll(context.Background())
		d.pgStores.Cron.Stop()
		deps.heartbeatTicker.Stop()
		if taskTicker != nil {
			taskTicker.Stop()
		}

		// Drain audit log queue before closing DB
		if deps.auditCh != nil {
			close(deps.auditCh)
		}

		// Close provider resources (e.g. Claude CLI temp files)
		d.providerRegistry.Close()

		// Stop sandbox pruning + release containers
		if deps.sandboxMgr != nil {
			deps.sandboxMgr.Stop()
			slog.Info("releasing sandbox containers...")
			deps.sandboxMgr.ReleaseAll(context.Background())
		}

		if deps.sched != nil {
			slog.Info("gateway: draining active runs", "timeout", "5s")
			deps.sched.Stop() // MarkDraining + StopAll
			time.Sleep(5 * time.Second)
		}

		cancel()
	}()

	slog.Info("goclaw gateway starting",
		"version", Version,
		"protocol", protocol.ProtocolVersion,
		"agents", d.agentRouter.List(),
		"tools", d.toolsReg.Count(),
		"channels", d.channelMgr.GetEnabledChannels(),
	)

	// Tailscale listener: build the mux first, then pass it to initTailscale
	// so the same routes are served on both the main listener and Tailscale.
	// Compiled via build tags: `go build -tags tsnet` to enable.
	mux := d.server.BuildMux()

	// Mount channel webhook handlers on the main mux (e.g. Feishu /feishu/events).
	// This allows webhook-based channels to share the main server port.
	for _, route := range d.channelMgr.WebhookHandlers() {
		mux.Handle(route.Path, route.Handler)
		slog.Info("webhook route mounted on gateway", "path", route.Path)
	}

	tsCleanup := initTailscale(ctx, d.cfg, mux)
	if tsCleanup != nil {
		defer tsCleanup()
	}

	// Phase 1: suggest localhost binding when Tailscale is active
	if d.cfg.Tailscale.Hostname != "" && d.cfg.Gateway.Host == "0.0.0.0" {
		slog.Info("Tailscale enabled. Consider setting GOCLAW_HOST=127.0.0.1 for localhost-only + Tailscale access")
	}

	// Security warnings
	if strings.Contains(d.cfg.Database.PostgresDSN, ":goclaw@") {
		slog.Warn("security.default_db_password: using default Postgres password — run ./prepare-env.sh to generate a strong one")
	}
	if len(d.cfg.Gateway.AllowedOrigins) > 0 {
		slog.Info("cors: allowed_origins configured", "origins", d.cfg.Gateway.AllowedOrigins)
	} else if !edition.Current().IsLimited() {
		slog.Warn("security.cors_open: no allowed_origins configured — all WebSocket origins accepted. Set gateway.allowed_origins or GOCLAW_ALLOWED_ORIGINS for production")
	}

	if err := d.server.Start(ctx); err != nil {
		slog.Error("gateway error", "error", err)
		os.Exit(1)
	}
}
