package whatsapp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// whatsappInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type whatsappInstanceConfig struct {
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	RequireMention *bool    `json:"require_mention,omitempty"`
	HistoryLimit   int      `json:"history_limit,omitempty"`
	AllowFrom      []string `json:"allow_from,omitempty"`
	BlockReply     *bool    `json:"block_reply,omitempty"`
}

// FactoryWithDB returns a ChannelFactory with DB access for whatsmeow auth state.
// dialect must be "pgx" (PostgreSQL) or "sqlite3" (SQLite/desktop).
func FactoryWithDB(db *sql.DB, pendingStore store.PendingMessageStore, dialect string) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

		var ic whatsappInstanceConfig
		if len(cfg) > 0 {
			if err := json.Unmarshal(cfg, &ic); err != nil {
				return nil, fmt.Errorf("decode whatsapp config: %w", err)
			}
		}

		// Detect old bridge_url config and give clear migration error.
		if len(cfg) > 0 {
			var legacy struct{ BridgeURL string `json:"bridge_url"` }
			if json.Unmarshal(cfg, &legacy) == nil && legacy.BridgeURL != "" {
				return nil, fmt.Errorf("whatsapp: bridge_url is no longer supported — " +
					"WhatsApp now runs natively via whatsmeow. Remove bridge_url from config")
			}
		}
		if len(creds) > 0 {
			var legacy struct{ BridgeURL string `json:"bridge_url"` }
			if json.Unmarshal(creds, &legacy) == nil && legacy.BridgeURL != "" {
				return nil, fmt.Errorf("whatsapp: bridge_url is no longer supported — " +
					"WhatsApp now runs natively via whatsmeow. Remove bridge_url from credentials")
			}
		}

		waCfg := config.WhatsAppConfig{
			Enabled:        true,
			AllowFrom:      ic.AllowFrom,
			DMPolicy:       ic.DMPolicy,
			GroupPolicy:    ic.GroupPolicy,
			RequireMention: ic.RequireMention,
			HistoryLimit:   ic.HistoryLimit,
			BlockReply:     ic.BlockReply,
		}

		// Parse per-group overrides from config JSONB.
		if len(cfg) > 0 {
			var wrapper struct {
				Groups map[string]*config.WhatsAppGroupConfig `json:"groups"`
			}
			if json.Unmarshal(cfg, &wrapper) == nil {
				waCfg.Groups = wrapper.Groups
			}
		}

		if len(waCfg.Groups) > 0 {
			slog.Info("whatsapp group overrides loaded", "name", name, "count", len(waCfg.Groups))
			for jid, gc := range waCfg.Groups {
				slog.Info("whatsapp group override", "jid", jid, "agent_id", gc.AgentID, "enabled", gc.Enabled)
			}
		}

		// DB instances default to "pairing" for groups (secure by default).
		if waCfg.GroupPolicy == "" {
			waCfg.GroupPolicy = "pairing"
		}

		ch, err := New(waCfg, msgBus, pairingSvc, db, pendingStore, dialect)
		if err != nil {
			return nil, err
		}
		ch.SetName(name)
		return ch, nil
	}
}
