package mcp

import (
	"testing"
)

func TestMergeEnv(t *testing.T) {
	tests := []struct {
		name      string
		base      map[string]string
		overrides map[string]string
		wantLen   int
		wantKey   string
		wantVal   string
	}{
		{
			name:    "nil overrides returns base",
			base:    map[string]string{"A": "1"},
			wantLen: 1,
			wantKey: "A",
			wantVal: "1",
		},
		{
			name:      "override adds new key",
			base:      map[string]string{"A": "1"},
			overrides: map[string]string{"B": "2"},
			wantLen:   2,
			wantKey:   "B",
			wantVal:   "2",
		},
		{
			name:      "override replaces existing key",
			base:      map[string]string{"REPO": "old"},
			overrides: map[string]string{"REPO": "new"},
			wantLen:   1,
			wantKey:   "REPO",
			wantVal:   "new",
		},
		{
			name:      "base keys preserved when overriding others",
			base:      map[string]string{"TOKEN": "secret", "REPO": "old"},
			overrides: map[string]string{"REPO": "new"},
			wantLen:   2,
			wantKey:   "TOKEN",
			wantVal:   "secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeEnv(tt.base, tt.overrides)
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if got[tt.wantKey] != tt.wantVal {
				t.Errorf("%s = %q, want %q", tt.wantKey, got[tt.wantKey], tt.wantVal)
			}
		})
	}
}
