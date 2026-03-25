package googlechat

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// googlechatCreds maps credentials JSON from channel_instances table.
type googlechatCreds struct {
	ServiceAccountJSON string `json:"service_account_json"`        // SA JSON content (stored encrypted in DB)
	ProjectNumber      string `json:"project_number,omitempty"`
}

// googlechatInstanceConfig maps non-secret config from channel_instances table.
type googlechatInstanceConfig struct {
	WebhookPath    string   `json:"webhook_path,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	HistoryLimit   int      `json:"history_limit,omitempty"`
	ReactionLevel  string   `json:"reaction_level,omitempty"`
	BlockReply     *bool    `json:"block_reply,omitempty"`
}

// Factory creates a Google Chat channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c googlechatCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode googlechat credentials: %w", err)
		}
	}
	if c.ServiceAccountJSON == "" {
		return nil, fmt.Errorf("googlechat service_account_json is required")
	}

	var ic googlechatInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode googlechat config: %w", err)
		}
	}

	gcCfg := config.GoogleChatConfig{
		Enabled:            true,
		ServiceAccountJSON: c.ServiceAccountJSON,
		ProjectNumber:      c.ProjectNumber,
		WebhookPath:        ic.WebhookPath,
		AllowFrom:          ic.AllowFrom,
		DMPolicy:           ic.DMPolicy,
		GroupPolicy:        ic.GroupPolicy,
		RequireMention:     ic.RequireMention,
		HistoryLimit:       ic.HistoryLimit,
		ReactionLevel:      ic.ReactionLevel,
		BlockReply:         ic.BlockReply,
	}

	// DB instances default to "pairing" for groups (secure by default).
	if gcCfg.GroupPolicy == "" {
		gcCfg.GroupPolicy = "pairing"
	}

	ch, err := New(gcCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}
