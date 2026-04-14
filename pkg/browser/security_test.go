package browser

import (
	"strings"
	"testing"
)

func TestValidateBrowserTargetURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		policy  SSRFPolicy
		wantErr string
	}{
		{
			name:    "blocks localhost",
			url:     "http://localhost:8080",
			wantErr: "blocked hostname",
		},
		{
			name:    "blocks private ip",
			url:     "http://127.0.0.1:8080",
			wantErr: "private IP address",
		},
		{
			name:    "blocks non-http scheme",
			url:     "ftp://1.1.1.1",
			wantErr: "only http and https URLs are supported",
		},
		{
			name: "allows public http url",
			url:  "https://1.1.1.1",
		},
		{
			name:   "allows explicitly allowlisted localhost hostname",
			url:    "http://localhost:8080",
			policy: SSRFPolicy{AllowedHostnames: []string{"localhost"}},
		},
		{
			name:   "allows wildcard allowlisted hostname",
			url:    "http://dev.example.internal:8080",
			policy: SSRFPolicy{HostnameAllowlist: []string{"*.example.internal"}},
		},
		{
			name:   "allows private network when enabled",
			url:    "http://127.0.0.1:8080",
			policy: SSRFPolicy{AllowPrivateNetwork: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBrowserTargetURL(tt.url, tt.policy)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateBrowserTargetURL(%q) unexpected error: %v", tt.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateBrowserTargetURL(%q) expected error", tt.url)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateBrowserTargetURL(%q) error = %q, want substring %q", tt.url, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateBrowserObservedURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		policy  SSRFPolicy
		wantErr string
	}{
		{
			name: "allows chrome error pages",
			url:  "chrome-error://chromewebdata/",
		},
		{
			name: "allows blank pages",
			url:  "about:blank",
		},
		{
			name:    "blocks observed file url",
			url:     "file:///etc/passwd",
			wantErr: "file URLs are not allowed",
		},
		{
			name:    "blocks observed private network url",
			url:     "http://10.0.0.1/",
			wantErr: "private IP address",
		},
		{
			name:   "allows allowlisted observed url",
			url:    "http://localhost:8080/health",
			policy: SSRFPolicy{AllowedHostnames: []string{"localhost"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBrowserObservedURL(tt.url, tt.policy)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateBrowserObservedURL(%q) unexpected error: %v", tt.url, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateBrowserObservedURL(%q) expected error", tt.url)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateBrowserObservedURL(%q) error = %q, want substring %q", tt.url, err.Error(), tt.wantErr)
			}
		})
	}
}
