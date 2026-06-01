package bitrix24

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDerivePublicURL_TableDriven covers the full matrix of scheme + host
// resolution branches. Each case sets only the fields a real reverse-proxy
// hop would set, so we surface regressions when somebody "simplifies" the
// header precedence logic.
func TestDerivePublicURL_TableDriven(t *testing.T) {
	cases := []struct {
		name         string
		host         string
		forwardedFor string // X-Forwarded-Host
		proto        string // X-Forwarded-Proto
		hasTLS       bool
		want         string
		wantErr      error
	}{
		{
			name:  "https_via_xforwarded_proto",
			host:  "goclaw.tamgiac.com",
			proto: "https",
			want:  "https://goclaw.tamgiac.com",
		},
		{
			name:   "https_via_r_TLS",
			host:   "goclaw.tamgiac.com",
			hasTLS: true,
			want:   "https://goclaw.tamgiac.com",
		},
		{
			// No TLS and no forwarded proto → honest http. If a reverse proxy
			// is terminating TLS, it must set X-Forwarded-Proto; otherwise
			// imbot.register will (correctly) reject the http URL downstream.
			name: "http_when_no_tls_and_no_proto_header",
			host: "goclaw.tamgiac.com",
			want: "http://goclaw.tamgiac.com",
		},
		{
			name:  "http_when_explicit_proto",
			host:  "goclaw.tamgiac.com",
			proto: "http",
			want:  "http://goclaw.tamgiac.com",
		},
		{
			name:         "xforwarded_host_takes_precedence_over_host",
			host:         "internal-lb:8080",
			forwardedFor: "goclaw.tamgiac.com",
			proto:        "https",
			want:         "https://goclaw.tamgiac.com",
		},
		{
			name:         "xforwarded_host_strips_chain_to_first_hop",
			host:         "internal-lb",
			forwardedFor: "goclaw.tamgiac.com, edge.cloudflare.com",
			proto:        "https",
			want:         "https://goclaw.tamgiac.com",
		},
		{
			name:  "keeps_non_standard_port",
			host:  "tunnel.example.com:8443",
			proto: "https",
			want:  "https://tunnel.example.com:8443",
		},
		{
			name:    "rejects_localhost",
			host:    "localhost",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_localhost_with_port",
			host:    "localhost:8080",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_127_0_0_1",
			host:    "127.0.0.1",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_private_192_168",
			host:    "192.168.1.10",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_private_10",
			host:    "10.0.0.5",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_link_local",
			host:    "169.254.169.254",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_ipv6_loopback",
			host:    "[::1]:8080",
			proto:   "http",
			wantErr: errPublicURLPrivate,
		},
		{
			name:    "rejects_empty_host",
			host:    "",
			proto:   "https",
			wantErr: errPublicURLEmpty,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/bitrix24/install", nil)
			req.Host = tc.host
			if tc.forwardedFor != "" {
				req.Header.Set("X-Forwarded-Host", tc.forwardedFor)
			}
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.hasTLS {
				req.TLS = &tls.ConnectionState{}
			}

			got, err := derivePublicURL(req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIsPrivateOrLoopback_Hostnames covers the non-IP branch: hostnames are
// treated as public, EXCEPT literal "localhost" and "*.localhost" — they're
// reserved and never reach a real network even if DNS resolves them.
func TestIsPrivateOrLoopback_Hostnames(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"goclaw.tamgiac.com", false},
		{"localhost", true},
		{"app.localhost", true},
		{"Localhost", true}, // case-insensitive
		{"my-server", false},
		{"", true}, // empty treated as invalid → reject
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			if got := isPrivateOrLoopback(tc.host); got != tc.want {
				t.Errorf("isPrivateOrLoopback(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

// TestDerivePublicURL_TrimsTrailingSlash documents that the helper does NOT
// strip trailing slashes — that's eventHandlerURL's job. We want the captured
// state to match exactly what Bitrix24 will hit, so a redundant slash here
// would cause a false "changed" comparison on re-install.
func TestDerivePublicURL_PreservesHostAsReceived(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bitrix24/install", nil)
	req.Host = "GoClaw.TamGiac.com" // mixed case
	req.Header.Set("X-Forwarded-Proto", "https")
	got, err := derivePublicURL(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Host casing preserved in URL string — only scheme is lowercased.
	if !strings.HasSuffix(got, "GoClaw.TamGiac.com") {
		t.Errorf("expected host casing preserved, got %q", got)
	}
}
