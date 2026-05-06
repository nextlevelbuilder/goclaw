package max

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// instanceConfig is the non-secret config persisted as JSONB in the
// channel_instances.config column. Fields mirror Telegram/WhatsApp style.
type instanceConfig struct {
	// Mode selects between long polling (dev) and webhook (prod).
	// Values: "polling" | "webhook". Default: "polling".
	Mode string `json:"mode,omitempty"`

	// WebhookURL is the public HTTPS endpoint that Max API will POST to.
	// Required when Mode == "webhook". Must be HTTPS (even self-signed).
	WebhookURL string `json:"webhook_url,omitempty"`

	// PollingTimeout in seconds for the long-poll cycle. Range 0-90, default 30.
	PollingTimeout int `json:"polling_timeout,omitempty"`

	// DMPolicy controls handling of direct messages from unknown senders.
	// Values: "pairing" | "allowlist" | "open" | "disabled". Default: "open".
	DMPolicy string `json:"dm_policy,omitempty"`

	// GroupPolicy controls handling of group messages.
	// Values: "open" | "allowlist" | "disabled". Default: "pairing".
	GroupPolicy string `json:"group_policy,omitempty"`

	// RequireMention gates group messages — bot only responds when @mentioned.
	// Default: true for groups.
	RequireMention *bool `json:"require_mention,omitempty"`

	// AllowFrom is the list of Max user IDs allowed to message the bot
	// in DM or group context (depending on DMPolicy/GroupPolicy).
	AllowFrom []string `json:"allow_from,omitempty"`

	// HistoryLimit is the maximum number of pending group messages buffered
	// while waiting for a mention to trigger an agent run.
	HistoryLimit int `json:"history_limit,omitempty"`

	// BlockReply overrides the gateway-level block_reply setting.
	// nil = inherit gateway default. Override only when you have a reason.
	BlockReply *bool `json:"block_reply,omitempty"`

	// DMStream toggles streaming preview for direct messages.
	// Default: true (streaming ON for DMs — modern UX expectation).
	// Set to false to disable: agent loop falls back to non-streaming Send.
	DMStream *bool `json:"dm_stream,omitempty"`

	// GroupStream toggles streaming preview for group messages.
	// Default: false (Max platform doesn't yet support bots in groups; even
	// once it does, in-place editing in groups is more visually noisy).
	GroupStream *bool `json:"group_stream,omitempty"`
}

// instanceCreds is the secret credentials JSON, encrypted at rest in
// channel_instances.credentials column.
type instanceCreds struct {
	// BotToken is the Max bot access token from @MasterBot
	// (https://business.max.ru/self → Чат-боты → Интеграция).
	BotToken string `json:"bot_token"`

	// BotID is the numeric user_id of the bot, returned by GET /me.
	// Optional — fetched on first start if not present.
	BotID int64 `json:"bot_id,omitempty"`

	// Username is the bot's @username, returned by GET /me.
	// Optional — fetched on first start if not present.
	Username string `json:"username,omitempty"`
}

// Factory creates a Max channel from DB instance data with no extra stores.
// Use FactoryWithPendingStoreAndAudio for production wiring.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return buildChannel(name, creds, cfg, msgBus, pairingSvc, nil, nil)
}

// FactoryWithPendingStoreAndAudio returns a ChannelFactory with the standard
// production stores and STT support. Wired up in cmd/gateway.go.
func FactoryWithPendingStoreAndAudio(
	pendingStore store.PendingMessageStore,
	audioMgr *audio.Manager,
) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return buildChannel(name, creds, cfg, msgBus, pairingSvc, pendingStore, audioMgr)
	}
}

// buildChannel parses creds + config and returns a configured Channel.
func buildChannel(
	name string,
	credsRaw json.RawMessage,
	cfgRaw json.RawMessage,
	msgBus *bus.MessageBus,
	pairingSvc store.PairingStore,
	pendingStore store.PendingMessageStore,
	audioMgr *audio.Manager,
) (channels.Channel, error) {
	var creds instanceCreds
	if len(credsRaw) > 0 {
		if err := json.Unmarshal(credsRaw, &creds); err != nil {
			return nil, fmt.Errorf("max: decode credentials: %w", err)
		}
	}
	if creds.BotToken == "" {
		return nil, fmt.Errorf("max: bot_token is required")
	}

	var cfg instanceConfig
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("max: decode config: %w", err)
		}
	}

	// Defaults aligned with Max API and Channel best practices.
	if cfg.Mode == "" {
		cfg.Mode = "polling"
	}
	if cfg.PollingTimeout <= 0 || cfg.PollingTimeout > 90 {
		cfg.PollingTimeout = 30
	}
	if cfg.DMPolicy == "" {
		cfg.DMPolicy = "open"
	}
	if cfg.GroupPolicy == "" {
		cfg.GroupPolicy = "pairing"
	}
	if cfg.HistoryLimit <= 0 {
		cfg.HistoryLimit = 50
	}

	c, err := New(name, creds, cfg, msgBus, pairingSvc, pendingStore, audioMgr)
	if err != nil {
		return nil, fmt.Errorf("max: build channel: %w", err)
	}
	return c, nil
}
