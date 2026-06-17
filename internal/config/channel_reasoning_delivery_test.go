package config

import (
	"encoding/json"
	"testing"
)

func TestNormalizeChannelInstanceConfigRaw_NormalizesTelegramReasoningDelivery(t *testing.T) {
	raw := json.RawMessage(`{"reasoning_delivery":"always_bubbles","reasoning_stream":false,"dm_stream":false}`)

	got := NormalizeChannelInstanceConfigRaw("telegram", raw)

	var cfg map[string]any
	if err := json.Unmarshal(got, &cfg); err != nil {
		t.Fatalf("normalized config is invalid JSON: %v", err)
	}
	if cfg["reasoning_delivery"] != reasoningDeliveryAlwaysBubbles {
		t.Fatalf("reasoning_delivery = %v", cfg["reasoning_delivery"])
	}
	if _, ok := cfg["reasoning_stream"]; ok {
		t.Fatalf("legacy reasoning_stream survived normalization: %s", got)
	}
}

func TestNormalizeChannelInstanceConfigValue_LegacyFalseMapsToOff(t *testing.T) {
	got := NormalizeChannelInstanceConfigValue("telegram", map[string]any{
		"reasoning_stream": false,
	})

	cfg := got.(map[string]any)
	if cfg["reasoning_delivery"] != reasoningDeliveryOff {
		t.Fatalf("reasoning_delivery = %v, want off", cfg["reasoning_delivery"])
	}
	if _, ok := cfg["reasoning_stream"]; ok {
		t.Fatal("legacy reasoning_stream survived normalization")
	}
}

func TestNormalizeChannelInstanceConfigValue_IgnoresOtherChannels(t *testing.T) {
	raw := json.RawMessage(`{"reasoning_stream":false}`)
	got := NormalizeChannelInstanceConfigRaw("slack", raw)
	if string(got) != string(raw) {
		t.Fatalf("non-telegram config changed: %s", got)
	}
}
