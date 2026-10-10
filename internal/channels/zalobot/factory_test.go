package zalobot

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestFactory_Success(t *testing.T) {
	t.Parallel()

	creds := json.RawMessage(`{"token": "test-bot-token"}`)
	cfg := json.RawMessage(`{
		"dm_policy": "pairing",
		"group_policy": "allowlist",
		"poll_timeout_sec": 20
	}`)

	msgBus := bus.New()
	ch, err := Factory("my-zalo-bot", creds, cfg, msgBus, nil)
	if err != nil {
		t.Fatalf("Factory returned error: %v", err)
	}

	if ch.Name() != "my-zalo-bot" {
		t.Errorf("expected channel name my-zalo-bot, got %s", ch.Name())
	}
	if ch.Type() != "zalo_bot" {
		t.Errorf("expected channel type zalo_bot, got %s", ch.Type())
	}
}

func TestFactory_MissingToken(t *testing.T) {
	t.Parallel()

	creds := json.RawMessage(`{}`)
	cfg := json.RawMessage(`{}`)

	msgBus := bus.New()
	_, err := Factory("my-zalo-bot", creds, cfg, msgBus, nil)
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
}
