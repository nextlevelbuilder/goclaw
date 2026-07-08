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
			"exclude_history_keys": ["group-1", ""],
			"custom_prompt": "  Avoid duplicate facts across candidate items.  ",
			"group_custom_prompts": {
				"group-1": " Prefer Project Orion context. ",
				" ": "ignored",
				"group-2": "   ",
				"group-3": "Keep owner names stable."
			},
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
	if !slices.Equal(cfg.ExcludeHistoryKeys, []string{"group-1"}) {
		t.Fatalf("exclude history keys = %v", cfg.ExcludeHistoryKeys)
	}
	if cfg.CustomPrompt != "Avoid duplicate facts across candidate items." {
		t.Fatalf("custom prompt = %q", cfg.CustomPrompt)
	}
	if !mapsEqual(cfg.GroupCustomPrompts, map[string]string{
		"group-1": "Prefer Project Orion context.",
		"group-3": "Keep owner names stable.",
	}) {
		t.Fatalf("group custom prompts = %#v", cfg.GroupCustomPrompts)
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

func TestApplyConfigPatchPreservesUnspecifiedReviewMode(t *testing.T) {
	base := DefaultConfig()
	base.ReviewMode = true
	base.CustomPrompt = "Keep existing dedupe guidance."
	base.GroupCustomPrompts = map[string]string{"group-1": "Keep group prompt."}

	cfg, err := ApplyConfigPatch(base, []byte(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatal("enabled patch was not applied")
	}
	if !cfg.ReviewMode {
		t.Fatal("partial patch must preserve review_mode")
	}
	if cfg.CustomPrompt != "Keep existing dedupe guidance." {
		t.Fatalf("custom prompt = %q", cfg.CustomPrompt)
	}
	if cfg.GroupCustomPrompts["group-1"] != "Keep group prompt." {
		t.Fatalf("group custom prompt missing: %#v", cfg.GroupCustomPrompts)
	}
}

func TestApplyInstanceConfigPatchPreservesSiblingRootConfig(t *testing.T) {
	raw := json.RawMessage(`{
		"discord": {"require_mention": true},
		"passive_memory": {
			"enabled": true,
			"review_mode": true,
			"custom_prompt": "Avoid duplicates."
		}
	}`)

	updated, cfg, err := ApplyInstanceConfigPatch(raw, []byte(`{
		"custom_prompt":" Prefer one Project Orion fact. ",
		"group_custom_prompts": {"group-1": " Keep product context. "}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CustomPrompt != "Prefer one Project Orion fact." {
		t.Fatalf("custom prompt = %q", cfg.CustomPrompt)
	}
	if cfg.GroupCustomPrompts["group-1"] != "Keep product context." {
		t.Fatalf("group custom prompt = %#v", cfg.GroupCustomPrompts)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(updated, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["discord"]; !ok {
		t.Fatalf("sibling discord config missing: %s", updated)
	}
	if _, ok := root["passive_memory"]; !ok {
		t.Fatalf("passive_memory missing: %s", updated)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range b {
		if a[key] != value {
			return false
		}
	}
	return true
}
