package personal

import "testing"

func TestZaloConfigFromInstanceMapsV2Fields(t *testing.T) {
	persist := false
	ic := zaloInstanceConfig{
		DMPolicy:          "allowlist",
		GroupPolicy:       "allowlist",
		HistoryLimit:      25,
		MediaMaxMB:        64,
		SendRatePerMinute: 10,
		SendDelayMS:       250,
		DailySendCap:      100,
		RichText:          true,
		QuoteReply:        true,
		MentionsEnabled:   true,
		ReactionWakeup:    true,
		PersistCookies:    &persist,
	}

	cfg := zaloConfigFromInstance(ic)

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.DMPolicy != ic.DMPolicy || cfg.GroupPolicy != ic.GroupPolicy {
		t.Fatalf("policies = %q/%q, want %q/%q", cfg.DMPolicy, cfg.GroupPolicy, ic.DMPolicy, ic.GroupPolicy)
	}
	if cfg.HistoryLimit != 25 {
		t.Fatalf("HistoryLimit = %d, want 25", cfg.HistoryLimit)
	}
	if cfg.MediaMaxMB != 64 || cfg.SendRatePerMinute != 10 || cfg.SendDelayMS != 250 || cfg.DailySendCap != 100 {
		t.Fatalf("numeric v2 fields not mapped: %+v", cfg)
	}
	if !cfg.RichText || !cfg.QuoteReply || !cfg.MentionsEnabled || !cfg.ReactionWakeup {
		t.Fatalf("bool v2 fields not mapped: %+v", cfg)
	}
	if cfg.EffectivePersistCookies() {
		t.Fatal("PersistCookies explicit false not mapped")
	}
}
