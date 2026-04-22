package http

import (
	"testing"
)

func TestFilterAllowedKeys_ChannelInstance_IncludesName(t *testing.T) {
	input := map[string]any{"name": "my-bot", "channel_type": "telegram", "evil_field": "hack"}
	result := filterAllowedKeys(input, channelInstanceAllowedFields)
	if result["name"] != "my-bot" {
		t.Error("expected 'name' to be retained")
	}
	if _, ok := result["evil_field"]; ok {
		t.Error("expected 'evil_field' to be stripped")
	}
	if result["channel_type"] != "telegram" {
		t.Error("expected 'channel_type' to be retained")
	}
}

func TestFilterAllowedKeys(t *testing.T) {
	allowed := map[string]bool{"name": true, "status": true}

	tests := []struct {
		name     string
		updates  map[string]any
		wantKeys []string
	}{
		{
			name:     "keeps allowed keys",
			updates:  map[string]any{"name": "foo", "status": "active"},
			wantKeys: []string{"name", "status"},
		},
		{
			name:     "filters disallowed keys",
			updates:  map[string]any{"name": "foo", "id": "inject", "owner_id": "hack"},
			wantKeys: []string{"name"},
		},
		{
			name:     "empty input returns empty",
			updates:  map[string]any{},
			wantKeys: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterAllowedKeys(tt.updates, allowed)
			if tt.wantKeys == nil {
				if len(result) != 0 {
					t.Errorf("expected empty map, got %v", result)
				}
				return
			}
			if len(result) != len(tt.wantKeys) {
				t.Errorf("expected %d keys, got %d: %v", len(tt.wantKeys), len(result), result)
			}
			for _, k := range tt.wantKeys {
				if _, ok := result[k]; !ok {
					t.Errorf("expected key %q in result", k)
				}
			}
		})
	}
}

func TestFilterAllowedKeys_AgentRestrictToWorkspaceAllowed(t *testing.T) {
	input := map[string]any{
		"restrict_to_workspace": false,
		"display_name":          "Fox Spirit",
		"evil_field":            true,
	}

	result := filterAllowedKeys(input, agentAllowedFields)

	if v, ok := result["restrict_to_workspace"]; !ok || v != false {
		t.Fatalf("expected restrict_to_workspace=false to be retained, got: %#v", result["restrict_to_workspace"])
	}
	if _, ok := result["display_name"]; !ok {
		t.Fatalf("expected display_name to be retained")
	}
	if _, ok := result["evil_field"]; ok {
		t.Fatalf("expected evil_field to be stripped")
	}
}
