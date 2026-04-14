package store

import (
	"strings"
	"testing"
)

func TestValidateUserID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"empty", "", false},
		{"normal", "user@example.com", false},
		{"max_length", strings.Repeat("a", 255), false},
		{"too_long", strings.Repeat("a", 256), true},
		{"way_too_long", strings.Repeat("x", 1000), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUserID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUserID(%d chars) error = %v, wantErr %v", len(tt.id), err, tt.wantErr)
			}
		})
	}
}

func TestValidateUserID_SecurityEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		// Note: SQL injection is handled by parameterized queries, but these
		// characters are still valid input (no control chars). We don't reject
		// SQL-looking strings as they could be legitimate usernames.
		{"sql_like_but_valid", "'; DROP TABLE users;--", false},

		// Unicode edge cases - format characters should be rejected
		{"unicode_zwj", "user\u200Dname", true},   // zero-width joiner
		{"unicode_rtl", "user\u202Ename", true},   // RTL override
		{"unicode_bom", "\uFEFFuser", true},       // BOM at start
		{"unicode_zwnj", "user\u200Cname", true},  // zero-width non-joiner

		// Null bytes - always reject
		{"null_byte_middle", "user\x00name", true},
		{"null_byte_end", "username\x00", true},
		{"null_byte_start", "\x00username", true},

		// Control characters - reject all
		{"control_tab", "user\tname", true},
		{"control_newline", "user\nname", true},
		{"control_carriage", "user\rname", true},
		{"control_bell", "user\x07name", true},
		{"control_backspace", "user\x08name", true},

		// Valid unicode - should pass
		{"unicode_emoji", "user🚀name", false},
		{"unicode_cjk", "用户名", false},
		{"unicode_arabic", "مستخدم", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUserID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUserID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}
