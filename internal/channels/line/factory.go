package line

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// lineCreds maps the credentials JSON from the channel_instances table.
type lineCreds struct {
	ChannelAccessToken string `json:"channel_access_token"`
	ChannelSecret      string `json:"channel_secret"`
}

// lineInstanceConfig maps the non-secret config JSONB from the channel_instances table.
type lineInstanceConfig struct {
	AllowFrom   []string `json:"allow_from,omitempty"`
	DMPolicy    string   `json:"dm_policy,omitempty"`
	GroupPolicy string   `json:"group_policy,omitempty"`
}

// Factory creates a LINE channel from DB instance data.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c lineCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode line credentials: %w", err)
		}
	}
	if c.ChannelAccessToken == "" || c.ChannelSecret == "" {
		return nil, fmt.Errorf("line channel_access_token and channel_secret are required")
	}

	var ic lineInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode line config: %w", err)
		}
	}

	lineCfg := config.LineConfig{
		Enabled:            true,
		ChannelAccessToken: c.ChannelAccessToken,
		ChannelSecret:      c.ChannelSecret,
		AllowFrom:          ic.AllowFrom,
		DMPolicy:           ic.DMPolicy,
		GroupPolicy:        ic.GroupPolicy,
	}

	ch, err := New(lineCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}

// FactoryWithPendingStore returns a ChannelFactory with persistent history support.
func FactoryWithPendingStore(_ store.PendingMessageStore) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return Factory(name, creds, cfg, msgBus, pairingSvc)
	}
}
