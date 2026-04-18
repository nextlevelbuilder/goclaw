package wechat

import (
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type wechatCreds struct {
	Token string `json:"token"`
}

type wechatInstanceConfig struct {
	AllowFrom  []string `json:"allow_from,omitempty"`
	DMPolicy   string   `json:"dm_policy,omitempty"`
	BaseURL    string   `json:"base_url,omitempty"`
	CDNBaseURL string   `json:"cdn_base_url,omitempty"`
}

func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

	var c wechatCreds
	if len(creds) > 0 {
		if err := json.Unmarshal(creds, &c); err != nil {
			return nil, fmt.Errorf("decode wechat credentials: %w", err)
		}
	}
	if c.Token == "" {
		return nil, fmt.Errorf("wechat token is required")
	}

	var ic wechatInstanceConfig
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &ic); err != nil {
			return nil, fmt.Errorf("decode wechat config: %w", err)
		}
	}

	wcCfg := config.WechatConfig{
		Enabled:    true,
		Token:      c.Token,
		AllowFrom:  ic.AllowFrom,
		DMPolicy:   ic.DMPolicy,
		BaseURL:    ic.BaseURL,
		CDNBaseURL: ic.CDNBaseURL,
	}

	ch, err := New(wcCfg, msgBus, pairingSvc)
	if err != nil {
		return nil, err
	}

	ch.SetName(name)
	return ch, nil
}
