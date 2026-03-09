package slack

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// slackCreds maps the credentials JSON from the channel_instances table.
type slackCreds struct {
	BotToken  string `json:"bot_token"`            // xoxb-...
	AppToken  string `json:"app_token"`            // xapp-... (Socket Mode)
	UserToken string `json:"user_token,omitempty"` // xoxp-... (optional: custom identity)
}

// slackInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type slackInstanceConfig struct {
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	HistoryLimit   int      `json:"history_limit,omitempty"`
	DMStream       *bool    `json:"dm_stream,omitempty"`
	GroupStream    *bool    `json:"group_stream,omitempty"`
	NativeStream   *bool    `json:"native_stream,omitempty"`
	ReactionLevel  string   `json:"reaction_level,omitempty"`
	BlockReply     *bool    `json:"block_reply,omitempty"`
	DebounceDelay  int      `json:"debounce_delay,omitempty"`
	ThreadTTL      *int     `json:"thread_ttl,omitempty"`
}

// Factory creates a Slack channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c slackCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode slack credentials: %w", err)
		}
	}
	if c.BotToken == "" {
		return nil, fmt.Errorf("slack bot_token is required")
	}
	if c.AppToken == "" {
		return nil, fmt.Errorf("slack app_token is required for Socket Mode")
	}

	var ic slackInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode slack config: %w", err)
		}
	}

	slackCfg := config.SlackConfig{
		Enabled:        true,
		BotToken:       c.BotToken,
		AppToken:       c.AppToken,
		UserToken:      c.UserToken,
		AllowFrom:      ic.AllowFrom,
		DMPolicy:       ic.DMPolicy,
		GroupPolicy:    ic.GroupPolicy,
		RequireMention: ic.RequireMention,
		HistoryLimit:   ic.HistoryLimit,
		DMStream:       ic.DMStream,
		GroupStream:    ic.GroupStream,
		NativeStream:   ic.NativeStream,
		ReactionLevel:  ic.ReactionLevel,
		BlockReply:     ic.BlockReply,
		DebounceDelay:  ic.DebounceDelay,
		ThreadTTL:      ic.ThreadTTL,
	}

	// Secure default: DB instances default to "pairing" for groups.
	if slackCfg.GroupPolicy == "" {
		slackCfg.GroupPolicy = "pairing"
	}

	ch, err := New(slackCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}
