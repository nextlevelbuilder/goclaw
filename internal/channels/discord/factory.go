package discord

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// discordCreds maps the credentials JSON from the channel_instances table.
type discordCreds struct {
	Token string `json:"token"`
}

// discordInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type discordInstanceConfig struct {
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	HistoryLimit   int      `json:"history_limit,omitempty"`
	BlockReply     *bool    `json:"block_reply,omitempty"`
}

// Factory creates a Discord channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c discordCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode discord credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("discord token is required")
	}

	var ic discordInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode discord config: %w", err)
		}
	}

	dcCfg := config.DiscordConfig{
		Enabled:        true,
		Token:          c.Token,
		AllowFrom:      ic.AllowFrom,
		DMPolicy:       ic.DMPolicy,
		GroupPolicy:    ic.GroupPolicy,
		RequireMention: ic.RequireMention,
		HistoryLimit:   ic.HistoryLimit,
		BlockReply:     ic.BlockReply,
	}

	// DB instances default to "pairing" for groups (secure by default).
	if dcCfg.GroupPolicy == "" {
		dcCfg.GroupPolicy = "pairing"
	}

	ch, err := New(dcCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}
