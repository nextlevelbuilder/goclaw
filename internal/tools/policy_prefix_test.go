package tools

import "testing"

func TestStripToolPrefix(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		input  string
		expect string
	}{
		// Template-based ({tool_name} placeholder)
		{"template match", "proxy_{tool_name}", "proxy_exec", "exec"},
		{"template with suffix", "{tool_name}_v2", "exec_v2", "exec"},
		{"template prefix+suffix", "pre_{tool_name}_suf", "pre_exec_suf", "exec"},
		{"template no match", "proxy_{tool_name}", "other_exec", "other_exec"},
		{"template empty result", "proxy_{tool_name}", "proxy_", "proxy_"},
		{"template exact placeholder", "{tool_name}", "exec", "exec"},

		// Literal prefix
		{"literal prefix with separator", "proxy_", "proxy_exec", "exec"},
		{"literal prefix no match", "proxy_", "other_exec", "other_exec"},
		{"literal prefix underscore join", "proxy", "proxy_exec", "exec"},
		{"literal prefix single underscore strip", "proxy", "proxy__exec", "_exec"},
		{"literal prefix empty after strip", "proxy_", "proxy_", "proxy_"},
		{"literal name shorter than prefix", "proxy_", "pro", "pro"},
		{"literal name equals prefix", "proxy", "proxy", "proxy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripToolPrefix(tt.tmpl, tt.input)
			if got != tt.expect {
				t.Errorf("StripToolPrefix(%q, %q) = %q, want %q", tt.tmpl, tt.input, got, tt.expect)
			}
		})
	}
}
