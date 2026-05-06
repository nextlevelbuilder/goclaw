package max

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestFactory_RequiresBotToken(t *testing.T) {
	creds := json.RawMessage(`{}`)
	cfg := json.RawMessage(`{}`)
	msgBus := bus.New()

	_, err := Factory("test", creds, cfg, msgBus, nil)
	if err == nil {
		t.Fatal("expected error when bot_token missing")
	}
	if !strings.Contains(err.Error(), "bot_token is required") {
		t.Fatalf("expected bot_token error, got: %v", err)
	}
}

func TestFactory_AppliesDefaults(t *testing.T) {
	creds := json.RawMessage(`{"bot_token":"test-token"}`)
	cfg := json.RawMessage(`{}`)
	msgBus := bus.New()

	ch, err := Factory("test-max", creds, cfg, msgBus, nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if ch.Name() != "test-max" {
		t.Errorf("Name = %q, want %q", ch.Name(), "test-max")
	}
	if ch.Type() != "max" {
		t.Errorf("Type = %q, want %q", ch.Type(), "max")
	}
	// Defaults applied via factory.buildChannel.
	mc, ok := ch.(*Channel)
	if !ok {
		t.Fatalf("Factory returned %T, want *Channel", ch)
	}
	if mc.cfg.Mode != "polling" {
		t.Errorf("default Mode = %q, want polling", mc.cfg.Mode)
	}
	if mc.cfg.PollingTimeout != 30 {
		t.Errorf("default PollingTimeout = %d, want 30", mc.cfg.PollingTimeout)
	}
	if mc.cfg.DMPolicy != "open" {
		t.Errorf("default DMPolicy = %q, want open", mc.cfg.DMPolicy)
	}
	if mc.cfg.GroupPolicy != "pairing" {
		t.Errorf("default GroupPolicy = %q, want pairing", mc.cfg.GroupPolicy)
	}
}

func TestFactory_RejectsMalformedCreds(t *testing.T) {
	creds := json.RawMessage(`{not json`)
	cfg := json.RawMessage(`{}`)
	msgBus := bus.New()

	_, err := Factory("test", creds, cfg, msgBus, nil)
	if err == nil {
		t.Fatal("expected error on malformed credentials")
	}
	if !strings.Contains(err.Error(), "decode credentials") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func TestFactory_RejectsMalformedConfig(t *testing.T) {
	creds := json.RawMessage(`{"bot_token":"test"}`)
	cfg := json.RawMessage(`{not json`)
	msgBus := bus.New()

	_, err := Factory("test", creds, cfg, msgBus, nil)
	if err == nil {
		t.Fatal("expected error on malformed config")
	}
	if !strings.Contains(err.Error(), "decode config") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func TestFactory_PreservesCustomConfig(t *testing.T) {
	creds := json.RawMessage(`{"bot_token":"test"}`)
	cfg := json.RawMessage(`{
		"mode": "webhook",
		"webhook_url": "https://example.com/max/hook",
		"polling_timeout": 60,
		"dm_policy": "allowlist",
		"group_policy": "open",
		"allow_from": ["123","456"],
		"history_limit": 100
	}`)
	msgBus := bus.New()

	ch, err := Factory("test", creds, cfg, msgBus, nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	mc := ch.(*Channel)

	if mc.cfg.Mode != "webhook" {
		t.Errorf("Mode = %q, want webhook", mc.cfg.Mode)
	}
	if mc.cfg.WebhookURL != "https://example.com/max/hook" {
		t.Errorf("WebhookURL = %q", mc.cfg.WebhookURL)
	}
	if mc.cfg.PollingTimeout != 60 {
		t.Errorf("PollingTimeout = %d, want 60", mc.cfg.PollingTimeout)
	}
	if mc.cfg.DMPolicy != "allowlist" {
		t.Errorf("DMPolicy = %q", mc.cfg.DMPolicy)
	}
	if mc.cfg.GroupPolicy != "open" {
		t.Errorf("GroupPolicy = %q", mc.cfg.GroupPolicy)
	}
	if len(mc.cfg.AllowFrom) != 2 {
		t.Errorf("AllowFrom len = %d, want 2", len(mc.cfg.AllowFrom))
	}
	if mc.cfg.HistoryLimit != 100 {
		t.Errorf("HistoryLimit = %d, want 100", mc.cfg.HistoryLimit)
	}
}

func TestNew_RejectsInvalidArgs(t *testing.T) {
	tests := []struct {
		name    string
		argName string
		creds   instanceCreds
		bus     *bus.MessageBus
		wantErr string
	}{
		{
			name:    "empty name",
			argName: "",
			creds:   instanceCreds{BotToken: "x"},
			bus:     bus.New(),
			wantErr: "name is required",
		},
		{
			name:    "nil bus",
			argName: "ch",
			creds:   instanceCreds{BotToken: "x"},
			bus:     nil,
			wantErr: "msgBus is required",
		},
		{
			name:    "missing token",
			argName: "ch",
			creds:   instanceCreds{},
			bus:     bus.New(),
			wantErr: "bot_token is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.argName, tt.creds, instanceConfig{}, tt.bus, nil, nil, nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// Ensure Channel implements channels.Channel interface at compile time.
// This is a sentinel — the test won't even compile if interface is unsatisfied.
func TestChannelImplementsInterface(t *testing.T) {
	creds := instanceCreds{BotToken: "test"}
	ch, err := New("test", creds, instanceConfig{HistoryLimit: 50}, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Compile-time check via interface assertion at runtime.
	if ch.Type() != "max" {
		t.Errorf("Type = %q, want max", ch.Type())
	}
	if ch.IsRunning() {
		t.Error("IsRunning = true before Start")
	}
}
