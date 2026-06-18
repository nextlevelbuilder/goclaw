package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSafeDialControl is the core regression for the redirect-SSRF fix: the dial
// guard rejects by RESOLVED IP, not hostname string. Control always receives a
// concrete ip:port, so these cover the "hostname → private IP" class deterministically
// without any real DNS.
func TestSafeDialControl(t *testing.T) {
	cases := []struct {
		name    string
		addr    string
		blocked bool
	}{
		{"loopback v4", "127.0.0.1:443", true},
		{"loopback v6", "[::1]:443", true},
		{"cloud metadata", "169.254.169.254:80", true},
		{"link-local v6", "[fe80::1]:80", true},
		{"private 10", "10.1.2.3:80", true},
		{"private 172", "172.16.5.5:80", true},
		{"private 192", "192.168.1.1:80", true},
		{"ula v6", "[fc00::1]:80", true},
		{"multicast", "224.0.0.1:80", true},
		{"unspecified", "0.0.0.0:80", true},
		{"public v4", "8.8.8.8:443", false},
		{"public v6", "[2606:4700:4700::1111]:443", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := safeDialControl("tcp", tc.addr, nil)
			switch {
			case tc.blocked && err == nil:
				t.Errorf("safeDialControl(%q) = nil, want blocked", tc.addr)
			case !tc.blocked && err != nil:
				t.Errorf("safeDialControl(%q) = %v, want allowed", tc.addr, err)
			}
		})
	}
}

func TestSafeDialControl_Malformed(t *testing.T) {
	if err := safeDialControl("tcp", "not-an-addr", nil); err == nil {
		t.Error("expected error for malformed dial address")
	}
	// Control only ever receives a resolved IP; a non-IP host is a hard failure.
	if err := safeDialControl("tcp", "example.com:80", nil); err == nil {
		t.Error("expected error for non-IP dial host")
	}
}

func TestSafeDialControl_TestBypass(t *testing.T) {
	SetAllowLoopbackForTest(true)
	defer SetAllowLoopbackForTest(false)
	if err := safeDialControl("tcp", "127.0.0.1:443", nil); err != nil {
		t.Errorf("with test bypass, loopback should be allowed: %v", err)
	}
}

// TestRedirectFollowingSafeClient_BlocksLoopbackDial proves the wired client
// refuses to connect to a loopback target at dial time (default, no bypass).
func TestRedirectFollowingSafeClient_BlocksLoopbackDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewRedirectFollowingSafeClient(5*time.Second, 5)
	_, err := c.Get(srv.URL)
	if err == nil {
		t.Fatal("expected loopback dial to be blocked by the SSRF guard")
	}
	if !strings.Contains(err.Error(), "blocked range") {
		t.Errorf("error %q does not mention the SSRF block", err)
	}
}

// TestRedirectFollowingSafeClient_AllowsWithBypass proves the happy path: with the
// test bypass on, the client connects and follows through to the server.
func TestRedirectFollowingSafeClient_AllowsWithBypass(t *testing.T) {
	SetAllowLoopbackForTest(true)
	defer SetAllowLoopbackForTest(false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewRedirectFollowingSafeClient(5*time.Second, 5)
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("with test bypass, dial should succeed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRedirectFollowingSafeClient_CheckRedirect(t *testing.T) {
	c := NewRedirectFollowingSafeClient(5*time.Second, 2)
	mk := func(u string) *http.Request { r, _ := http.NewRequest(http.MethodGet, u, nil); return r }

	httpsReq := mk("https://cdn.example/x")
	if err := c.CheckRedirect(httpsReq, []*http.Request{mk("https://a/"), mk("https://b/")}); err == nil {
		t.Error("expected error when redirect count reaches the cap")
	}
	if err := c.CheckRedirect(mk("file:///etc/passwd"), nil); err == nil {
		t.Error("expected error for non-http(s) redirect scheme")
	}
	if err := c.CheckRedirect(httpsReq, nil); err != nil {
		t.Errorf("https redirect within cap should be allowed: %v", err)
	}
}
