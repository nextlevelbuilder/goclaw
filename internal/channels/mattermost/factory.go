package mattermost

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mattermostCreds maps the credentials JSON from the channel_instances table.
type mattermostCreds struct {
	ServerURL string `json:"server_url"`
	BotToken  string `json:"bot_token"`
}

// mattermostInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type mattermostInstanceConfig struct {
	TeamName       string   `json:"team_name,omitempty"`
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	HistoryLimit   int      `json:"history_limit,omitempty"`
	DMStream       *bool    `json:"dm_stream,omitempty"`
	GroupStream    *bool    `json:"group_stream,omitempty"`
	ReactionLevel  string   `json:"reaction_level,omitempty"`
	BlockReply     *bool    `json:"block_reply,omitempty"`
	DebounceDelay  int      `json:"debounce_delay,omitempty"`
	ThreadTTL      *int     `json:"thread_ttl,omitempty"`
}

// Factory creates a Mattermost channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c mattermostCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode mattermost credentials: %w", err)
		}
	}
	if c.ServerURL == "" {
		return nil, fmt.Errorf("mattermost server_url is required")
	}
	if c.BotToken == "" {
		return nil, fmt.Errorf("mattermost token is required")
	}

	var ic mattermostInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode mattermost config: %w", err)
		}
	}

	reactionLevel := ic.ReactionLevel
	if reactionLevel == "" {
		reactionLevel = "off" // Mattermost default: typing indicator replaces reactions
	}

	mmCfg := config.MattermostConfig{
		Enabled:        true,
		ServerURL:      c.ServerURL,
		BotToken:       c.BotToken,
		TeamName:       ic.TeamName,
		AllowFrom:      ic.AllowFrom,
		DMPolicy:       ic.DMPolicy,
		GroupPolicy:    ic.GroupPolicy,
		RequireMention: ic.RequireMention,
		HistoryLimit:   ic.HistoryLimit,
		DMStream:       ic.DMStream,
		GroupStream:    ic.GroupStream,
		ReactionLevel:  reactionLevel,
		BlockReply:     ic.BlockReply,
		DebounceDelay:  ic.DebounceDelay,
		ThreadTTL:      ic.ThreadTTL,
	}

	ch, err := New(mmCfg, msgBus, pairingSvc, nil)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}

// FactoryWithPendingStore returns a ChannelFactory with persistent history support.
func FactoryWithPendingStore(pendingStore store.PendingMessageStore) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

		var c mattermostCreds
		if len(creds) > 0 {
			if err := json.Unmarshal(creds, &c); err != nil {
				return nil, fmt.Errorf("decode mattermost credentials: %w", err)
			}
		}
		if c.ServerURL == "" {
			return nil, fmt.Errorf("mattermost server_url is required")
		}
		if c.BotToken == "" {
			return nil, fmt.Errorf("mattermost token is required")
		}

		var ic mattermostInstanceConfig
		if len(cfg) > 0 {
			if err := json.Unmarshal(cfg, &ic); err != nil {
				return nil, fmt.Errorf("decode mattermost config: %w", err)
			}
		}

		reactionLevel := ic.ReactionLevel
		if reactionLevel == "" {
			reactionLevel = "off"
		}

		mmCfg := config.MattermostConfig{
			Enabled:        true,
			ServerURL:      c.ServerURL,
			BotToken:       c.BotToken,
			TeamName:       ic.TeamName,
			AllowFrom:      ic.AllowFrom,
			DMPolicy:       ic.DMPolicy,
			GroupPolicy:    ic.GroupPolicy,
			RequireMention: ic.RequireMention,
			HistoryLimit:   ic.HistoryLimit,
			DMStream:       ic.DMStream,
			GroupStream:    ic.GroupStream,
			ReactionLevel:  reactionLevel,
			BlockReply:     ic.BlockReply,
			DebounceDelay:  ic.DebounceDelay,
			ThreadTTL:      ic.ThreadTTL,
		}

		ch, err := New(mmCfg, msgBus, pairingSvc, pendingStore)
		if err != nil {
			return nil, err
		}
		ch.SetName(name)
		return ch, nil
	}
}
