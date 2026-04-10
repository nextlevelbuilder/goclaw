package teams

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// teamsCreds maps the encrypted credentials JSON from channel_instances table.
type teamsCreds struct {
	BotID       string `json:"bot_id"`
	BotPassword string `json:"bot_password"`
	BotType     string `json:"bot_type,omitempty"`
	TenantID    string `json:"tenant_id,omitempty"`
}

// teamsInstanceConfig maps the config JSONB from channel_instances table.
type teamsInstanceConfig struct {
	BotType     string                     `json:"bot_type,omitempty"`
	TenantID    string                     `json:"tenant_id,omitempty"`
	WebhookPath string                     `json:"webhook_path,omitempty"`
	DMPolicy    string                     `json:"dm_policy,omitempty"`
	GroupPolicy string                     `json:"group_policy,omitempty"`
	AllowFrom   config.FlexibleStringSlice `json:"allow_from,omitempty"`
	BlockReply  *bool                      `json:"block_reply,omitempty"`
}

// Factory creates a Teams channel from DB instance credentials and config.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, _ store.PairingStore) (channels.Channel, error) {

	var c teamsCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode teams credentials: %w", err)
		}
	}
	if c.BotID == "" || c.BotPassword == "" {
		return nil, fmt.Errorf("teams bot_id and bot_password are required")
	}

	var ic teamsInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode teams config: %w", err)
		}
	}

	// bot_type and tenant_id can come from either credentials or config.
	// Credentials take priority (API callers), config is where the UI stores them.
	botType := c.BotType
	if botType == "" {
		botType = ic.BotType
	}
	tenantID := c.TenantID
	if tenantID == "" {
		tenantID = ic.TenantID
	}

	teamsCfg := config.TeamsConfig{
		Enabled:     true,
		BotID:       c.BotID,
		BotPassword: c.BotPassword,
		BotType:     botType,
		TenantID:    tenantID,
		WebhookPath: ic.WebhookPath,
		DMPolicy:    ic.DMPolicy,
		GroupPolicy: ic.GroupPolicy,
		AllowFrom:   ic.AllowFrom,
		BlockReply:  ic.BlockReply,
	}

	ch, err := New(teamsCfg, msgBus)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}
