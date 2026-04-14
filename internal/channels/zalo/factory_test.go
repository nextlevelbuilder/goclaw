package zalo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestFactory_ValidCredsProducesChannel verifies Factory constructs a live
// Channel when credentials and config JSON are well-formed.
func TestFactory_ValidCredsProducesChannel(t *testing.T) {
	creds := []byte(`{"token":"fake-zalo-token","webhook_secret":"hook-sec"}`)
	cfg := []byte(`{"dm_policy":"open","media_max_mb":7,"allow_from":["+84900000000"],"webhook_url":"https://example.test/hook","block_reply":true}`)

	mb := bus.New()
	ch, err := Factory("my-zalo", creds, cfg, mb, nil)
	if err != nil {
		t.Fatalf("Factory error: %v", err)
	}
	if ch == nil {
		t.Fatal("Factory returned nil channel")
	}
	if ch.Name() != "my-zalo" {
		t.Errorf("Name() = %q, want my-zalo", ch.Name())
	}
	zc, ok := ch.(*Channel)
	if !ok {
		t.Fatalf("unexpected channel type %T", ch)
	}
	if zc.token != "fake-zalo-token" {
		t.Errorf("token = %q, want fake-zalo-token", zc.token)
	}
	if zc.dmPolicy != "open" {
		t.Errorf("dmPolicy = %q, want open", zc.dmPolicy)
	}
	if zc.mediaMaxMB != 7 {
		t.Errorf("mediaMaxMB = %d, want 7", zc.mediaMaxMB)
	}
	if zc.blockReply == nil || !*zc.blockReply {
		t.Errorf("blockReply = %v, want true", zc.blockReply)
	}
}

// TestFactory_ErrorCases covers the error branches in order:
// - missing token in creds
// - malformed creds JSON
// - malformed config JSON
// - empty creds bytes (missing required token after unmarshal)
func TestFactory_ErrorCases(t *testing.T) {
	mb := bus.New()
	cases := []struct {
		name    string
		creds   []byte
		cfg     []byte
		wantSub string
	}{
		{
			name:    "missing token",
			creds:   []byte(`{"webhook_secret":"x"}`),
			cfg:     []byte(`{}`),
			wantSub: "token is required",
		},
		{
			name:    "malformed creds",
			creds:   []byte(`{not-json`),
			cfg:     []byte(`{}`),
			wantSub: "decode zalo credentials",
		},
		{
			name:    "malformed config",
			creds:   []byte(`{"token":"t"}`),
			cfg:     []byte(`{malformed`),
			wantSub: "decode zalo config",
		},
		{
			name:    "empty creds bytes",
			creds:   nil,
			cfg:     []byte(`{}`),
			wantSub: "token is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Factory("zalo-x", tc.creds, tc.cfg, mb, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

// TestNew_DefaultsWhenFieldsUnset verifies New() applies defaults:
// - dm_policy defaults to "pairing"
// - media_max_mb defaults to defaultMediaMaxMB when ≤0
func TestNew_DefaultsWhenFieldsUnset(t *testing.T) {
	mb := bus.New()
	ch, err := New(config.ZaloConfig{Token: "t"}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ch.dmPolicy != "pairing" {
		t.Errorf("dmPolicy = %q, want pairing (default)", ch.dmPolicy)
	}
	if ch.mediaMaxMB != defaultMediaMaxMB {
		t.Errorf("mediaMaxMB = %d, want %d", ch.mediaMaxMB, defaultMediaMaxMB)
	}
	// block_reply should be nil (inherit gateway default).
	if got := ch.BlockReplyEnabled(); got != nil {
		t.Errorf("BlockReplyEnabled() = %v, want nil", got)
	}
}

// TestNew_EmptyTokenRejected verifies New() rejects empty token.
func TestNew_EmptyTokenRejected(t *testing.T) {
	mb := bus.New()
	if _, err := New(config.ZaloConfig{}, mb, nil); err == nil {
		t.Fatal("expected error on empty token, got nil")
	}
}

// TestNew_CustomValuesPreserved verifies caller-provided values stick.
func TestNew_CustomValuesPreserved(t *testing.T) {
	mb := bus.New()
	blockReply := false
	ch, err := New(config.ZaloConfig{
		Token:      "xyz",
		DMPolicy:   "open",
		MediaMaxMB: 12,
		BlockReply: &blockReply,
	}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ch.dmPolicy != "open" {
		t.Errorf("dmPolicy = %q, want open", ch.dmPolicy)
	}
	if ch.mediaMaxMB != 12 {
		t.Errorf("mediaMaxMB = %d, want 12", ch.mediaMaxMB)
	}
	if got := ch.BlockReplyEnabled(); got == nil || *got != false {
		t.Errorf("BlockReplyEnabled() = %v, want false pointer", got)
	}
}

// TestFactoryConfigWithoutOptionals verifies minimal config still parses —
// zero values for non-required fields are accepted.
func TestFactoryConfigWithoutOptionals(t *testing.T) {
	mb := bus.New()
	creds := []byte(`{"token":"t"}`)
	cfg := []byte(`{}`)

	ch, err := Factory("zalo", creds, cfg, mb, nil)
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	zc := ch.(*Channel)
	if zc.dmPolicy != "pairing" {
		t.Errorf("dmPolicy = %q, want pairing (default)", zc.dmPolicy)
	}
}

// Sanity guard: factory must accept the same JSON shapes that
// zaloInstanceConfig declares; ensure deserialization matches struct tags.
func TestZaloInstanceConfigRoundTrip(t *testing.T) {
	src := zaloInstanceConfig{
		DMPolicy:   "pairing",
		WebhookURL: "https://example.test",
		MediaMaxMB: 3,
		AllowFrom:  []string{"user1", "user2"},
	}
	data, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var got zaloInstanceConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.DMPolicy != src.DMPolicy || got.MediaMaxMB != src.MediaMaxMB {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, src)
	}
	if len(got.AllowFrom) != 2 {
		t.Errorf("AllowFrom len = %d, want 2", len(got.AllowFrom))
	}
}
