package slack

import (
	"testing"
)

func TestIsAllowedDownloadHost(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		expected bool
	}{
		{
			name:     "valid slack.com domain",
			rawURL:   "https://files.slack.com/path/to/file",
			expected: true,
		},
		{
			name:     "valid slack-edge.com domain",
			rawURL:   "https://a.b.slack-edge.com/x/y/z",
			expected: true,
		},
		{
			name:     "valid slack-files.com domain",
			rawURL:   "https://files.slack-files.com/path/to/file",
			expected: true,
		},
		{
			name:     "http instead of https",
			rawURL:   "http://files.slack.com/path",
			expected: false,
		},
		{
			name:     "non-slack domain",
			rawURL:   "https://evil.com/malicious",
			expected: false,
		},
		{
			name:     "slack in subdomain but different tld",
			rawURL:   "https://files.slack.org/path",
			expected: false,
		},
		{
			name:     "slack.com but with subdomain not in allowlist",
			rawURL:   "https://notfiles.slack.com/path",
			expected: true, // matches .slack.com suffix
		},
		{
			name:     "slack-edge.com variations",
			rawURL:   "https://cdn.slack-edge.com/file",
			expected: true,
		},
		{
			name:     "empty string",
			rawURL:   "",
			expected: false,
		},
		{
			name:     "invalid url",
			rawURL:   "not a url at all",
			expected: false,
		},
		{
			name:     "hostname with special chars",
			rawURL:   "https://a]b.slack-edge.com/x",
			expected: true, // Go's URL parser handles this, suffix matches
		},
		{
			name:     "domain with slack.com.evil.com",
			rawURL:   "https://slack.com.evil.com/path",
			expected: false,
		},
		{
			name:     "ftp scheme",
			rawURL:   "ftp://files.slack.com/path",
			expected: false,
		},
		{
			name:     "case insensitive hostname",
			rawURL:   "https://FILES.SLACK.COM/path",
			expected: true,
		},
		{
			name:     "multiple subdomains",
			rawURL:   "https://a.b.c.slack-files.com/file",
			expected: true,
		},
		{
			name:     "hostname with port",
			rawURL:   "https://files.slack.com:443/path",
			expected: true,
		},
		{
			name:     "url with query params",
			rawURL:   "https://files.slack.com/file?token=abc&download=true",
			expected: true,
		},
		{
			name:     "url with fragment",
			rawURL:   "https://files.slack.com/file#section",
			expected: true,
		},
		{
			name:     "very long subdomain chain",
			rawURL:   "https://very.long.subdomain.chain.slack.com/file",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAllowedDownloadHost(tt.rawURL)
			if got != tt.expected {
				t.Errorf("isAllowedDownloadHost(%q) = %v, want %v", tt.rawURL, got, tt.expected)
			}
		})
	}
}
