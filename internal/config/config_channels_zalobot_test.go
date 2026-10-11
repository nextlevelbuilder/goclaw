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
func TestZaloBotConfig_ChatBehavior_StringBooleans(t *testing.T) {
	t.Parallel()

	// Test string booleans and "inherit" which Web UI can send
	raw := `{
		"enabled": true,
		"token": "test-bot-token-123",
		"chat_behavior": {
			"enabled": "true",
			"intermediate_replies": {
				"enabled": "false",
				"mode": "inherit"
			},
			"quick_ack": {
				"enabled": "true",
				"mode": "sidecar_generated"
			},
			"final_split": {
				"enabled": "off"
			}
		}
	}`

	var cfg config.ZaloBotConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("failed to unmarshal ZaloBotConfig with string booleans: %v", err)
	}

	if cfg.ChatBehavior == nil {
		t.Fatal("expected ChatBehavior to be non-nil")
	}
	if cfg.ChatBehavior.Enabled == nil || !*cfg.ChatBehavior.Enabled {
		t.Errorf("expected ChatBehavior.Enabled=true, got %v", cfg.ChatBehavior.Enabled)
	}
	if cfg.ChatBehavior.IntermediateReplies == nil {
		t.Fatal("expected IntermediateReplies to be non-nil")
	}
	if cfg.ChatBehavior.IntermediateReplies.Enabled == nil || *cfg.ChatBehavior.IntermediateReplies.Enabled {
		t.Errorf("expected IntermediateReplies.Enabled=false, got %v", cfg.ChatBehavior.IntermediateReplies.Enabled)
	}
	if cfg.ChatBehavior.IntermediateReplies.Mode != nil {
		t.Errorf("expected IntermediateReplies.Mode=nil for 'inherit', got %v", *cfg.ChatBehavior.IntermediateReplies.Mode)
	}
	if cfg.ChatBehavior.QuickAck == nil {
		t.Fatal("expected QuickAck to be non-nil")
	}
	if cfg.ChatBehavior.QuickAck.Enabled == nil || !*cfg.ChatBehavior.QuickAck.Enabled {
		t.Errorf("expected QuickAck.Enabled=true, got %v", cfg.ChatBehavior.QuickAck.Enabled)
	}
	if cfg.ChatBehavior.QuickAck.Mode == nil || *cfg.ChatBehavior.QuickAck.Mode != "sidecar_generated" {
		t.Errorf("expected QuickAck.Mode='sidecar_generated', got %v", cfg.ChatBehavior.QuickAck.Mode)
	}
	if cfg.ChatBehavior.FinalSplit == nil {
		t.Fatal("expected FinalSplit to be non-nil")
	}
	if cfg.ChatBehavior.FinalSplit.Enabled == nil || *cfg.ChatBehavior.FinalSplit.Enabled {
		t.Errorf("expected FinalSplit.Enabled=false for 'off', got %v", cfg.ChatBehavior.FinalSplit.Enabled)
	}
}

func TestZaloBotConfig_ChatBehavior_Inherit(t *testing.T) {
	t.Parallel()

	raw := `{
		"enabled": true,
		"token": "test-bot-token-123",
		"chat_behavior": {
			"enabled": "inherit"
		}
	}`

	var cfg config.ZaloBotConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if cfg.ChatBehavior == nil {
		t.Fatal("expected ChatBehavior non-nil")
	}
	if cfg.ChatBehavior.Enabled != nil {
		t.Errorf("expected Enabled=nil for 'inherit', got %v", *cfg.ChatBehavior.Enabled)
	}
}
