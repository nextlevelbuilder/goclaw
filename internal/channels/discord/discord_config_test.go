package discord

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestNewPreservesExplicitZeroHistoryLimit(t *testing.T) {
	ch, err := New(config.DiscordConfig{Token: "token", HistoryLimit: 0}, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if got := ch.HistoryLimit(); got != 0 {
		t.Fatalf("HistoryLimit() = %d, want 0", got)
	}
}

func TestFactoryDefaultsMissingHistoryLimitButPreservesExplicitZero(t *testing.T) {
	creds := json.RawMessage(`{"token":"token"}`)

	defaulted, err := Factory("discord-main", creds, json.RawMessage(`{}`), nil, nil)
	if err != nil {
		t.Fatalf("Factory default config returned error: %v", err)
	}
	defaultedDiscord, ok := defaulted.(*Channel)
	if !ok {
		t.Fatalf("Factory returned %T, want *discord.Channel", defaulted)
	}
	if got := defaultedDiscord.HistoryLimit(); got != channels.DefaultGroupHistoryLimit {
		t.Fatalf("default HistoryLimit() = %d, want %d", got, channels.DefaultGroupHistoryLimit)
	}

	disabled, err := Factory("discord-disabled", creds, json.RawMessage(`{"history_limit":0}`), nil, nil)
	if err != nil {
		t.Fatalf("Factory explicit zero returned error: %v", err)
	}
	disabledDiscord, ok := disabled.(*Channel)
	if !ok {
		t.Fatalf("Factory returned %T, want *discord.Channel", disabled)
	}
	if got := disabledDiscord.HistoryLimit(); got != 0 {
		t.Fatalf("explicit zero HistoryLimit() = %d, want 0", got)
	}
}
