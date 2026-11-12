package mattermost

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// withPrivateAllowed sets the operator opt-in for one test.
func withPrivateAllowed(t *testing.T, allowed bool) {
	t.Helper()
	prev := allowPrivateFn
	allowPrivateFn = func() bool { return allowed }
	t.Cleanup(func() { allowPrivateFn = prev })
}

func TestCheckDialIP(t *testing.T) {
	cases := []struct {
		ip             string
		allowedDefault bool
		allowedPrivate bool
	}{
		{"203.0.113.10", true, true}, // public (TEST-NET-3 is not in the block list)
		{"10.0.0.5", false, true},
		{"192.168.1.20", false, true},
		{"127.0.0.1", false, true},
		{"::1", false, true},
		{"169.254.169.254", false, false}, // cloud metadata: never
		{"fe80::1", false, false},
		{"0.0.0.0", false, false},
		{"224.0.0.1", false, false},
	}
	for _, private := range []bool{false, true} {
		withPrivateAllowed(t, private)
		for _, tc := range cases {
			want := tc.allowedDefault
			if private {
				want = tc.allowedPrivate
			}
			err := checkDialIP(net.ParseIP(tc.ip))
			if (err == nil) != want {
				t.Errorf("private=%v checkDialIP(%s) = %v, want allowed=%v", private, tc.ip, err, want)
			}
			if err != nil && !errors.Is(err, errBlockedAddress) {
				t.Errorf("checkDialIP(%s) error must wrap errBlockedAddress: %v", tc.ip, err)
			}
		}
	}
}

func TestStart_PrivateServerWithoutOptInIsConfigFailure(t *testing.T) {
	fs := newFakeServer(t)
	withPrivateAllowed(t, false)

	ch, err := New(config.MattermostConfig{ServerURL: fs.URL, BotToken: testToken}, bus.New(), nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := ch.Start(context.Background()); err == nil {
		t.Fatal("expected Start to refuse a loopback server without the opt-in")
	}
	if h := ch.HealthSnapshot(); h.FailureKind != channels.ChannelFailureKindConfig || h.Retryable {
		t.Fatalf("health = %+v, want non-retryable config failure", h)
	}
	if len(fs.callsTo("GET", "/api/v4/users/me")) != 0 {
		t.Fatal("no request may reach a blocked address")
	}
}

func TestHTTPClient_RedirectToBlockedAddressIsRefused(t *testing.T) {
	// The redirect target is a link-local (metadata) address, blocked even
	// for internal servers.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	resp, err := newHTTPClient(apiTimeout).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("redirect to cloud metadata must be refused")
	}
	if !errors.Is(err, errBlockedAddress) {
		t.Fatalf("expected egress policy error, got %v", err)
	}
}
