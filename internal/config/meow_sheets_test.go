package config

import (
	"testing"
	"time"
)

func TestMeowSheetsEnabledKillSwitch(t *testing.T) {
	// env "false" must override a config-file enabled:true (real kill switch).
	c := &Config{}
	c.MeowSheets.Enabled = true
	t.Setenv("GOCLAW_MEOW_SHEETS_ENABLED", "false")
	c.applyEnvOverrides()
	if c.MeowSheets.Enabled {
		t.Fatal("GOCLAW_MEOW_SHEETS_ENABLED=false must disable")
	}

	// env "on" must enable when config default is false.
	c2 := &Config{}
	t.Setenv("GOCLAW_MEOW_SHEETS_ENABLED", "on")
	c2.applyEnvOverrides()
	if !c2.MeowSheets.Enabled {
		t.Fatal("GOCLAW_MEOW_SHEETS_ENABLED=on must enable")
	}
}

func TestMeowSheetsSyncInterval(t *testing.T) {
	cases := map[string]time.Duration{
		"":     5 * time.Minute, // default
		"bad":  5 * time.Minute, // invalid → default
		"30s":  time.Minute,     // floored at 1m
		"10m":  10 * time.Minute,
		"0":    5 * time.Minute, // non-positive → default
	}
	for in, want := range cases {
		if got := (MeowSheetsConfig{Interval: in}).SyncInterval(); got != want {
			t.Errorf("SyncInterval(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMeowSheetsApprovedBy(t *testing.T) {
	if got := (MeowSheetsConfig{}).ApprovedBy(); got != "sheets-sync" {
		t.Errorf("default ApprovedBy = %q", got)
	}
	if got := (MeowSheetsConfig{ApprovedByFallback: "ops"}).ApprovedBy(); got != "ops" {
		t.Errorf("ApprovedBy = %q", got)
	}
}
