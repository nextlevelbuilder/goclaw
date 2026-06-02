package bitrix24

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func TestFactory_BareCallReturnsError(t *testing.T) {
	// The bare Factory must refuse to construct — the gateway has to wire
	// FactoryWithPortalStore so the portal store is in scope.
	_, err := Factory("bitrix24", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("bare Factory should error — needs portal store")
	}
}

func TestFactoryWithPortalStore_NilStore(t *testing.T) {
	fn := FactoryWithPortalStore(nil, "")
	_, err := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "nil BitrixPortalStore") {
		t.Fatalf("expected nil-store error, got %v", err)
	}
}

func TestFactoryWithPortalStore_RequiresFields(t *testing.T) {
	fs := newFakeStore()
	fn := FactoryWithPortalStore(fs, "")
	defer resetWebhookRouterForTest()

	cases := []struct {
		name string
		cfg  string
	}{
		{"empty", `{}`},
		{"missing bot_code", `{"portal":"p","bot_name":"n"}`},
		{"missing bot_name", `{"portal":"p","bot_code":"c"}`},
		{"missing portal", `{"bot_code":"c","bot_name":"n"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fn("b1", nil, json.RawMessage(tc.cfg), nil, nil)
			if err == nil {
				t.Fatalf("expected required-field error, got nil")
			}
		})
	}
}

func TestFactoryWithPortalStore_AppliesDefaults(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch, err := fn("b1", nil,
		json.RawMessage(`{"portal":"acme","bot_code":"goclaw","bot_name":"GoClaw Bot"}`),
		&bus.MessageBus{}, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc, ok := ch.(*Channel)
	if !ok {
		t.Fatalf("want *Channel, got %T", ch)
	}
	cfg := bc.Config()
	if cfg.BotType != "B" {
		t.Errorf("BotType default = %q; want \"B\" (Bitrix24 imbot.register default)", cfg.BotType)
	}
	if bc.IsOpenChannelBot() {
		t.Errorf("default bot must not report IsOpenChannelBot()=true")
	}
	if cfg.DMPolicy != string(channels.DMPolicyPairing) {
		t.Errorf("DMPolicy = %q; want pairing", cfg.DMPolicy)
	}
	if cfg.GroupPolicy != string(channels.GroupPolicyOpen) {
		t.Errorf("GroupPolicy = %q; want open", cfg.GroupPolicy)
	}
	if cfg.TextChunkLimit != 4000 {
		t.Errorf("TextChunkLimit = %d; want 4000", cfg.TextChunkLimit)
	}
	if cfg.MediaMaxMB != 20 {
		t.Errorf("MediaMaxMB = %d; want 20", cfg.MediaMaxMB)
	}
	if cfg.ReactionLevel != "minimal" {
		t.Errorf("ReactionLevel = %q; want minimal", cfg.ReactionLevel)
	}
	if cfg.RequireMention == nil || !*cfg.RequireMention {
		t.Errorf("RequireMention should default to true")
	}
	if cfg.Streaming == nil || !*cfg.Streaming {
		t.Errorf("Streaming should default to true")
	}
	if ch.Type() != channels.TypeBitrix24 {
		t.Errorf("Type = %q; want bitrix24", ch.Type())
	}
	if ch.Name() != "b1" {
		t.Errorf("Name = %q; want b1", ch.Name())
	}
}

func TestFactoryWithPortalStore_InvalidJSON(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	if _, err := fn("b1", nil, json.RawMessage(`{not json}`), nil, nil); err == nil {
		t.Fatal("expected JSON decode error")
	}
	if _, err := fn("b1", json.RawMessage(`{not json}`),
		json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`), nil, nil); err == nil {
		t.Fatal("expected creds decode error")
	}
}

func TestFactoryWithPortalStore_RouterSingleton(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	cfg := json.RawMessage(`{"portal":"p1","bot_code":"c1","bot_name":"n1"}`)
	ch1, err := fn("b1", nil, cfg, &bus.MessageBus{}, nil)
	if err != nil {
		t.Fatalf("factory 1: %v", err)
	}
	ch2, err := fn("b2", nil,
		json.RawMessage(`{"portal":"p2","bot_code":"c2","bot_name":"n2"}`),
		&bus.MessageBus{}, nil)
	if err != nil {
		t.Fatalf("factory 2: %v", err)
	}
	r1 := ch1.(*Channel).Router()
	r2 := ch2.(*Channel).Router()
	if r1 != r2 {
		t.Fatal("all channels should share the singleton router")
	}
}

func TestFactoryWithPortalStore_WebhookHandlerFirstClaimWins(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch1, _ := fn("b1", nil, json.RawMessage(`{"portal":"p1","bot_code":"c1","bot_name":"n1"}`),
		&bus.MessageBus{}, nil)
	ch2, _ := fn("b2", nil, json.RawMessage(`{"portal":"p2","bot_code":"c2","bot_name":"n2"}`),
		&bus.MessageBus{}, nil)

	// Only the first channel's WebhookHandler returns a path+handler.
	wc1, ok := ch1.(channels.WebhookChannel)
	if !ok {
		t.Fatal("channel does not implement WebhookChannel")
	}
	wc2 := ch2.(channels.WebhookChannel)

	path1, h1 := wc1.WebhookHandler()
	path2, h2 := wc2.WebhookHandler()

	if path1 == "" || h1 == nil {
		t.Fatalf("first Channel should return a path+handler, got %q / %v", path1, h1)
	}
	if path2 != "" || h2 != nil {
		t.Errorf("second Channel should return empty, got %q / %v", path2, h2)
	}
}

func TestApplyConfigDefaults_RespectsExplicit(t *testing.T) {
	no := false
	cfg := bitrixInstanceConfig{
		BotType:        "O",
		DMPolicy:       "open",
		GroupPolicy:    "allowlist",
		TextChunkLimit: 1000,
		MediaMaxMB:     5,
		ReactionLevel:  "off",
		RequireMention: &no,
		Streaming:      &no,
	}
	applyConfigDefaults(&cfg)
	if cfg.BotType != "O" {
		t.Errorf("explicit BotType was overwritten: %q", cfg.BotType)
	}
	if cfg.DMPolicy != "open" || cfg.GroupPolicy != "allowlist" ||
		cfg.TextChunkLimit != 1000 || cfg.MediaMaxMB != 5 || cfg.ReactionLevel != "off" {
		t.Errorf("explicit values were overwritten: %+v", cfg)
	}
	if *cfg.RequireMention || *cfg.Streaming {
		t.Errorf("explicit bool pointers lost: %+v", cfg)
	}
}

// TestFactoryWithPortalStore_BotType covers all three outcomes of the
// bot_type field: (1) default when omitted, (2) accepted values flow
// through verbatim, and (3) anything else is rejected at load.
//
// Validation happens AFTER applyConfigDefaults, so the "" case exercises
// the default → "B" path, not the rejection path.
func TestFactoryWithPortalStore_BotType(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
		wantTyp string
		wantOC  bool // IsOpenChannelBot()
	}{
		{"default_is_B", `{"portal":"p","bot_code":"c","bot_name":"n"}`, false, "B", false},
		{"explicit_B", `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"B"}`, false, "B", false},
		{"explicit_O_is_open_channel", `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"O"}`, false, "O", true},
		{"reject_lowercase_b", `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"b"}`, true, "", false},
		{"reject_unknown_H", `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":"H"}`, true, "", false},
		{"reject_empty_string_only_if_whitespace", `{"portal":"p","bot_code":"c","bot_name":"n","bot_type":" "}`, true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeStore()
			resetWebhookRouterForTest()
			defer resetWebhookRouterForTest()
			fn := FactoryWithPortalStore(fs, "")

			ch, err := fn("b1", nil, json.RawMessage(tc.cfg), &bus.MessageBus{}, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for bot_type validation, got nil (cfg=%s)", tc.cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			bc := ch.(*Channel)
			if got := bc.Config().BotType; got != tc.wantTyp {
				t.Errorf("BotType = %q; want %q", got, tc.wantTyp)
			}
			if got := bc.IsOpenChannelBot(); got != tc.wantOC {
				t.Errorf("IsOpenChannelBot() = %v; want %v", got, tc.wantOC)
			}
		})
	}
}

func TestMergeAllowLists(t *testing.T) {
	if got := mergeAllowLists(nil, nil); got != nil {
		t.Errorf("both nil should stay nil; got %v", got)
	}
	got := mergeAllowLists([]string{"a", "b"}, []string{"c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d; want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

// sanityContext ensures the Start early-return path on missing tenant id
// doesn't try to hit the store. Covers a common mis-wiring where the
// InstanceLoader forgets SetTenantID before Start.
func TestChannel_Start_MissingTenantID_FailsFast(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")
	ch, err := fn("b1", nil, json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		&bus.MessageBus{}, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if err := ch.Start(context.Background()); err == nil {
		t.Fatal("Start must fail when TenantID is zero")
	}
}
