package channelmemory

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestParseConfigDefaultsDisabled(t *testing.T) {
	cfg := ParseConfig(nil)
	if cfg.Enabled {
		t.Fatal("default passive memory must be disabled")
	}
	if !cfg.ReviewMode {
		t.Fatal("default passive memory must require review")
	}
	if !slices.Equal(cfg.AllowedTypes, DefaultAllowedTypes) {
		t.Fatalf("allowed types = %v, want %v", cfg.AllowedTypes, DefaultAllowedTypes)
	}
}

func TestParseConfigNormalizesBoundsAndTypes(t *testing.T) {
	raw := json.RawMessage(`{
		"passive_memory": {
			"enabled": true,
			"review_mode": true,
			"interval_minutes": 2,
			"message_cap": 5000,
			"retention_hours": 0,
			"allowed_types": ["people", "people", "unknown", "todos"],
			"exclude_users": ["u1", ""],
			"exclude_patterns": ["secret", "["],
			"min_messages": 1,
			"group_only": false
		}
	}`)
	cfg := ParseConfig(raw)
	if !cfg.Enabled || !cfg.ReviewMode {
		t.Fatalf("unexpected enabled/review flags: %+v", cfg)
	}
	if cfg.IntervalMinutes != 15 {
		t.Fatalf("interval = %d, want lower bound 15", cfg.IntervalMinutes)
	}
	if cfg.MessageCap != 1000 {
		t.Fatalf("message cap = %d, want upper bound 1000", cfg.MessageCap)
	}
	if cfg.RetentionHours != 168 {
		t.Fatalf("retention = %d, want fallback 168", cfg.RetentionHours)
	}
	if !slices.Equal(cfg.AllowedTypes, []string{"people", "todos"}) {
		t.Fatalf("allowed types = %v", cfg.AllowedTypes)
	}
	if !slices.Equal(cfg.ExcludeUsers, []string{"u1"}) {
		t.Fatalf("exclude users = %v", cfg.ExcludeUsers)
	}
	if !slices.Equal(cfg.ExcludePatterns, []string{"secret"}) {
		t.Fatalf("exclude patterns = %v", cfg.ExcludePatterns)
	}
	if cfg.MinMessages != 2 {
		t.Fatalf("min messages = %d, want lower bound 2", cfg.MinMessages)
	}
	if !cfg.GroupOnly {
		t.Fatal("group_only must normalize to true")
	}
}

func TestMergeIntoInstanceConfigPreservesSiblingFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	raw := MergeIntoInstanceConfig(json.RawMessage(`{"foo":"bar"}`), cfg)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root["foo"] != "bar" {
		t.Fatalf("sibling field missing: %s", raw)
	}
	if _, ok := root["passive_memory"]; !ok {
		t.Fatalf("passive_memory missing: %s", raw)
	}
}
