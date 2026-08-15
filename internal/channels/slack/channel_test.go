package slack

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
			expectedChunk: "line1",
			expectedRem:   "line2\nline3",
		},
		{
			name:          "over limit no newline",
			content:       "a very long string with no newlines at all",
			maxLen:        10,
			expectedChunk: "a very",
			expectedRem:   "long\nstring\nwith no\nnewlines\nat all",
		},
		{
			name:          "newline at boundary but not in second half",
			content:       "short\nvery long remaining text here",
			maxLen:        10,
			expectedChunk: "short",
			expectedRem:   "very long\nremaining\ntext here",
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
			expectedChunk: "中",
			expectedRem:   "文te\nst日\n本\n語",
		},
		{
			name:          "emoji characters",
			content:       "hello 👋 world 🌍 test",
			maxLen:        10,
			expectedChunk: "hello",
			expectedRem:   "👋\nworld\n🌍 test",
		},
		{
			name:          "newline in second half with long first half",
			content:       "aaaaaaaaaa\nbbbbbbbbbb",
			maxLen:        15,
			expectedChunk: "aaaaaaaaaa",
			expectedRem:   "bbbbbbbbbb",
		},
		{
			name:          "multiple newlines",
			content:       "line1\nline2\nline3\nline4\nline5",
			maxLen:        20,
			expectedChunk: "line1\nline2\nline3",
			expectedRem:   "line4\nline5",
		},
		{
			name:          "newline at exact boundary",
			content:       "12345\n67890",
			maxLen:        10,
			expectedChunk: "12345",
			expectedRem:   "67890",
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
			name:     "token_revoked error",
			errMsg:   "token_revoked",
			expected: true,
		},
		{
			name:     "account_inactive error",
			errMsg:   "account_inactive",
			expected: true,
		},
		{
			name:     "not_authed error",
			errMsg:   "not_authed",
			expected: true,
		},
		{
			name:     "team_not_found error",
			errMsg:   "team_not_found",
			expected: true,
		},
		{
			name:     "missing_scope error",
			errMsg:   "missing_scope",
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
			name:     "case insensitive token_revoked",
			errMsg:   "TOKEN_REVOKED",
			expected: true,
		},
		{
			name:     "error message contains non_retryable",
			errMsg:   "error: invalid_auth while connecting",
			expected: true,
		},
		{
			name:     "similar but different error",
			errMsg:   "invalid_token_auth",
			expected: false,
		},
		{
			name:     "socket error",
			errMsg:   "i/o timeout",
			expected: false,
		},
		{
			name:     "connection refused",
			errMsg:   "connection refused",
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
