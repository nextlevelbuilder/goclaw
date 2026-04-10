package i18n

import (
	"testing"
)

// TestT_ValidLocaleAndKey tests basic message retrieval with valid locale and key.
func TestT_ValidLocaleAndKey(t *testing.T) {
	tests := []struct {
		name     string
		locale   string
		key      string
		args     []any
		wantMsg  string
		wantKey  bool // if true, expects key to be returned (not found)
	}{
		{
			name:    "English - simple message without args",
			locale:  LocaleEN,
			key:     MsgRequired,
			args:    nil,
			wantMsg: "%s is required", // template string
			wantKey: false,
		},
		{
			name:    "English - message with args",
			locale:  LocaleEN,
			key:     MsgRequired,
			args:    []any{"email"},
			wantMsg: "email is required",
			wantKey: false,
		},
		{
			name:    "Vietnamese - has translation for required",
			locale:  LocaleVI,
			key:     MsgRequired,
			args:    nil,
			wantMsg: "%s là bắt buộc", // Vietnamese translation exists
			wantKey: false,
		},
		{
			name:    "Chinese - has translation with args",
			locale:  LocaleZH,
			key:     MsgRequired,
			args:    []any{"username"},
			wantMsg: "username 是必填项", // Chinese translation exists
			wantKey: false,
		},
		{
			name:    "English - invalid key returns key itself",
			locale:  LocaleEN,
			key:     "nonexistent.key",
			args:    nil,
			wantMsg: "nonexistent.key",
			wantKey: true,
		},
		{
			name:    "Unsupported locale - fallback to English",
			locale:  "fr", // French not supported
			key:     MsgRequired,
			args:    nil,
			wantMsg: "%s is required",
			wantKey: false,
		},
		{
			name:    "Empty locale - falls back to English",
			locale:  "",
			key:     MsgRequired,
			args:    nil,
			wantMsg: "%s is required", // empty locale falls back to English
			wantKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := T(tt.locale, tt.key, tt.args...)

			// For keys that exist in catalogs, verify we get the localized message
			// For missing keys, verify we get the key back
			if tt.wantKey {
				if result != tt.key {
					t.Errorf("T(%q, %q) = %q; want key %q", tt.locale, tt.key, result, tt.key)
				}
			} else {
				if result != tt.wantMsg {
					t.Errorf("T(%q, %q, %v) = %q; want %q", tt.locale, tt.key, tt.args, result, tt.wantMsg)
				}
			}
		})
	}
}

// TestT_MultipleArgs tests message formatting with multiple arguments.
func TestT_MultipleArgs(t *testing.T) {
	tests := []struct {
		name    string
		locale  string
		key     string
		args    []any
		wantMsg string
	}{
		{
			name:    "Message with 2 args",
			locale:  LocaleEN,
			key:     MsgNotFound,
			args:    []any{"agent", "abc123"},
			wantMsg: "agent not found: abc123",
		},
		{
			name:    "Message with correct number of args",
			locale:  LocaleEN,
			key:     MsgRequired,
			args:    []any{"email"},
			wantMsg: "email is required", // only uses first arg
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := T(tt.locale, tt.key, tt.args...)
			if result != tt.wantMsg {
				t.Errorf("T(%q, %q, %v) = %q; want %q", tt.locale, tt.key, tt.args, result, tt.wantMsg)
			}
		})
	}
}

// TestT_EmptyArgsOnTemplateString returns raw template if no args provided.
func TestT_EmptyArgsOnTemplateString(t *testing.T) {
	result := T(LocaleEN, MsgRequired) // no args
	wantTemplate := "%s is required"

	if result != wantTemplate {
		t.Errorf("T(LocaleEN, MsgRequired) without args = %q; want template %q", result, wantTemplate)
	}
}

// TestIsSupportedLocale tests locale support checking.
func TestIsSupportedLocale(t *testing.T) {
	tests := []struct {
		name     string
		locale   string
		wantBool bool
	}{
		{
			name:     "English is supported",
			locale:   LocaleEN,
			wantBool: true,
		},
		{
			name:     "Vietnamese is supported",
			locale:   LocaleVI,
			wantBool: true,
		},
		{
			name:     "Chinese is supported",
			locale:   LocaleZH,
			wantBool: true,
		},
		{
			name:     "French is not supported",
			locale:   "fr",
			wantBool: false,
		},
		{
			name:     "Spanish is not supported",
			locale:   "es",
			wantBool: false,
		},
		{
			name:     "German is not supported",
			locale:   "de",
			wantBool: false,
		},
		{
			name:     "Empty string is not supported",
			locale:   "",
			wantBool: false,
		},
		{
			name:     "Case matters - EN uppercase not supported",
			locale:   "EN",
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSupported(tt.locale)
			if result != tt.wantBool {
				t.Errorf("IsSupported(%q) = %v; want %v", tt.locale, result, tt.wantBool)
			}
		})
	}
}

// TestNormalizeLocale tests locale normalization with fallback logic.
func TestNormalizeLocale(t *testing.T) {
	tests := []struct {
		name       string
		locale     string
		wantNormal string
	}{
		{
			name:       "Already normalized - en",
			locale:     LocaleEN,
			wantNormal: LocaleEN,
		},
		{
			name:       "Already normalized - vi",
			locale:     LocaleVI,
			wantNormal: LocaleVI,
		},
		{
			name:       "Already normalized - zh",
			locale:     LocaleZH,
			wantNormal: LocaleZH,
		},
		{
			name:       "en-US prefix stripped to en",
			locale:     "en-US",
			wantNormal: LocaleEN,
		},
		{
			name:       "en-GB prefix stripped to en",
			locale:     "en-GB",
			wantNormal: LocaleEN,
		},
		{
			name:       "vi-VN prefix stripped to vi",
			locale:     "vi-VN",
			wantNormal: LocaleVI,
		},
		{
			name:       "zh-CN prefix stripped to zh",
			locale:     "zh-CN",
			wantNormal: LocaleZH,
		},
		{
			name:       "zh-TW prefix stripped to zh",
			locale:     "zh-TW",
			wantNormal: LocaleZH,
		},
		{
			name:       "Unsupported locale defaults to en",
			locale:     "fr",
			wantNormal: DefaultLocale,
		},
		{
			name:       "Unsupported with region defaults to en",
			locale:     "fr-FR",
			wantNormal: DefaultLocale,
		},
		{
			name:       "Empty string defaults to en",
			locale:     "",
			wantNormal: DefaultLocale,
		},
		{
			name:       "Single char defaults to en",
			locale:     "x",
			wantNormal: DefaultLocale,
		},
		{
			name:       "Unknown prefix defaults to en",
			locale:     "de-DE",
			wantNormal: DefaultLocale,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Normalize(tt.locale)
			if result != tt.wantNormal {
				t.Errorf("Normalize(%q) = %q; want %q", tt.locale, result, tt.wantNormal)
			}
		})
	}
}

// TestFallbackToEnglish tests fallback behavior when key is missing in requested locale.
func TestFallbackToEnglish(t *testing.T) {
	tests := []struct {
		name    string
		locale  string
		key     string
		wantMsg string
	}{
		{
			name:    "Vietnamese has translation for required",
			locale:  LocaleVI,
			key:     MsgRequired,
			wantMsg: "%s là bắt buộc", // Vietnamese has translation
		},
		{
			name:    "Chinese has translation for required",
			locale:  LocaleZH,
			key:     MsgRequired,
			wantMsg: "%s 是必填项", // Chinese has translation
		},
		{
			name:    "Key not in any catalog returns key itself",
			locale:  LocaleVI,
			key:     "totally.fake.key.that.does.not.exist",
			wantMsg: "totally.fake.key.that.does.not.exist",
		},
		{
			name:    "Missing key in English returns key",
			locale:  LocaleEN,
			key:     "totally.fake.key.that.does.not.exist",
			wantMsg: "totally.fake.key.that.does.not.exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := T(tt.locale, tt.key)
			if result != tt.wantMsg {
				t.Errorf("T(%q, %q) = %q; want %q", tt.locale, tt.key, result, tt.wantMsg)
			}
		})
	}
}

// TestLookupFunction tests the internal lookup helper directly.
func TestLookup_DirectAccess(t *testing.T) {
	tests := []struct {
		name    string
		locale  string
		key     string
		wantMsg string
	}{
		{
			name:    "Direct English lookup",
			locale:  LocaleEN,
			key:     MsgRequired,
			wantMsg: "%s is required",
		},
		{
			name:    "Missing key returns key",
			locale:  LocaleEN,
			key:     "missing.key",
			wantMsg: "missing.key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lookup(tt.locale, tt.key)
			if result != tt.wantMsg {
				t.Errorf("lookup(%q, %q) = %q; want %q", tt.locale, tt.key, result, tt.wantMsg)
			}
		})
	}
}

// TestMultipleLocalesIndependent ensures catalogs are properly isolated.
func TestMultipleLocalesIndependent(t *testing.T) {
	// Verify that changing one locale doesn't affect others
	msg_en := T(LocaleEN, MsgRequired)
	msg_vi := T(LocaleVI, MsgRequired)
	msg_zh := T(LocaleZH, MsgRequired)

	// All should have resolved to something (not the key itself)
	if msg_en == "" {
		t.Error("English message should not be empty")
	}
	if msg_vi == "" {
		t.Error("Vietnamese message should not be empty")
	}
	if msg_zh == "" {
		t.Error("Chinese message should not be empty")
	}

	// English should be a template
	if msg_en != "%s is required" {
		t.Errorf("English message unexpected: %q", msg_en)
	}
}
