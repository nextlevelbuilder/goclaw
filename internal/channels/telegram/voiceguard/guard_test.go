package voiceguard

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SanitizeReply — passthrough cases
// ---------------------------------------------------------------------------

func TestSanitize_PassThrough_WrongAgent(t *testing.T) {
	got := SanitizeReply("voice-agent", "other-agent", "telegram", "direct",
		"<media:voice>…</media:voice>", "system error occurred", "", "", nil)
	if got != "system error occurred" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestSanitize_PassThrough_EmptyVoiceAgentID(t *testing.T) {
	got := SanitizeReply("", "voice-agent", "telegram", "direct",
		"<media:voice>…</media:voice>", "exit status 1", "", "", nil)
	if got != "exit status 1" {
		t.Errorf("expected passthrough when VoiceAgentID empty, got %q", got)
	}
}

func TestSanitize_PassThrough_NonTelegram(t *testing.T) {
	got := SanitizeReply("voice-agent", "voice-agent", "discord", "direct",
		"<media:voice>…</media:voice>", "rate limit exceeded", "", "", nil)
	if got != "rate limit exceeded" {
		t.Errorf("expected passthrough for non-telegram channel, got %q", got)
	}
}

func TestSanitize_PassThrough_GroupChat(t *testing.T) {
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "group",
		"<media:voice>…</media:voice>", "system error occurred", "", "", nil)
	if got != "system error occurred" {
		t.Errorf("expected passthrough for group chat, got %q", got)
	}
}

func TestSanitize_PassThrough_NoAudioTag(t *testing.T) {
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		"just a regular text message", "system error occurred", "", "", nil)
	if got != "system error occurred" {
		t.Errorf("expected passthrough when no audio tag, got %q", got)
	}
}

func TestSanitize_PassThrough_CleanReply(t *testing.T) {
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		"<media:voice>…</media:voice>", "Great job! Your pronunciation is improving.", "", "", nil)
	if got != "Great job! Your pronunciation is improving." {
		t.Errorf("expected clean reply passthrough, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// SanitizeReply — error detection + fallback
// ---------------------------------------------------------------------------

func TestSanitize_ErrorWithTranscript_DefaultFallback(t *testing.T) {
	inbound := `<media:voice><transcript>I usually wake up at seven</transcript></media:voice>`
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		inbound, "system error: tool execution failed", "", "", nil)

	if !strings.Contains(got, "I usually wake up at seven") {
		t.Errorf("expected transcript in fallback, got: %q", got)
	}
	if strings.Contains(got, "system error") {
		t.Errorf("technical error leaked into fallback: %q", got)
	}
}

func TestSanitize_ErrorWithTranscript_CustomFallback(t *testing.T) {
	inbound := `<media:voice><transcript>hello world</transcript></media:voice>`
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		inbound, "rate limit exceeded",
		"Transcript received: %s. Please send again!", "", nil)

	want := "Transcript received: hello world. Please send again!"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestSanitize_ErrorWithTranscript_CustomFallbackNoPlaceholder(t *testing.T) {
	customTpl := "Please resend your voice note, there was a small hiccup!"
	inbound := `<media:voice><transcript>hello world</transcript></media:voice>`
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		inbound, "system error: tool execution failed", customTpl, "", nil)

	if got != customTpl {
		t.Errorf("expected clean fallback %q, got %q", customTpl, got)
	}
	if strings.Contains(got, "%!") {
		t.Errorf("fmt.Sprintf garbage leaked into output: %q", got)
	}
}

func TestSanitize_ErrorNoTranscript_DefaultFallback(t *testing.T) {
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		"<media:voice>…</media:voice>", "exit status 1", "", "", nil)

	if strings.Contains(got, "exit status") {
		t.Errorf("technical error leaked into fallback: %q", got)
	}
	if got == "" {
		t.Error("expected non-empty fallback, got empty string")
	}
}

func TestSanitize_ErrorNoTranscript_CustomFallback(t *testing.T) {
	custom := "Sorry, please resend your voice note."
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		"<media:audio>…</media:audio>", "tool error: service unavailable", "", custom, nil)

	if got != custom {
		t.Errorf("expected custom no-transcript fallback %q, got %q", custom, got)
	}
}

func TestSanitize_MediaAudioTag(t *testing.T) {
	inbound := `<media:audio><transcript>good morning</transcript></media:audio>`
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		inbound, "rate limit: too many requests", "", "", nil)

	if strings.Contains(got, "rate limit") {
		t.Errorf("technical error leaked: %q", got)
	}
	if !strings.Contains(got, "good morning") {
		t.Errorf("expected transcript in fallback, got: %q", got)
	}
}

func TestSanitize_CustomErrorMarkers(t *testing.T) {
	markers := []string{"custom failure", "oops"}
	// Default marker should NOT trigger when custom markers are set.
	got := SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		"<media:voice>…</media:voice>", "system error occurred", "", "", markers)
	if got != "system error occurred" {
		t.Errorf("expected passthrough (custom markers don't include 'system error'), got %q", got)
	}
	// Custom marker should trigger.
	got = SanitizeReply("voice-agent", "voice-agent", "telegram", "direct",
		"<media:voice>…</media:voice>", "oops something went wrong", "", "", markers)
	if got == "oops something went wrong" {
		t.Error("expected fallback for custom marker 'oops', got passthrough")
	}
}

// ---------------------------------------------------------------------------
// containsErrorLanguage
// ---------------------------------------------------------------------------

func TestContainsErrorLanguage_Positives(t *testing.T) {
	cases := []string{
		"vấn đề kỹ thuật xảy ra",
		"lỗi hệ thống",
		"vấn đề hệ thống",
		"technical issue detected",
		"system error: something broke",
		"exit status 1",
		"rate limit exceeded",
		"tool error: execution failed",
		"SYSTEM ERROR occurred", // mixed case
	}
	for _, s := range cases {
		if !containsErrorLanguage(s, nil) {
			t.Errorf("expected true for %q, got false", s)
		}
	}
}

func TestContainsErrorLanguage_Negatives(t *testing.T) {
	cases := []string{
		"",
		"Great job!",
		"Your pronunciation is improving.",
		"Please try again.",
		"I heard you say: hello world.",
	}
	for _, s := range cases {
		if containsErrorLanguage(s, nil) {
			t.Errorf("expected false for %q, got true", s)
		}
	}
}

// ---------------------------------------------------------------------------
// extractTranscript
// ---------------------------------------------------------------------------

func TestExtractTranscript_Present(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`<media:voice><transcript>hello world</transcript></media:voice>`, "hello world"},
		{`<media:audio><transcript>  spaces around  </transcript></media:audio>`, "spaces around"},
		{"<media:voice>\n<transcript>\nMulti\nline\ntranscript\n</transcript>\n</media:voice>", "Multi line transcript"},
		{"<transcript>only transcript</transcript>", "only transcript"},
	}
	for _, tc := range cases {
		got := extractTranscript(tc.input)
		if got != tc.want {
			t.Errorf("input %q: expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

func TestExtractTranscript_Absent(t *testing.T) {
	cases := []string{
		"<media:voice>…</media:voice>",
		"plain text message",
		"",
	}
	for _, s := range cases {
		got := extractTranscript(s)
		if got != "" {
			t.Errorf("expected empty transcript for %q, got %q", s, got)
		}
	}
}
