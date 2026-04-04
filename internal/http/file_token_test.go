package http

import (
	"strings"
	"testing"
)

func TestSignMediaPath(t *testing.T) {
	secret := "test-secret-key"

	tests := []struct {
		name     string
		rawPath  string
		wantBase string // expected URL path prefix (before ?ft=)
		wantEmpty bool
	}{
		{
			name:     "clean absolute path",
			rawPath:  "/app/workspace/teams/abc/login.html",
			wantBase: "/v1/files/app/workspace/teams/abc/login.html",
		},
		{
			name:     "single /v1/files/ prefix",
			rawPath:  "/v1/files/app/workspace/login.html",
			wantBase: "/v1/files/app/workspace/login.html",
		},
		{
			name:     "double /v1/files/ prefix (legacy bug)",
			rawPath:  "/v1/files/v1/files/app/workspace/login.html",
			wantBase: "/v1/files/app/workspace/login.html",
		},
		{
			name:     "triple /v1/files/ prefix (legacy bug)",
			rawPath:  "/v1/files/v1/files/v1/files/app/workspace/login.html",
			wantBase: "/v1/files/app/workspace/login.html",
		},
		{
			name:     "stale ?ft= token stripped",
			rawPath:  "/v1/files/app/workspace/login.html?ft=old.123",
			wantBase: "/v1/files/app/workspace/login.html",
		},
		{
			name:     "stacked prefixes and tokens (legacy corruption)",
			rawPath:  "/v1/files/v1/files/app/workspace/login.html?ft=old1.1?ft=old2.2",
			wantBase: "/v1/files/app/workspace/login.html",
		},
		{
			name:     "/v1/media/ prefix stripped",
			rawPath:  "/v1/media/app/workspace/image.png",
			wantBase: "/v1/files/app/workspace/image.png",
		},
		{
			name:      "empty path returns empty",
			rawPath:   "",
			wantEmpty: true,
		},
		{
			name:      "path traversal rejected",
			rawPath:   "/app/workspace/../../etc/passwd",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SignMediaPath(tt.rawPath, secret)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty, got %q", result)
				}
				return
			}

			if !strings.HasPrefix(result, tt.wantBase+"?ft=") {
				t.Errorf("expected prefix %q?ft=..., got %q", tt.wantBase, result)
			}

			// Must have exactly one ?ft= token
			if strings.Count(result, "?ft=") != 1 {
				t.Errorf("expected exactly one ?ft= token, got %d in %q", strings.Count(result, "?ft="), result)
			}

			// Must have exactly one /v1/files/ prefix
			if strings.Count(result, "/v1/files/") != 1 {
				t.Errorf("expected exactly one /v1/files/ prefix, got %d in %q", strings.Count(result, "/v1/files/"), result)
			}
		})
	}
}
