package bitrix24

import (
	"strings"
	"testing"
)

func TestRedactMCPBody(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		mustHide []string // substrings that must NOT survive
		mustKeep []string // substrings that must survive
	}{
		{
			name:     "access_token echoed in error body",
			in:       `{"error":"bad request","received":{"access_token":"abc123SECRET","domain":"acme.bitrix24.com"}}`,
			mustHide: []string{"abc123SECRET"},
			mustKeep: []string{"acme.bitrix24.com", "[redacted]"},
		},
		{
			name:     "refresh_token + client_secret",
			in:       `{"refresh_token":"rrrSECRET","client_secret":"cccSECRET","ok":false}`,
			mustHide: []string{"rrrSECRET", "cccSECRET"},
			mustKeep: []string{"[redacted]", `"ok":false`},
		},
		{
			name:     "spaced colon still scrubbed",
			in:       `{ "access_token" : "spacedSECRET" }`,
			mustHide: []string{"spacedSECRET"},
			mustKeep: []string{"[redacted]"},
		},
		{
			name:     "no tokens — unchanged",
			in:       `{"error":"tenant_not_installed"}`,
			mustHide: nil,
			mustKeep: []string{`"error":"tenant_not_installed"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactMCPBody(tc.in)
			for _, h := range tc.mustHide {
				if strings.Contains(got, h) {
					t.Errorf("secret %q leaked through redaction: %q", h, got)
				}
			}
			for _, k := range tc.mustKeep {
				if !strings.Contains(got, k) {
					t.Errorf("expected %q to survive, got %q", k, got)
				}
			}
		})
	}
}
