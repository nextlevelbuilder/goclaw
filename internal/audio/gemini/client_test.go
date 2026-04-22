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
