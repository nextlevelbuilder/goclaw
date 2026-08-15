package slack

import (
	"testing"
)

func TestExtractChannelID(t *testing.T) {
	tests := []struct {
		name     string
		localKey string
		expected string
	}{
		{
			name:     "plain channel id",
			localKey: "C123456",
			expected: "C123456",
		},
		{
			name:     "threaded message",
			localKey: "C123456:thread:1234.5678",
			expected: "C123456",
		},
		{
			name:     "threaded with different ts format",
			localKey: "C999:thread:999999.999999",
			expected: "C999",
		},
		{
			name:     "no thread marker",
			localKey: "C123456789",
			expected: "C123456789",
		},
		{
			name:     "empty string",
			localKey: "",
			expected: "",
		},
		{
			name:     "only thread marker",
			localKey: ":thread:1234.5678",
			expected: ":thread:1234.5678",
		},
		{
			name:     "colon but no thread text",
			localKey: "C123:thread:",
			expected: "C123",
		},
		{
			name:     "multiple colons",
			localKey: "C123:thread:1234:5678",
			expected: "C123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractChannelID(tt.localKey)
			if got != tt.expected {
				t.Errorf("extractChannelID(%q) = %q, want %q", tt.localKey, got, tt.expected)
			}
		})
	}
}

func TestExtractThreadTS(t *testing.T) {
	tests := []struct {
		name     string
		localKey string
		expected string
	}{
		{
			name:     "threaded message",
			localKey: "C123456:thread:1234.5678",
			expected: "1234.5678",
		},
		{
			name:     "plain channel id",
			localKey: "C123456",
			expected: "",
		},
		{
			name:     "empty string",
			localKey: "",
			expected: "",
		},
		{
			name:     "only thread marker (no channel id)",
			localKey: ":thread:1234.5678",
			expected: "", // idx must be > 0, so this returns ""
		},
		{
			name:     "thread marker but empty ts",
			localKey: "C123:thread:",
			expected: "",
		},
		{
			name:     "multiple colons after thread",
			localKey: "C123:thread:1234:5678:extra",
			expected: "1234:5678:extra",
		},
		{
			name:     "thread marker not found",
			localKey: "C123:other:1234.5678",
			expected: "",
		},
		{
			name:     "thread marker case sensitive",
			localKey: "C123:THREAD:1234.5678",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractThreadTS(tt.localKey)
			if got != tt.expected {
				t.Errorf("extractThreadTS(%q) = %q, want %q", tt.localKey, got, tt.expected)
			}
		})
	}
}
