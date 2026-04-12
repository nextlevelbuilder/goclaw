package telegram

import (
	"testing"
)

// --- parseChatID ---

func TestParseChatID(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"12345", 12345, false},
		{"-100123456789", -100123456789, false},
		{"0", 0, false},
		{"-1", -1, false},
		{"", 0, true},
		{"abc", 0, true},
		{"12.34", 12, false}, // Sscanf stops at '.'
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseChatID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseChatID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseChatID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --- parseRawChatID ---

func TestParseRawChatID(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		// Plain chat ID
		{"-100123456", -100123456, false},
		// With :topic: suffix
		{"-100123456:topic:42", -100123456, false},
		// With :thread: suffix
		{"-100123456:thread:99", -100123456, false},
		// Positive ID with topic
		{"12345:topic:7", 12345, false},
		// Invalid base
		{"notanumber:topic:1", 0, true},
		// Empty string
		{"", 0, true},
		// Just the suffix marker (no id before it)
		{":topic:42", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseRawChatID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRawChatID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseRawChatID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --- isEnabled / effectiveMentionMode / effectiveRequireMention (resolvedTopicConfig) ---
// These are already partially covered by topic_config_test.go; add a few missing branches.

func TestIsEnabled_ExplicitTrue(t *testing.T) {
	b := true
	r := resolvedTopicConfig{enabled: &b}
	if !r.isEnabled() {
		t.Error("isEnabled() with explicit true should return true")
	}
}

func TestIsEnabled_ExplicitFalse(t *testing.T) {
	b := false
	r := resolvedTopicConfig{enabled: &b}
	if r.isEnabled() {
		t.Error("isEnabled() with explicit false should return false")
	}
}

func TestEffectiveMentionMode_Override(t *testing.T) {
	r := resolvedTopicConfig{mentionMode: "yield"}
	got := r.effectiveMentionMode("strict")
	if got != "yield" {
		t.Errorf("effectiveMentionMode = %q, want yield", got)
	}
}

func TestEffectiveMentionMode_FallbackToDefault(t *testing.T) {
	r := resolvedTopicConfig{}
	got := r.effectiveMentionMode("strict")
	if got != "strict" {
		t.Errorf("effectiveMentionMode = %q, want strict (default)", got)
	}
}

func TestEffectiveRequireMention_Override(t *testing.T) {
	b := false
	r := resolvedTopicConfig{requireMention: &b}
	if r.effectiveRequireMention(true) != false {
		t.Error("effectiveRequireMention should return override false when set")
	}
}
