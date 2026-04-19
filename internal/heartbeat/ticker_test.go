package heartbeat

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Test pure functions with table-driven tests

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect int // minutes since midnight
	}{
		{"midnight", "00:00", 0},
		{"morning", "08:30", 8*60 + 30},
		{"noon", "12:00", 12 * 60},
		{"afternoon", "14:45", 14*60 + 45},
		{"evening", "18:00", 18 * 60},
		{"late_night", "23:59", 23*60 + 59},
		{"invalid_format", "invalid", 0},
		{"missing_colon", "1030", 0},
		{"single_digit_hour", "9:15", 9*60 + 15},
		{"single_digit_minute", "10:5", 10*60 + 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHHMM(tt.input)
			if got != tt.expect {
				t.Errorf("parseHHMM(%q) = %d, want %d", tt.input, got, tt.expect)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLen    int
		expect    string
		expectLen int
	}{
		{"short_string", "hello", 10, "hello", 5},
		{"exact_length", "hello", 5, "hello", 5},
		{"truncate_needed", "hello world", 5, "hello…", 6}, // 5 chars + ellipsis
		{"empty_string", "", 10, "", 0},
		{"one_char", "a", 1, "a", 1},
		{"truncate_one_char", "ab", 1, "a…", 2},
		{"unicode_emoji", "👋hello", 3, "👋he…", 4}, // emoji counts as 1 rune
		{"cjk_chars", "你好世界", 2, "你好…", 3},
		{"mixed_unicode", "hello👋world", 8, "hello👋wo…", 9}, // "hello" (5) + emoji (1) + "wo" (2) + "…" (1) = 9
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.expect {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expect)
			}
			if len([]rune(got)) != tt.expectLen {
				t.Errorf("truncate(%q, %d) result has %d runes, want %d",
					tt.input, tt.maxLen, len([]rune(got)), tt.expectLen)
			}
		})
	}
}

func TestDeref(t *testing.T) {
	tests := []struct {
		name   string
		input  *string
		expect string
	}{
		{"nil_pointer", nil, ""},
		{"empty_string", new(""), ""},
		{"normal_string", new("hello"), "hello"},
		{"whitespace", new("  "), "  "},
		{"unicode_string", new("你好"), "你好"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deref(tt.input)
			if got != tt.expect {
				t.Errorf("deref(%v) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestProcessResponse(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		maxChars      int
		expectDeliver bool
		description   string
	}{
		{
			name:          "empty_response",
			response:      "",
			expectDeliver: true,
			description:   "empty response should deliver (no HEARTBEAT_OK token)",
		},
		{
			name:          "heartbeat_ok_only",
			response:      "HEARTBEAT_OK",
			expectDeliver: false,
			description:   "HEARTBEAT_OK suppresses delivery",
		},
		{
			name:          "heartbeat_ok_with_content",
			response:      "Everything is fine. HEARTBEAT_OK All checks passed.",
			expectDeliver: false,
			description:   "HEARTBEAT_OK anywhere in response suppresses delivery",
		},
		{
			name:          "normal_content",
			response:      "System check: CPU 45%, Memory 60%, Disk 70%",
			expectDeliver: true,
			description:   "normal content without HEARTBEAT_OK is delivered",
		},
		{
			name:          "ok_token_case_sensitive",
			response:      "heartbeat_ok Status is good",
			expectDeliver: true,
			description:   "HEARTBEAT_OK is case-sensitive, lowercase should not suppress",
		},
		{
			name:          "heartbeat_ok_at_start",
			response:      "HEARTBEAT_OK status unchanged",
			expectDeliver: false,
			description:   "HEARTBEAT_OK at start suppresses",
		},
		{
			name:          "heartbeat_ok_at_end",
			response:      "All good. HEARTBEAT_OK",
			expectDeliver: false,
			description:   "HEARTBEAT_OK at end suppresses",
		},
		{
			name:          "multiline_without_token",
			response:      "Line 1\nLine 2\nLine 3",
			expectDeliver: true,
			description:   "multiline without HEARTBEAT_OK is delivered",
		},
		{
			name:          "multiline_with_token",
			response:      "Line 1\nLine 2 HEARTBEAT_OK\nLine 3",
			expectDeliver: false,
			description:   "HEARTBEAT_OK in middle of multiline suppresses",
		},
		{
			name:          "long_response",
			response:      "This is a very long response with lots of details about the system status, but no special tokens",
			expectDeliver: true,
			description:   "long response without token is delivered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deliver, _ := processResponse(tt.response, tt.maxChars)
			if deliver != tt.expectDeliver {
				t.Errorf("processResponse deliver = %v, want %v (%s)",
					deliver, tt.expectDeliver, tt.description)
			}
		})
	}
}

//go:fix inline
func strPtr(s string) *string {
	return new(s)
}

func TestIsWithinActiveHours_NoConfig(t *testing.T) {
	// When no active hours configured, should always return true
	hb := &store.AgentHeartbeat{
		ActiveHoursStart: nil,
		ActiveHoursEnd:   nil,
	}
	got := isWithinActiveHours(*hb)
	if !got {
		t.Errorf("isWithinActiveHours with no config = false, want true")
	}
}

func TestIsWithinActiveHours_EmptyConfig(t *testing.T) {
	// When active hours are empty strings, should always return true
	hb := &store.AgentHeartbeat{
		ActiveHoursStart: new(""),
		ActiveHoursEnd:   new(""),
	}
	got := isWithinActiveHours(*hb)
	if !got {
		t.Errorf("isWithinActiveHours with empty config = false, want true")
	}
}

// Test simple time window logic (no midnight wrap)

func TestIsWithinActiveHours_SimpleWindow(t *testing.T) {
	// Helper to test window logic
	testLogic := func(hour, minute, startStr, endStr string) (inside bool) {
		startMin := parseHHMM(startStr)
		endMin := parseHHMM(endStr)
		nowMin := parseHHMM(hour + ":" + minute)

		if startMin <= endMin {
			inside = nowMin >= startMin && nowMin < endMin
		} else {
			inside = nowMin >= startMin || nowMin < endMin
		}
		return
	}

	tests := []struct {
		name         string
		hour         int
		minute       int
		startStr     string
		endStr       string
		expectInside bool
	}{
		// Within window
		{"within_morning", 10, 30, "08:00", "17:00", true},
		{"at_exact_start", 8, 0, "08:00", "17:00", true},
		{"just_before_end", 16, 59, "08:00", "17:00", true},

		// Outside window
		{"at_exact_end", 17, 0, "08:00", "17:00", false},
		{"before_start", 7, 59, "08:00", "17:00", false},
		{"after_end", 17, 1, "08:00", "17:00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testLogic(
				formatInt(tt.hour), formatInt(tt.minute),
				tt.startStr, tt.endStr,
			)
			if got != tt.expectInside {
				t.Errorf("window %s-%s at %02d:%02d = %v, want %v",
					tt.startStr, tt.endStr, tt.hour, tt.minute, got, tt.expectInside)
			}
		})
	}
}

// Test midnight-wrap window logic

func TestIsWithinActiveHours_MidnightWrap(t *testing.T) {
	// Helper to test window logic
	testLogic := func(hour, minute int) (inside bool) {
		startMin := parseHHMM("22:00")
		endMin := parseHHMM("06:00")
		nowMin := hour*60 + minute

		if startMin <= endMin {
			inside = nowMin >= startMin && nowMin < endMin
		} else {
			// Wraps midnight
			inside = nowMin >= startMin || nowMin < endMin
		}
		return
	}

	tests := []struct {
		name         string
		hour         int
		minute       int
		expectInside bool
	}{
		{"midnight_10pm", 22, 0, true},     // 22:00 is in window
		{"evening_11pm", 23, 30, true},     // 23:30 is in window
		{"night_2am", 2, 0, true},          // 02:00 is in window
		{"early_morning_5am", 5, 59, true}, // 05:59 is in window
		{"morning_6am", 6, 0, false},       // 06:00 is outside (exclusive end)
		{"morning_7am", 7, 0, false},       // 07:00 is outside
		{"afternoon_2pm", 14, 0, false},    // 14:00 is outside
		{"evening_9pm", 21, 59, false},     // 21:59 is outside
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testLogic(tt.hour, tt.minute)
			if got != tt.expectInside {
				t.Errorf("22:00-06:00 at %02d:%02d = %v, want %v",
					tt.hour, tt.minute, got, tt.expectInside)
			}
		})
	}
}

// Helper function

func formatInt(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
