package zalobot

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type zaloBotCreds struct {
	Token string `json:"token"`
}

type zaloBotInstanceConfig struct {
	DMPolicy       string                     `json:"dm_policy,omitempty"`
	GroupPolicy    string                     `json:"group_policy,omitempty"`
	GroupAllowFrom []string                   `json:"group_allow_from,omitempty"`
	RequireMention *bool                      `json:"require_mention,omitempty"`
	PollTimeoutSec int                        `json:"poll_timeout_sec,omitempty"`
	AllowFrom      []string                   `json:"allow_from,omitempty"`
	BlockReply     *bool                      `json:"block_reply,omitempty"`
	ChatBehavior   *config.ChatBehaviorConfig `json:"chat_behavior,omitempty"`
}

// Factory creates a Zalo Bot channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c zaloBotCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode zalo_bot credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("zalo_bot token is required")
	}

	var ic zaloBotInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode zalo_bot config: %w", err)
		}
	}

	botCfg := config.ZaloBotConfig{
		Enabled:        true,
		Token:          c.Token,
		AllowFrom:      ic.AllowFrom,
		DMPolicy:       ic.DMPolicy,
		GroupPolicy:    ic.GroupPolicy,
		GroupAllowFrom: ic.GroupAllowFrom,
		RequireMention: ic.RequireMention,
		PollTimeoutSec: ic.PollTimeoutSec,
		BlockReply:     ic.BlockReply,
		ChatBehavior:   ic.ChatBehavior,
	}

	ch, err := New(botCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}
