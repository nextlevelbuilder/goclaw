package mattermost

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mattermostCreds maps the encrypted credentials JSON of a channel instance.
type mattermostCreds struct {
	BotToken string `json:"bot_token"` // bot account access token or personal access token
}

// mattermostInstanceConfig maps the non-secret config JSONB of a channel instance.
type mattermostInstanceConfig struct {
	ServerURL       string                     `json:"server_url"`
	DMPolicy        string                     `json:"dm_policy,omitempty"`
	GroupPolicy     string                     `json:"group_policy,omitempty"`
	AllowFrom       []string                   `json:"allow_from,omitempty"`
	RequireMention  *bool                      `json:"require_mention,omitempty"`
	HistoryLimit    int                        `json:"history_limit,omitempty"`
	DMStream        *bool                      `json:"dm_stream,omitempty"`
	GroupStream     *bool                      `json:"group_stream,omitempty"`
	ReactionLevel   string                     `json:"reaction_level,omitempty"`
	BlockReply      *bool                      `json:"block_reply,omitempty"`
	ChatBehavior    *config.ChatBehaviorConfig `json:"chat_behavior,omitempty"`
	ThreadTTL       *int                       `json:"thread_ttl,omitempty"`
	GroupDMAsDirect *bool                      `json:"group_dm_as_direct,omitempty"`
}

// parseInstance decodes instance credentials and config into a channel config.
func parseInstance(creds, cfg json.RawMessage) (config.MattermostConfig, error) {
	var c mattermostCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return config.MattermostConfig{}, fmt.Errorf("decode mattermost credentials: %w", err)
		}
	}
	if c.BotToken == "" {
		return config.MattermostConfig{}, fmt.Errorf("mattermost bot_token is required")
	}

	var ic mattermostInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return config.MattermostConfig{}, fmt.Errorf("decode mattermost config: %w", err)
		}
	}

	out := config.MattermostConfig{
		Enabled:         true,
		ServerURL:       ic.ServerURL,
		BotToken:        c.BotToken,
		AllowFrom:       ic.AllowFrom,
		DMPolicy:        ic.DMPolicy,
		GroupPolicy:     ic.GroupPolicy,
		RequireMention:  ic.RequireMention,
		HistoryLimit:    ic.HistoryLimit,
		DMStream:        ic.DMStream,
		GroupStream:     ic.GroupStream,
		ReactionLevel:   ic.ReactionLevel,
		BlockReply:      ic.BlockReply,
		ChatBehavior:    ic.ChatBehavior,
		ThreadTTL:       ic.ThreadTTL,
		GroupDMAsDirect: ic.GroupDMAsDirect,
	}

	// Secure default: DB instances require pairing for channels.
	if out.GroupPolicy == "" {
		out.GroupPolicy = "pairing"
	}
	return out, nil
}

// FactoryWithPendingStore returns a ChannelFactory with persistent group history.
func FactoryWithPendingStore(pendingStore store.PendingMessageStore) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

		mmCfg, err := parseInstance(creds, cfg)
		if err != nil {
			return nil, err
		}
		ch, err := New(mmCfg, msgBus, pairingSvc, pendingStore)
		if err != nil {
			return nil, err
		}
		ch.SetName(name)
		return ch, nil
	}
}
