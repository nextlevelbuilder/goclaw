package bootstrap

import (
	"testing"
)

func TestModeAllowlist(t *testing.T) {
	tests := []struct {
		mode string
		want map[string]bool
	}{
		{"full", nil},
		{"", nil},
		{"task", map[string]bool{AgentsTaskFile: true, ToolsFile: true, CapabilitiesFile: true, SoulFile: true, IdentityFile: true}},
		{"minimal", map[string]bool{AgentsCoreFile: true, CapabilitiesFile: true}},
		{"none", map[string]bool{ToolsFile: true}},
		{"unknown", nil}, // fail-open to full
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := ModeAllowlist(tt.mode)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ModeAllowlist(%q) = %v, want nil", tt.mode, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ModeAllowlist(%q) = nil, want %v", tt.mode, tt.want)
			}
			if len(got) != len(tt.want) {
				t.Errorf("ModeAllowlist(%q) len = %d, want %d", tt.mode, len(got), len(tt.want))
			}
			for k := range tt.want {
				if !got[k] {
					t.Errorf("ModeAllowlist(%q) missing %q", tt.mode, k)
				}
			}
		})
	}
}
