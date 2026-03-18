package http

import (
	"testing"
)

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
