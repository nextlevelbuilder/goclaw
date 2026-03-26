package mattermost

import (
	"testing"
)

func TestSplitAtLimit(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		maxLen        int
		expectedChunk string
		expectedRem   string
	}{
		{
			name:          "under limit",
			content:       "hello world",
			maxLen:        20,
			expectedChunk: "hello world",
			expectedRem:   "",
		},
		{
			name:          "exact limit",
			content:       "hello world",
			maxLen:        11,
			expectedChunk: "hello world",
			expectedRem:   "",
		},
		{
			name:          "over limit with newline at half",
			content:       "line1\nline2\nline3",
			maxLen:        10,
			expectedChunk: "line1\nline",
			expectedRem:   "2\nline3",
		},
		{
			name:          "over limit no newline",
			content:       "a very long string with no newlines at all",
			maxLen:        10,
			expectedChunk: "a very lon",
			expectedRem:   "g string with no newlines at all",
		},
		{
			name:          "empty string",
			content:       "",
			maxLen:        10,
			expectedChunk: "",
			expectedRem:   "",
		},
		{
			name:          "single character",
			content:       "a",
			maxLen:        10,
			expectedChunk: "a",
			expectedRem:   "",
		},
		{
			name:          "cjk characters",
			content:       "中文test日本語",
			maxLen:        5,
			expectedChunk: "中文tes",
			expectedRem:   "t日本語",
		},
		{
			name:          "emoji characters",
			content:       "hello 👋 world 🌍 test",
			maxLen:        10,
			expectedChunk: "hello 👋 wo",
			expectedRem:   "rld 🌍 test",
		},
		{
			name:          "newline in second half with long first half",
			content:       "aaaaaaaaaa\nbbbbbbbbbb",
			maxLen:        15,
			expectedChunk: "aaaaaaaaaa\n",
			expectedRem:   "bbbbbbbbbb",
		},
		{
			name:          "multiple newlines",
			content:       "line1\nline2\nline3\nline4\nline5",
			maxLen:        20,
			expectedChunk: "line1\nline2\nline3\n",
			expectedRem:   "line4\nline5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk, rem := splitAtLimit(tt.content, tt.maxLen)
			if chunk != tt.expectedChunk || rem != tt.expectedRem {
				t.Errorf("splitAtLimit(%q, %d) = (%q, %q), want (%q, %q)",
					tt.content, tt.maxLen, chunk, rem, tt.expectedChunk, tt.expectedRem)
			}
		})
	}
}

func TestIsNonRetryableAuthError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "invalid_auth error",
			errMsg:   "invalid_auth",
			expected: true,
		},
		{
			name:     "token revoked error",
			errMsg:   "token revoked",
			expected: true,
		},
		{
			name:     "unauthorized error",
			errMsg:   "unauthorized",
			expected: true,
		},
		{
			name:     "not_authed error",
			errMsg:   "not_authed",
			expected: true,
		},
		{
			name:     "invalid_token error",
			errMsg:   "invalid_token",
			expected: true,
		},
		{
			name:     "retryable error",
			errMsg:   "rate_limited",
			expected: false,
		},
		{
			name:     "random error",
			errMsg:   "some random error",
			expected: false,
		},
		{
			name:     "empty string",
			errMsg:   "",
			expected: false,
		},
		{
			name:     "case insensitive invalid_auth",
			errMsg:   "Invalid_Auth",
			expected: true,
		},
		{
			name:     "error message contains non_retryable",
			errMsg:   "error: unauthorized while connecting",
			expected: true,
		},
		{
			name:     "connection refused",
			errMsg:   "connection refused",
			expected: false,
		},
		{
			name:     "i/o timeout",
			errMsg:   "i/o timeout",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNonRetryableAuthError(tt.errMsg)
			if got != tt.expected {
				t.Errorf("isNonRetryableAuthError(%q) = %v, want %v", tt.errMsg, got, tt.expected)
			}
		})
	}
}

func TestFormatMention(t *testing.T) {
	tests := []struct {
		name     string
		username string
		expected string
	}{
		{"empty", "", ""},
		{"without prefix", "testuser", "@testuser"},
		{"with prefix", "@testuser", "@testuser"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMention(tt.username)
			if got != tt.expected {
				t.Errorf("formatMention(%q) = %q, want %q", tt.username, got, tt.expected)
			}
		})
	}
}

func TestFormatQuote(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{"empty", "", ""},
		{"single line", "hello world", "> hello world"},
		{"multi line", "line1\nline2", "> line1\n> line2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatQuote(tt.text)
			if got != tt.expected {
				t.Errorf("formatQuote(%q) = %q, want %q", tt.text, got, tt.expected)
			}
		})
	}
}

func TestBuildMediaTags(t *testing.T) {
	tests := []struct {
		name     string
		paths    []string
		expected string
	}{
		{"empty", nil, ""},
		{"image", []string{"/tmp/photo.jpg"}, "[Attached image: photo.jpg]"},
		{"video", []string{"/tmp/clip.mp4"}, "[Attached video: clip.mp4]"},
		{"pdf", []string{"/tmp/doc.pdf"}, "[Attached PDF: doc.pdf]"},
		{"unknown", []string{"/tmp/data.bin"}, "[Attached file: data.bin]"},
		{
			"multiple",
			[]string{"/tmp/a.png", "/tmp/b.mp3"},
			"[Attached image: a.png]\n[Attached audio: b.mp3]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMediaTags(tt.paths)
			if got != tt.expected {
				t.Errorf("buildMediaTags(%v) = %q, want %q", tt.paths, got, tt.expected)
			}
		})
	}
}
