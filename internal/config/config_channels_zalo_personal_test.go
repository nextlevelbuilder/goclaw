package config

import (
	"encoding/json"
	"testing"
)

func TestZaloPersonalEffectiveDefaults(t *testing.T) {
	var cfg ZaloPersonalConfig

	if got := cfg.EffectiveMediaMaxMB(); got != 30 {
		t.Fatalf("EffectiveMediaMaxMB() = %d, want 30", got)
	}
	if got := cfg.EffectiveSendRatePerMinute(); got != 30 {
		t.Fatalf("EffectiveSendRatePerMinute() = %d, want 30", got)
	}
	if got := cfg.EffectiveSendDelayMS(); got != 1200 {
		t.Fatalf("EffectiveSendDelayMS() = %d, want 1200", got)
	}
	if got := cfg.EffectiveDailySendCap(); got != 500 {
		t.Fatalf("EffectiveDailySendCap() = %d, want 500", got)
	}
	if !cfg.EffectivePersistCookies() {
		t.Fatal("EffectivePersistCookies() = false, want true by default")
	}
}

func TestZaloPersonalEffectiveExplicitValues(t *testing.T) {
	persist := false
	cfg := ZaloPersonalConfig{
		MediaMaxMB:        64,
		SendRatePerMinute: 10,
		SendDelayMS:       250,
		DailySendCap:      100,
		PersistCookies:    &persist,
	}

	if got := cfg.EffectiveMediaMaxMB(); got != 64 {
		t.Fatalf("EffectiveMediaMaxMB() = %d, want 64", got)
	}
	if got := cfg.EffectiveSendRatePerMinute(); got != 10 {
		t.Fatalf("EffectiveSendRatePerMinute() = %d, want 10", got)
	}
	if got := cfg.EffectiveSendDelayMS(); got != 250 {
		t.Fatalf("EffectiveSendDelayMS() = %d, want 250", got)
	}
	if got := cfg.EffectiveDailySendCap(); got != 100 {
		t.Fatalf("EffectiveDailySendCap() = %d, want 100", got)
	}
	if cfg.EffectivePersistCookies() {
		t.Fatal("EffectivePersistCookies() = true, want explicit false")
	}
}

func TestZaloPersonalConfigJSONTags(t *testing.T) {
	persist := false
	cfg := ZaloPersonalConfig{
		MediaMaxMB:        30,
		SendRatePerMinute: 30,
		SendDelayMS:       1200,
		DailySendCap:      500,
		PersistCookies:    &persist,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"media_max_mb",
		"send_rate_per_minute",
		"send_delay_ms",
		"daily_send_cap",
		"persist_cookies",
	} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("marshalled config missing %q: %s", key, data)
		}
	}
	if got, ok := raw["persist_cookies"].(bool); !ok || got {
		t.Fatalf("persist_cookies = %v, want false", raw["persist_cookies"])
	}
}
