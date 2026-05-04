package bot

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type zaloCreds struct {
	Token         string `json:"token"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

type zaloInstanceConfig struct {
	DMPolicy    string   `json:"dm_policy,omitempty"`
	Transport   string   `json:"transport,omitempty"`
	WebhookPath string   `json:"webhook_path,omitempty"`
	MediaMaxMB  int      `json:"media_max_mb,omitempty"`
	AllowFrom   []string `json:"allow_from,omitempty"`
	BlockReply  *bool    `json:"block_reply,omitempty"`
}

// Factory creates a Zalo Bot channel from channel_instances data.
// Webhook-mode channels register with common.SharedRouter() at Start().
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c zaloCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode zalo credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("zalo token is required")
	}

	var ic zaloInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode zalo config: %w", err)
		}
	}

	zCfg := config.ZaloConfig{
		Enabled:       true,
		Token:         c.Token,
		AllowFrom:     ic.AllowFrom,
		DMPolicy:      ic.DMPolicy,
		Transport:     ic.Transport,
		WebhookPath:   ic.WebhookPath,
		WebhookSecret: c.WebhookSecret,
		MediaMaxMB:    ic.MediaMaxMB,
		BlockReply:    ic.BlockReply,
	}

	ch, err := New(zCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}
	ch.SetName(name)
	return ch, nil
}
