package config_test

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestZaloBotConfig_Unmarshal(t *testing.T) {
	t.Parallel()

	raw := `{
		"enabled": true,
		"token": "test-bot-token-123",
		"allow_from": ["user_1", "user_2"],
		"dm_policy": "pairing",
		"group_policy": "allowlist",
		"group_allow_from": ["group_1"],
		"require_mention": true,
		"poll_timeout_sec": 45
	}`

	var cfg config.ZaloBotConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("failed to unmarshal ZaloBotConfig: %v", err)
	}

	if !cfg.Enabled {
		t.Errorf("expected Enabled=true, got %v", cfg.Enabled)
	}
	if cfg.Token != "test-bot-token-123" {
		t.Errorf("expected Token=test-bot-token-123, got %q", cfg.Token)
	}
	if len(cfg.AllowFrom) != 2 || cfg.AllowFrom[0] != "user_1" {
		t.Errorf("unexpected AllowFrom: %v", cfg.AllowFrom)
	}
	if cfg.DMPolicy != "pairing" {
		t.Errorf("expected DMPolicy=pairing, got %q", cfg.DMPolicy)
	}
	if cfg.GroupPolicy != "allowlist" {
		t.Errorf("expected GroupPolicy=allowlist, got %q", cfg.GroupPolicy)
	}
	if len(cfg.GroupAllowFrom) != 1 || cfg.GroupAllowFrom[0] != "group_1" {
		t.Errorf("unexpected GroupAllowFrom: %v", cfg.GroupAllowFrom)
	}
	if cfg.RequireMention == nil || !*cfg.RequireMention {
		t.Errorf("expected RequireMention=true, got %v", cfg.RequireMention)
	}
	if cfg.PollTimeoutSec != 45 {
		t.Errorf("expected PollTimeoutSec=45, got %d", cfg.PollTimeoutSec)
	}
}

func TestChannelsConfig_ZaloBotField(t *testing.T) {
	t.Parallel()

	raw := `{
		"zalo_bot": {
			"enabled": true,
			"token": "bot-token-xyz"
		}
	}`

	var channelsCfg config.ChannelsConfig
	if err := json.Unmarshal([]byte(raw), &channelsCfg); err != nil {
		t.Fatalf("failed to unmarshal ChannelsConfig: %v", err)
	}

	if !channelsCfg.ZaloBot.Enabled {
		t.Errorf("expected ZaloBot.Enabled=true, got %v", channelsCfg.ZaloBot.Enabled)
	}
	if channelsCfg.ZaloBot.Token != "bot-token-xyz" {
		t.Errorf("expected ZaloBot.Token=bot-token-xyz, got %q", channelsCfg.ZaloBot.Token)
	}
}
