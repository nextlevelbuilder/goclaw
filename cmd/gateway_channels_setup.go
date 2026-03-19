package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/discord"
	"github.com/nextlevelbuilder/goclaw/internal/channels/feishu"
	slackchannel "github.com/nextlevelbuilder/goclaw/internal/channels/slack"
	"github.com/nextlevelbuilder/goclaw/internal/channels/telegram"
	"github.com/nextlevelbuilder/goclaw/internal/channels/whatsapp"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo"
	zalopersonal "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/zalomethods"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// registerConfigChannels registers config-based channels as fallback when no DB instances are loaded.
func registerConfigChannels(cfg *config.Config, channelMgr *channels.Manager, msgBus *bus.MessageBus, pgStores *store.Stores, instanceLoader *channels.InstanceLoader) {
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.Token != "" && instanceLoader == nil {
		tg, err := telegram.New(cfg.Channels.Telegram, msgBus, pgStores.Pairing, nil, nil, nil, nil)
		if err != nil {
			slog.Error("failed to initialize telegram channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeTelegram, tg)
			slog.Info("telegram channel enabled (config)")
		}
	}

	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.Token != "" && instanceLoader == nil {
		dc, err := discord.New(cfg.Channels.Discord, msgBus, nil, nil)
		if err != nil {
			slog.Error("failed to initialize discord channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeDiscord, dc)
			slog.Info("discord channel enabled (config)")
		}
	}

	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.BridgeURL != "" && instanceLoader == nil {
		wa, err := whatsapp.New(cfg.Channels.WhatsApp, msgBus, nil)
		if err != nil {
			slog.Error("failed to initialize whatsapp channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeWhatsApp, wa)
			slog.Info("whatsapp channel enabled (config)")
		}
	}

	if cfg.Channels.Zalo.Enabled && cfg.Channels.Zalo.Token != "" && instanceLoader == nil {
		z, err := zalo.New(cfg.Channels.Zalo, msgBus, pgStores.Pairing)
		if err != nil {
			slog.Error("failed to initialize zalo channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeZaloOA, z)
			slog.Info("zalo channel enabled (config)")
		}
	}

	if cfg.Channels.ZaloPersonal.Enabled && instanceLoader == nil {
		zp, err := zalopersonal.New(cfg.Channels.ZaloPersonal, msgBus, pgStores.Pairing, nil)
		if err != nil {
			slog.Error("failed to initialize zca channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeZaloPersonal, zp)
			slog.Info("zca (zalo personal) channel enabled (config)")
		}
	}

	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.BotToken != "" && cfg.Channels.Slack.AppToken != "" && instanceLoader == nil {
		sl, err := slackchannel.New(cfg.Channels.Slack, msgBus, nil, nil)
		if err != nil {
			slog.Error("failed to initialize slack channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeSlack, sl)
			slog.Info("slack channel enabled (config)")
		}
	}

	if cfg.Channels.Feishu.Enabled && cfg.Channels.Feishu.AppID != "" && instanceLoader == nil {
		f, err := feishu.New(cfg.Channels.Feishu, msgBus, pgStores.Pairing, nil)
		if err != nil {
			slog.Error("failed to initialize feishu channel", "error", err)
		} else {
			channelMgr.RegisterChannel(channels.TypeFeishu, f)
			slog.Info("feishu/lark channel enabled (config)")
		}
	}
}

// wireChannelRPCMethods registers WS RPC methods for channels, instances, agent links, and teams.
func wireChannelRPCMethods(server *gateway.Server, pgStores *store.Stores, channelMgr *channels.Manager, agentRouter *agent.Router, msgBus *bus.MessageBus, dataDir string) {
	// Register channels RPC methods (after channelMgr is initialized with all channels)
	methods.NewChannelsMethods(channelMgr).Register(server.Router())

	// Register channel instances WS RPC methods
	if pgStores.ChannelInstances != nil {
		methods.NewChannelInstancesMethods(pgStores.ChannelInstances, msgBus, msgBus).Register(server.Router())
		zalomethods.NewQRMethods(pgStores.ChannelInstances, msgBus).Register(server.Router())
		zalomethods.NewContactsMethods(pgStores.ChannelInstances).Register(server.Router())
	}

	// Register agent links WS RPC methods
	if pgStores.AgentLinks != nil && pgStores.Agents != nil {
		methods.NewAgentLinksMethods(pgStores.AgentLinks, pgStores.Agents, agentRouter, msgBus, msgBus).Register(server.Router())
	}

	// Register agent teams WS RPC methods
	if pgStores.Teams != nil {
		methods.NewTeamsMethods(pgStores.Teams, pgStores.Agents, pgStores.AgentLinks, agentRouter, msgBus, msgBus, dataDir).Register(server.Router())
	}
}

// wireChannelEventSubscribers sets up event subscribers for channel instance cache invalidation,
// pairing approval/revocation, and agent cascade disable.
func wireChannelEventSubscribers(
	msgBus *bus.MessageBus,
	server *gateway.Server,
	pgStores *store.Stores,
	channelMgr *channels.Manager,
	instanceLoader *channels.InstanceLoader,
	pairingMethods *methods.PairingMethods,
	cfg *config.Config,
) {
	// Cache invalidation: reload channel instances on changes.
	if instanceLoader != nil {
		msgBus.Subscribe(bus.TopicCacheChannelInstances, func(event bus.Event) {
			if event.Name != protocol.EventCacheInvalidate {
				return
			}
			payload, ok := event.Payload.(bus.CacheInvalidatePayload)
			if !ok || payload.Kind != bus.CacheKindChannelInstances {
				return
			}
			go instanceLoader.Reload(context.Background())
		})
	}

	// Wire pairing approval notification → channel (matching TS notifyPairingApproved).
	botName := cfg.ResolveDisplayName("default")
	pairingMethods.SetOnApprove(func(ctx context.Context, channel, chatID, senderID string) {
		// Browser/internal channels use WebSocket — UI polls approval status directly.
		if channels.IsInternalChannel(channel) {
			slog.Debug("pairing approved for internal channel, skipping notification", "channel", channel)
			return
		}
		msg := fmt.Sprintf("✅ %s access approved. Send a message to start chatting.", botName)
		// Group pairings need group_id metadata so channels (e.g. Zalo) route to group API.
		if strings.HasPrefix(senderID, "group:") {
			msgBus.PublishOutbound(bus.OutboundMessage{
				Channel:  channel,
				ChatID:   chatID,
				Content:  msg,
				Metadata: map[string]string{"group_id": chatID},
			})
		} else if err := channelMgr.SendToChannel(ctx, channel, chatID, msg); err != nil {
			slog.Warn("failed to send pairing approval notification", "channel", channel, "chatID", chatID, "error", err)
		}
	})

	// Wire pairing revocation → force disconnect active WebSocket sessions.
	msgBus.Subscribe(bus.TopicPairingRevoked, func(event bus.Event) {
		if event.Name != bus.EventPairingRevoked {
			return
		}
		payload, ok := event.Payload.(bus.PairingRevokedPayload)
		if !ok {
			return
		}
		go server.DisconnectByPairing(payload.SenderID, payload.Channel)
	})

	// Cascade: when an agent becomes inactive, disable its linked channel instances.
	if pgStores.ChannelInstances != nil {
		ciStore := pgStores.ChannelInstances
		msgBus.Subscribe(bus.TopicAgentStatusChanged, func(event bus.Event) {
			if event.Name != bus.EventAgentStatusChanged {
				return
			}
			payload, ok := event.Payload.(bus.AgentStatusChangedPayload)
			if !ok || payload.NewStatus != store.AgentStatusInactive {
				return
			}
			go func() {
				agentID, err := uuid.Parse(payload.AgentID)
				if err != nil {
					return
				}
				all, err := ciStore.ListAll(context.Background())
				if err != nil {
					slog.Warn("cascade disable: failed to list channel instances", "error", err)
					return
				}
				disabled := 0
				for _, inst := range all {
					if inst.AgentID == agentID && inst.Enabled {
						if err := ciStore.Update(context.Background(), inst.ID, map[string]any{"enabled": false}); err != nil {
							slog.Warn("cascade disable: failed to disable channel instance", "name", inst.Name, "error", err)
						} else {
							disabled++
						}
					}
				}
				if disabled > 0 {
					slog.Info("cascade disabled channel instances for inactive agent", "agent_id", payload.AgentID, "count", disabled)
					// Trigger channel reload so disabled instances are stopped.
					msgBus.Broadcast(bus.Event{
						Name:    protocol.EventCacheInvalidate,
						Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
					})
				}
			}()
		})
	}
}
