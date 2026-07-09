package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

const validCreds = `{"client_id":"ding_key","client_secret":"ding_secret"}`

func TestResolve_RequiresCredentials(t *testing.T) {
	tests := []struct {
		name  string
		creds string
	}{
		{"empty", ``},
		{"empty object", `{}`},
		{"missing client_secret", `{"client_id":"ding_key"}`},
		{"missing client_id", `{"client_secret":"ding_secret"}`},
		{"blank client_id", `{"client_id":"","client_secret":"s"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolve(json.RawMessage(tc.creds), nil)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), "client_id and client_secret are required") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolve_MalformedJSON(t *testing.T) {
	if _, err := resolve(json.RawMessage(`{bad`), nil); err == nil {
		t.Fatal("want error on malformed credentials")
	}
	if _, err := resolve(json.RawMessage(validCreds), json.RawMessage(`{bad`)); err == nil {
		t.Fatal("want error on malformed config")
	}
}

// DB instances must be secure by default: an unconfigured instance is reachable
// by anyone who finds the bot, so both DMs and groups start in pairing mode.
func TestConfigDefaults(t *testing.T) {
	cfg, err := resolve(json.RawMessage(validCreds), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	cfg.applyDefaults()

	if cfg.DMPolicy != "pairing" {
		t.Errorf("dm_policy = %q, want pairing", cfg.DMPolicy)
	}
	if cfg.GroupPolicy != "pairing" {
		t.Errorf("group_policy = %q, want pairing", cfg.GroupPolicy)
	}
	if cfg.TextChunkLimit != defaultTextChunkLimit {
		t.Errorf("text_chunk_limit = %d, want %d", cfg.TextChunkLimit, defaultTextChunkLimit)
	}
	if cfg.MediaMaxMB != defaultMediaMaxMB {
		t.Errorf("media_max_mb = %d, want %d", cfg.MediaMaxMB, defaultMediaMaxMB)
	}
	if cfg.GroupReplyMode != GroupReplyModeAICard {
		t.Errorf("group_reply_mode = %q, want %q", cfg.GroupReplyMode, GroupReplyModeAICard)
	}
	if cfg.GroupSessionScope != GroupSessionScopeGroup {
		t.Errorf("group_session_scope = %q, want %q", cfg.GroupSessionScope, GroupSessionScopeGroup)
	}
	if !cfg.RequireMentionOrDefault() {
		t.Error("require_mention should default to true")
	}
	if !cfg.StreamingOrDefault() {
		t.Error("streaming should default to true")
	}
	if cfg.AsyncModeOrDefault() {
		t.Error("async_mode should default to false")
	}
}

// A nil *bool means "unset" and must not collide with an explicit false.
func TestConfigExplicitFalseBeatsDefault(t *testing.T) {
	cfg, err := resolve(json.RawMessage(validCreds), json.RawMessage(
		`{"require_mention":false,"streaming":false,"async_mode":true}`))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.RequireMentionOrDefault() {
		t.Error("require_mention=false must survive the default")
	}
	if cfg.StreamingOrDefault() {
		t.Error("streaming=false must survive the default")
	}
	if !cfg.AsyncModeOrDefault() {
		t.Error("async_mode=true not honored")
	}
}

func TestConfigFullRoundTrip(t *testing.T) {
	raw := `{
		"endpoint": "https://custom.example.com",
		"allow_from": ["staff1"],
		"group_allow_from": ["cid1"],
		"dm_policy": "open",
		"group_policy": "allowlist",
		"text_chunk_limit": 1500,
		"media_max_mb": 5,
		"history_limit": 42,
		"group_reply_mode": "markdown",
		"group_session_scope": "group_sender",
		"ack_text": "收到"
	}`
	cfg, err := resolve(json.RawMessage(validCreds), json.RawMessage(raw))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if cfg.ClientID != "ding_key" || cfg.ClientSecret != "ding_secret" {
		t.Errorf("credentials not carried through: %+v", cfg)
	}
	if cfg.Endpoint != "https://custom.example.com" {
		t.Errorf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.DMPolicy != "open" || cfg.GroupPolicy != "allowlist" {
		t.Errorf("policies not carried through: %+v", cfg)
	}
	// Explicit values must not be overwritten by applyDefaults.
	if cfg.TextChunkLimit != 1500 || cfg.MediaMaxMB != 5 || cfg.HistoryLimit != 42 {
		t.Errorf("limits clobbered by defaults: %+v", cfg)
	}
	if cfg.GroupReplyMode != GroupReplyModeMarkdown {
		t.Errorf("group_reply_mode = %q", cfg.GroupReplyMode)
	}
	if cfg.GroupSessionScope != GroupSessionScopeGroupSender {
		t.Errorf("group_session_scope = %q", cfg.GroupSessionScope)
	}
	if cfg.AckText != "收到" {
		t.Errorf("ack_text = %q", cfg.AckText)
	}
}

// A typo in an enum must fail when the operator saves the instance, not hours
// later when the first group message silently takes the wrong branch.
func TestConfigValidate_RejectsBadEnums(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"bad group_reply_mode", func(c *Config) { c.GroupReplyMode = "aicards" }, "group_reply_mode"},
		{"bad group_session_scope", func(c *Config) { c.GroupSessionScope = "sender" }, "group_session_scope"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{ClientID: "a", ClientSecret: "b"}
			cfg.applyDefaults()
			tc.mutate(&cfg)
			err := cfg.validate()
			if err == nil {
				t.Fatal("want validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestFactory_BuildsChannel(t *testing.T) {
	msgBus := bus.New()
	ch, err := Factory("dingtalk-prod", json.RawMessage(validCreds), nil, msgBus, nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if got := ch.Name(); got != "dingtalk-prod" {
		t.Errorf("Name() = %q, want dingtalk-prod", got)
	}
	// Before the InstanceLoader calls SetType, Type() falls back to Name().
	// After it does, Type() must report the platform, not the instance name.
	if setter, ok := ch.(interface{ SetType(string) }); ok {
		setter.SetType(channels.TypeDingtalk)
	} else {
		t.Fatal("channel does not expose SetType; InstanceLoader wiring would silently skip it")
	}
	if got := ch.Type(); got != channels.TypeDingtalk {
		t.Errorf("Type() = %q, want %q", got, channels.TypeDingtalk)
	}
	if ch.IsRunning() {
		t.Error("channel should not be running before Start")
	}
}

func TestFactory_PropagatesConfigError(t *testing.T) {
	msgBus := bus.New()
	_, err := Factory("x", json.RawMessage(validCreds),
		json.RawMessage(`{"group_reply_mode":"nope"}`), msgBus, nil)
	if err == nil {
		t.Fatal("want error from validate(), got nil")
	}
	if !strings.Contains(err.Error(), "group_reply_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFactoryWithPendingStore_Variants(t *testing.T) {
	msgBus := bus.New()
	for name, f := range map[string]channels.ChannelFactory{
		"pending":       FactoryWithPendingStore(nil),
		"pending+audio": FactoryWithPendingStoreAndAudio(nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			ch, err := f("dt", json.RawMessage(validCreds), nil, msgBus, nil)
			if err != nil {
				t.Fatalf("factory: %v", err)
			}
			if ch.Name() != "dt" {
				t.Errorf("Name() = %q", ch.Name())
			}
		})
	}
}
