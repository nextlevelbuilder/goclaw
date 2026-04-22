package audio

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// newBridgeTestCfg builds a minimal config with optional channel STT URLs.
func newBridgeTestCfg(telegramURL, feishuURL, discordURL string) *config.Config {
	return &config.Config{
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramConfig{STTProxyURL: telegramURL, STTAPIKey: "tg-key"},
			Feishu:   config.FeishuConfig{STTProxyURL: feishuURL},
			Discord:  config.DiscordConfig{STTProxyURL: discordURL},
		},
	}
}

// Case 1: tenant has explicit chain set + channel has STTProxyURL
// → channel override wins (bridge-registered provider takes precedence).
func TestBridgeLegacySTT_ChannelOverrideWinsOverTenantChain(t *testing.T) {
	// Each test gets its own manager to avoid shared bridgedChannels state.
	mgr := NewManager(ManagerConfig{})

	// Simulate tenant-level provider already registered and chain set.
	mgr.RegisterSTT(&mockSTT{
		name:   "elevenlabs",
		result: &TranscriptResult{Text: "tenant", Provider: "elevenlabs"},
	})
	mgr.SetSTTChain([]string{"elevenlabs"})

	// Bridge registers telegram channel override.
	cfg := newBridgeTestCfg("http://stt.example.com", "", "")
	BridgeLegacySTT(mgr, cfg)

	// Channel override must be registered.
	if _, ok := mgr.channelSTTOverrides["telegram"]; !ok {
		t.Fatal("expected telegram channel override to be registered")
	}

	// resolveSTTChain with telegram context returns channel override, not tenant chain.
	ctx := WithChannel(context.Background(), "telegram")
	chain := mgr.resolveSTTChain(ctx)
	if len(chain) == 0 {
		t.Fatal("expected non-empty channel override chain")
	}
	// Channel-scoped key format is "proxy:telegram".
	if chain[0] != "proxy:telegram" {
		t.Errorf("expected chain[0]='proxy:telegram', got %q", chain[0])
	}
}

// Case 2: tenant has no explicit chain + channel has STTProxyURL
// → bridge registers channel-scoped provider; no-URL channels skipped.
func TestBridgeLegacySTT_RegistersWhenNoTenantChain(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
	cfg := newBridgeTestCfg("http://stt.proxy.example.com", "http://feishu.stt.example.com", "")

	BridgeLegacySTT(mgr, cfg)

	if _, ok := mgr.channelSTTOverrides["telegram"]; !ok {
		t.Error("expected telegram channel override registered")
	}
	if _, ok := mgr.channelSTTOverrides["feishu"]; !ok {
		t.Error("expected feishu channel override registered")
	}
	// Discord URL empty → should NOT be registered.
	if _, ok := mgr.channelSTTOverrides["discord"]; ok {
		t.Error("expected discord channel override NOT registered (empty URL)")
	}
}

// Case 3: calling BridgeLegacySTT twice is idempotent — no duplicate registrations.
func TestBridgeLegacySTT_Idempotent(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
	cfg := newBridgeTestCfg("http://stt.example.com", "", "")

	BridgeLegacySTT(mgr, cfg)
	countAfterFirst := len(mgr.sttProviders)

	BridgeLegacySTT(mgr, cfg)
	countAfterSecond := len(mgr.sttProviders)

	if countAfterSecond != countAfterFirst {
		t.Errorf("idempotency violated: provider count changed from %d to %d on second call",
			countAfterFirst, countAfterSecond)
	}
}
