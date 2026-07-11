package gateway

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicURLSnapshot_StartsEmpty(t *testing.T) {
	s := NewPublicURLSnapshot()
	if got := s.Get(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestPublicURLSnapshot_SetAndGet(t *testing.T) {
	s := NewPublicURLSnapshot()
	s.Set("https://goclaw.tamgiac.com")
	if got := s.Get(); got != "https://goclaw.tamgiac.com" {
		t.Errorf("got %q", got)
	}
}

func TestPublicURLSnapshot_SetIgnoresEmpty(t *testing.T) {
	s := NewPublicURLSnapshot()
	s.Set("https://existing.com")
	s.Set("") // must NOT clobber
	if got := s.Get(); got != "https://existing.com" {
		t.Errorf("empty Set must not overwrite, got %q", got)
	}
}

func TestPublicURLSnapshot_Update_FromRequest(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		fwdHost   string
		fwdProto  string
		hasTLS    bool
		wantURL   string
	}{
		{
			name:     "behind_cloudflare_tunnel",
			host:     "internal-lb:8080",
			fwdHost:  "goclaw.tamgiac.com",
			fwdProto: "https",
			wantURL:  "https://goclaw.tamgiac.com",
		},
		{
			name:    "direct_tls",
			host:    "goclaw.example.com",
			hasTLS:  true,
			wantURL: "https://goclaw.example.com",
		},
		{
			name:    "direct_http_no_proxy",
			host:    "127.0.0.1:8080",
			wantURL: "http://127.0.0.1:8080",
		},
		{
			name:     "xforwarded_host_comma_list",
			host:     "internal",
			fwdHost:  "edge1.example.com, edge2.example.com",
			fwdProto: "https",
			wantURL:  "https://edge1.example.com",
		},
		{
			name:     "websocket_upgrade_wss_normalizes_to_https",
			host:     "internal-lb:8080",
			fwdHost:  "goclaw.tamgiac.com",
			fwdProto: "wss",
			wantURL:  "https://goclaw.tamgiac.com",
		},
		{
			name:     "websocket_upgrade_ws_normalizes_to_http",
			host:     "internal-lb:8080",
			fwdHost:  "goclaw.tamgiac.com",
			fwdProto: "ws",
			wantURL:  "http://goclaw.tamgiac.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewPublicURLSnapshot()
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			req.Host = tc.host
			if tc.fwdHost != "" {
				req.Header.Set("X-Forwarded-Host", tc.fwdHost)
			}
			if tc.fwdProto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.fwdProto)
			}
			if tc.hasTLS {
				req.TLS = &tls.ConnectionState{}
			}
			got := s.Update(req)
			if got != tc.wantURL {
				t.Errorf("Update returned %q, want %q", got, tc.wantURL)
			}
			if stored := s.Get(); stored != tc.wantURL {
				t.Errorf("stored %q, want %q", stored, tc.wantURL)
			}
		})
	}
}

func TestPublicURLSnapshot_Update_EmptyHost_NoChange(t *testing.T) {
	s := NewPublicURLSnapshot()
	s.Set("https://existing.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "" // emulate weird request with no Host
	if got := s.Update(req); got != "" {
		t.Errorf("Update should return empty on missing host, got %q", got)
	}
	if stored := s.Get(); stored != "https://existing.com" {
		t.Errorf("empty Update must not clobber existing, got %q", stored)
	}
}

// TestPublicURLSnapshot_SetIfPublic_AcceptsPublic verifies that legitimate
// public-internet URLs flow into the snapshot.
func TestPublicURLSnapshot_SetIfPublic_AcceptsPublic(t *testing.T) {
	cases := []string{
		"https://goclaw.tamgiac.com",
		"https://goclaw.tamgiac.com:8443",
		"http://app.example.co.uk",
		"https://203.0.113.10", // TEST-NET-3 (documentation), not private
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			s := NewPublicURLSnapshot()
			if ok := s.SetIfPublic(url); !ok {
				t.Errorf("SetIfPublic(%q) = false, want true", url)
			}
			if s.Get() != url {
				t.Errorf("Get() = %q, want %q", s.Get(), url)
			}
		})
	}
}

// TestPublicURLSnapshot_SetIfPublic_RejectsPrivate documents every host class
// we refuse: loopback, RFC1918, link-local, IPv6 variants, and the reserved
// "localhost" hostname. These would all be useless install URLs from
// Bitrix24's perspective (Bitrix24 servers cannot reach a developer's
// localhost) and would only poison the snapshot for other admins.
func TestPublicURLSnapshot_SetIfPublic_RejectsPrivate(t *testing.T) {
	cases := []string{
		"http://localhost:8080",
		"http://LocalHost:8080", // case-insensitive
		"http://app.localhost",
		"http://127.0.0.1:8080",
		"http://127.10.20.30",
		"http://192.168.1.5:443",
		"http://10.0.0.1",
		"http://172.16.0.1",
		"http://172.31.255.254", // top of 172.16/12 range
		"http://169.254.169.254:80",
		"http://[::1]:8080",
		"http://0.0.0.0:8080",
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			s := NewPublicURLSnapshot()
			s.Set("https://existing-public.example.com") // preload — must NOT be overwritten
			if ok := s.SetIfPublic(url); ok {
				t.Errorf("SetIfPublic(%q) = true, want false", url)
			}
			if s.Get() != "https://existing-public.example.com" {
				t.Errorf("private/loopback host overwrote existing public URL: %q", s.Get())
			}
		})
	}
}

// TestPublicURLSnapshot_SetIfPublic_RejectsMalformed covers the
// not-a-real-URL case — Set ought to fail rather than store garbage.
func TestPublicURLSnapshot_SetIfPublic_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"not a url",
		"://missing-scheme",
		"https://", // no host
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			s := NewPublicURLSnapshot()
			if ok := s.SetIfPublic(url); ok {
				t.Errorf("SetIfPublic(%q) = true, want false", url)
			}
		})
	}
}

func TestPublicURLSnapshot_Middleware_InvokesNext(t *testing.T) {
	s := NewPublicURLSnapshot()
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Host = "goclaw.tamgiac.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	s.Middleware(next).ServeHTTP(rec, req)

	if !nextCalled {
		t.Error("middleware did not call next handler")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("downstream status not propagated, got %d", rec.Code)
	}
	if got := s.Get(); got != "https://goclaw.tamgiac.com" {
		t.Errorf("middleware did not update snapshot, got %q", got)
	}
}
