package teams

import (
	"testing"
	"time"
)

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{409, false},
	}
	for _, tt := range tests {
		got := isRetryableStatus(tt.code)
		if got != tt.want {
			t.Errorf("isRetryableStatus(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestComputeBackoff_ExponentialGrowth(t *testing.T) {
	prev := time.Duration(0)
	for attempt := range 3 {
		delay := computeBackoff(attempt, 0)
		if attempt > 0 && delay <= prev {
			t.Errorf("attempt %d delay %v should be > previous %v", attempt, delay, prev)
		}
		prev = delay
	}
}

func TestComputeBackoff_RespectsRetryAfter(t *testing.T) {
	delay := computeBackoff(0, 10*time.Second)
	if delay != 10*time.Second {
		t.Errorf("delay = %v, want 10s (Retry-After)", delay)
	}
}

func TestComputeBackoff_CapsRetryAfter(t *testing.T) {
	delay := computeBackoff(0, 300*time.Second) // 5 min > 120s cap
	if delay != sendMaxRetryAfter {
		t.Errorf("delay = %v, want %v (capped)", delay, sendMaxRetryAfter)
	}
}

func TestComputeBackoff_CapsMaxDelay(t *testing.T) {
	delay := computeBackoff(100, 0) // very high attempt
	if delay > sendMaxDelay+sendMaxDelay/4 {
		t.Errorf("delay %v exceeds max %v + jitter", delay, sendMaxDelay)
	}
}

func TestParseRetryAfterHeader(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{"5", 5 * time.Second},
		{"120", 120 * time.Second},
		{"0", 0},
		{"-1", 0},
		{"", 0},
		{"not-a-number", 0},
		{"3.5", 0}, // non-integer
	}
	for _, tt := range tests {
		got := parseRetryAfterHeader(tt.value)
		if got != tt.want {
			t.Errorf("parseRetryAfterHeader(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestTeamsSendError_Error(t *testing.T) {
	err := &teamsSendError{statusCode: 429, body: "rate limited"}
	got := err.Error()
	want := "bot framework API 429: rate limited"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
