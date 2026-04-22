package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- Validate reject tests ----

func TestValidate_RejectsLoopbackIPv4(t *testing.T) {
	_, _, err := Validate("http://127.0.0.1/x")
	if err == nil {
		t.Fatal("expected error for loopback IPv4, got nil")
	}
}

func TestValidate_RejectsLoopbackIPv6(t *testing.T) {
	_, _, err := Validate("http://[::1]/x")
	if err == nil {
		t.Fatal("expected error for loopback IPv6, got nil")
	}
}

func TestValidate_RejectsLinkLocal(t *testing.T) {
	_, _, err := Validate("http://169.254.169.254/latest/meta-data")
	if err == nil {
		t.Fatal("expected error for cloud-metadata link-local address, got nil")
	}
}

func TestValidate_RejectsRFC1918(t *testing.T) {
	cases := []string{
		"http://10.1.2.3/",
		"http://172.16.0.1/",
		"http://192.168.0.1/",
	}
	for _, rawURL := range cases {
		t.Run(rawURL, func(t *testing.T) {
			_, _, err := Validate(rawURL)
			if err == nil {
				t.Fatalf("expected SSRF error for %q, got nil", rawURL)
			}
		})
	}
}

func TestValidate_RejectsMulticast(t *testing.T) {
	_, _, err := Validate("http://224.0.0.1/")
	if err == nil {
		t.Fatal("expected error for multicast address, got nil")
	}
}

func TestValidate_RejectsUnspecified(t *testing.T) {
	_, _, err := Validate("http://0.0.0.0/")
	if err == nil {
		t.Fatal("expected error for unspecified address, got nil")
	}
}

func TestValidate_RejectsNonHTTPScheme(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"gopher://x",
		"ftp://x",
	}
	for _, rawURL := range cases {
		t.Run(rawURL, func(t *testing.T) {
			_, _, err := Validate(rawURL)
			if err == nil {
				t.Fatalf("expected error for scheme in %q, got nil", rawURL)
			}
		})
	}
}

func TestValidate_RejectsHostThatResolvesToBlockedIP(t *testing.T) {
	// "localhost." (with trailing dot) forces a DNS lookup that resolves to 127.0.0.1
	// on virtually all systems. If it somehow doesn't resolve, the test still passes
	// because an unresolvable host is also rejected.
	_, _, err := Validate("http://localhost./")
	if err == nil {
		t.Fatal("expected error: localhost resolves to loopback, got nil")
	}
}

func TestValidate_AcceptsPublicLiteralIP(t *testing.T) {
	// 93.184.216.34 is the well-known example.com IP — a stable public address.
	// Use a literal IP to avoid DNS flakiness in CI.
	_, ip, err := Validate("http://93.184.216.34/")
	if err != nil {
		t.Fatalf("expected public IP to be accepted, got error: %v", err)
	}
	if ip == nil {
		t.Fatal("expected non-nil pinned IP")
	}
}

// ---- NewSafeClient tests ----

func TestNewSafeClient_NoRedirects(t *testing.T) {
	// Server that always redirects to /dest.
	SetAllowLoopbackForTest(true)
	defer SetAllowLoopbackForTest(false)

	redirectTarget := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, redirectTarget+"/dest", http.StatusFound)
			return
		}
		// /dest endpoint — should never be reached.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	redirectTarget = srv.URL

	_, pinnedIP, err := validate(srv.URL+"/", true)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	client := NewSafeClient(5e9) // 5s

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req = req.WithContext(WithPinnedIP(req.Context(), pinnedIP))

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	// Must receive the 302, not follow it.
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status=%d, want 302 (redirect not followed)", resp.StatusCode)
	}
}

func TestNewSafeClient_DialPinned(t *testing.T) {
	// httptest.NewServer binds to 127.0.0.1. Validate (without loopback bypass)
	// must reject it, proving the pre-dial check is active on the real path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, _, err := Validate(srv.URL + "/")
	if err == nil {
		t.Fatal("expected SSRF block for httptest server (127.0.0.1), got nil")
	}
}

func TestNewSafeClient_RejectsNoPinnedIP(t *testing.T) {
	// If the caller forgot to call WithPinnedIP, the DialContext must reject.
	SetAllowLoopbackForTest(true)
	defer SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewSafeClient(5e9)

	// Build request WITHOUT pinned IP in context.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	_, err = client.Do(req)
	if err == nil {
		t.Fatal("expected error when no pinned IP in context, got nil")
	}
}
