package bot

import (
	"strings"
	"testing"
)

func TestFormatAPIError_KnownCodes(t *testing.T) {
	tests := []struct {
		code     int
		descr    string
		mustHave []string // substrings the hint should contain
	}{
		{400, "Bad request", []string{"400", "Bad request", "endpoint path"}},
		{401, "Unauthorized", []string{"401", "Unauthorized", "token"}},
		{403, "Internal server error", []string{"403", "Internal server error", "retry"}},
		{404, "Not found", []string{"404", "Not found", "chat_id"}},
		{408, "Request timeout", []string{"408", "Request timeout", "backoff"}},
		{429, "Quota exceeded", []string{"429", "Quota exceeded", "rate limit"}},
	}

	for _, tt := range tests {
		got := formatAPIError(tt.code, tt.descr).Error()
		for _, want := range tt.mustHave {
			if !strings.Contains(got, want) {
				t.Errorf("formatAPIError(%d, %q) missing %q in %q", tt.code, tt.descr, want, got)
			}
		}
	}
}

func TestFormatAPIError_UnknownCodeFallsBack(t *testing.T) {
	got := formatAPIError(999, "weird").Error()
	want := "zalo API error 999: weird"
	if got != want {
		t.Errorf("formatAPIError(999, %q) = %q, want %q (no hint, legacy format)", "weird", got, want)
	}
}
