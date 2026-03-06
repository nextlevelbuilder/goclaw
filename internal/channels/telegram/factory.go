package telegram

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// telegramCreds maps the credentials JSON from the channel_instances table.
type telegramCreds struct {
	Token string `json:"token"`
	Proxy string `json:"proxy,omitempty"`
}

// telegramInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type telegramInstanceConfig struct {
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	HistoryLimit   int      `json:"history_limit,omitempty"`
	DMStream       *bool    `json:"dm_stream,omitempty"`
	GroupStream    *bool    `json:"group_stream,omitempty"`
	ReactionLevel  string   `json:"reaction_level,omitempty"`
	MediaMaxBytes  int64    `json:"media_max_bytes,omitempty"`
	LinkPreview    *bool    `json:"link_preview,omitempty"`
	BlockReply     *bool    `json:"block_reply,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
}

// Factory creates a Telegram channel from DB instance data (no agent/team store).
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return buildChannel(name, creds, cfg, msgBus, pairingSvc, nil, nil)
}

// FactoryWithStores returns a ChannelFactory that includes agent and team stores
// for group file writer management and /tasks, /task_detail commands.
func FactoryWithStores(agentStore store.AgentStore, teamStore store.TeamStore) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return buildChannel(name, creds, cfg, msgBus, pairingSvc, agentStore, teamStore)
	}
}

func buildChannel(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore, agentStore store.AgentStore, teamStore store.TeamStore) (channels.Channel, error) {

	var c telegramCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode telegram credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("telegram token is required")
	}

	var ic telegramInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode telegram config: %w", err)
		}
	}

	tgCfg := config.TelegramConfig{
		Enabled:        true,
		Token:          c.Token,
		Proxy:          c.Proxy,
		AllowFrom:      ic.AllowFrom,
		DMPolicy:       ic.DMPolicy,
		GroupPolicy:    ic.GroupPolicy,
		RequireMention: ic.RequireMention,
		HistoryLimit:   ic.HistoryLimit,
		DMStream:       ic.DMStream,
		GroupStream:    ic.GroupStream,
		ReactionLevel:  ic.ReactionLevel,
		MediaMaxBytes:  ic.MediaMaxBytes,
		LinkPreview:    ic.LinkPreview,
		BlockReply:     ic.BlockReply,
	}

	// DB instances default to "pairing" for groups (secure by default).
	// Config-based channels keep "open" default for backward compat.
	if tgCfg.GroupPolicy == "" {
		tgCfg.GroupPolicy = "pairing"
	}

	ch, err := New(tgCfg, msgBus, pairingSvc, agentStore, teamStore)
	if err != nil {
		return nil, err
	}

	// Override the channel name from DB instance.
	ch.SetName(name)
	return ch, nil
}
