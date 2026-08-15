package gemini

import "testing"

// TestBuildURL_AppendsModelAndAction verifies the URL is constructed correctly.
func TestBuildURL_AppendsModelAndAction(t *testing.T) {
	base := "https://generativelanguage.googleapis.com"
	model := "gemini-2.5-flash-preview-tts"
	got := buildURL(base, model)
	want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-preview-tts:generateContent"
	if got != want {
		t.Errorf("buildURL = %q, want %q", got, want)
	}
}

// TestBuildURL_TrimsTrailingSlash verifies trailing slash on base is stripped.
func TestBuildURL_TrimsTrailingSlash(t *testing.T) {
	got := buildURL("https://generativelanguage.googleapis.com/", "gemini-2.5-pro-preview-tts")
	want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro-preview-tts:generateContent"
	if got != want {
		t.Errorf("buildURL = %q, want %q", got, want)
	}
}

// TestProviderClient_DefaultTimeoutIs120s pins the validation-locked decision that
// the Gemini HTTP client defaults to 120000ms when timeoutMs<=0, matching the handler
// default. Without this alignment, unset tenant configs silently cap at 30s.
func TestProviderClient_DefaultTimeoutIs120s(t *testing.T) {
	c := newClient("key", "", 0)
	if c.timeoutMs != 120000 {
		t.Errorf("newClient timeoutMs=0 should default to 120000, got %d", c.timeoutMs)
	}
}

// TestProviderClient_ExplicitTimeoutIsHonored verifies that an explicit timeoutMs
// is preserved and not overwritten by the default.
func TestProviderClient_ExplicitTimeoutIsHonored(t *testing.T) {
	c := newClient("key", "", 45000)
	if c.timeoutMs != 45000 {
		t.Errorf("newClient timeoutMs=45000 should stay 45000, got %d", c.timeoutMs)
	}
}
