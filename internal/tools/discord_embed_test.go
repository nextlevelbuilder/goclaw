package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeEmbedSender struct {
	called   bool
	gotCh    string
	gotParam channels.DiscordSendEmbedParams
	result   channels.DiscordSendEmbedResult
	err      error
}

func (f *fakeEmbedSender) fn(_ context.Context, ch string, params channels.DiscordSendEmbedParams) (channels.DiscordSendEmbedResult, error) {
	f.called = true
	f.gotCh = ch
	f.gotParam = params
	return f.result, f.err
}

func embedCtx() context.Context {
	ctx := context.Background()
	ctx = WithToolChannel(ctx, "disc")
	ctx = WithToolChatID(ctx, "1111")
	ctx = WithToolPeerKind(ctx, "group")
	return ctx
}

func TestSendDiscordEmbed_NoSender(t *testing.T) {
	tool := NewSendDiscordEmbedTool()
	res := tool.Execute(embedCtx(), map[string]any{
		"embeds": []any{map[string]any{"title": "x"}},
	})
	if res == nil || !res.IsError {
		t.Fatalf("expected error result when sender unset, got %+v", res)
	}
}

func TestSendDiscordEmbed_RequiresEmbeds(t *testing.T) {
	f := &fakeEmbedSender{}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)

	for _, args := range []map[string]any{
		{}, // missing
		{"embeds": []any{}}, // empty
	} {
		res := tool.Execute(embedCtx(), args)
		if res == nil || !res.IsError {
			t.Errorf("args=%v: want error result, got %+v", args, res)
		}
		if f.called {
			t.Errorf("args=%v: sender should not be called", args)
		}
	}
}

func TestSendDiscordEmbed_ContextFallback(t *testing.T) {
	f := &fakeEmbedSender{result: channels.DiscordSendEmbedResult{MessageID: "m1", ChannelID: "1111"}}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)

	res := tool.Execute(embedCtx(), map[string]any{
		"embeds": []any{map[string]any{"title": "Hi", "description": "there"}},
	})
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	if !f.called {
		t.Fatal("sender was not called")
	}
	if f.gotCh != "disc" {
		t.Errorf("channel = %q, want disc", f.gotCh)
	}
	if f.gotParam.ChannelID != "1111" {
		t.Errorf("channel_id = %q, want 1111 (from ctx)", f.gotParam.ChannelID)
	}
	if len(f.gotParam.Embeds) != 1 || f.gotParam.Embeds[0].Title != "Hi" {
		t.Errorf("embeds decoded wrong: %+v", f.gotParam.Embeds)
	}

	var got channels.DiscordSendEmbedResult
	if err := json.Unmarshal([]byte(res.ForLLM), &got); err != nil {
		t.Fatalf("result JSON: %v", err)
	}
	if got.MessageID != "m1" {
		t.Errorf("result message_id = %q", got.MessageID)
	}
}

func TestSendDiscordEmbed_FullDecode(t *testing.T) {
	f := &fakeEmbedSender{result: channels.DiscordSendEmbedResult{MessageID: "m1"}}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)

	res := tool.Execute(embedCtx(), map[string]any{
		"channel_id": "2222",
		"content":    "heads up",
		"reply_to":   "m0",
		"embeds": []any{
			map[string]any{
				"title":       "Status",
				"description": "ok",
				"url":         "https://example.com",
				"color":       float64(0x2ECC71),
				"timestamp":   "2026-04-21T15:04:05Z",
				"author":      map[string]any{"name": "ci", "url": "https://ex.com/ci", "icon_url": "https://ex.com/i.png"},
				"footer":      map[string]any{"text": "b42", "icon_url": "https://ex.com/f.png"},
				"image":       map[string]any{"url": "https://ex.com/big.png"},
				"thumbnail":   map[string]any{"url": "https://ex.com/th.png"},
				"fields": []any{
					map[string]any{"name": "env", "value": "prod", "inline": true},
					map[string]any{"name": "dur", "value": "12s", "inline": true},
				},
			},
		},
	})
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	p := f.gotParam
	if p.ChannelID != "2222" || p.Content != "heads up" || p.ReplyTo != "m0" {
		t.Errorf("top-level params wrong: %+v", p)
	}
	if len(p.Embeds) != 1 {
		t.Fatalf("embeds count = %d", len(p.Embeds))
	}
	e := p.Embeds[0]
	if e.Title != "Status" || e.URL != "https://example.com" || e.Color != 0x2ECC71 {
		t.Errorf("basic fields wrong: %+v", e)
	}
	if e.Author == nil || e.Author.Name != "ci" {
		t.Error("author not decoded")
	}
	if e.Footer == nil || e.Footer.Text != "b42" {
		t.Error("footer not decoded")
	}
	if e.Image == nil || e.Image.URL != "https://ex.com/big.png" {
		t.Error("image not decoded")
	}
	if e.Thumbnail == nil || e.Thumbnail.URL != "https://ex.com/th.png" {
		t.Error("thumbnail not decoded")
	}
	if len(e.Fields) != 2 || !e.Fields[0].Inline {
		t.Errorf("fields not decoded: %+v", e.Fields)
	}
}

func TestSendDiscordEmbed_TenantBlock(t *testing.T) {
	f := &fakeEmbedSender{}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)

	chTenant := uuid.New()
	tool.SetChannelTenantChecker(func(_ string) (uuid.UUID, bool) { return chTenant, true })

	// Different tenant on context — should block.
	ctx := store.WithTenantID(embedCtx(), uuid.New())
	res := tool.Execute(ctx, map[string]any{
		"embeds": []any{map[string]any{"title": "x"}},
	})
	if res == nil || !res.IsError || !strings.Contains(res.ForLLM, "not accessible") {
		t.Fatalf("expected cross-tenant block, got %+v", res)
	}
	if f.called {
		t.Fatal("sender should not be invoked on tenant block")
	}
}

// TestSendDiscordEmbed_ChannelNotFound covers the tenant-checker path where
// the named channel instance does not exist in the manager. Mirrors the same
// test in create_discord_thread so the coverage shape is consistent.
func TestSendDiscordEmbed_ChannelNotFound(t *testing.T) {
	f := &fakeEmbedSender{}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)
	tool.SetChannelTenantChecker(func(_ string) (uuid.UUID, bool) { return uuid.Nil, false })

	res := tool.Execute(embedCtx(), map[string]any{
		"embeds": []any{map[string]any{"title": "x"}},
	})
	if res == nil || !res.IsError || !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("expected channel-not-found error, got %+v", res)
	}
	if f.called {
		t.Fatal("sender should not be invoked when channel is unknown")
	}
}

// TestSendDiscordEmbed_LegacyChannelBypassesTenantCheck covers the code path
// where the channel instance is tenant-less (uuid.Nil — legacy/config-based
// channels). The tenant check should fall through and the send should
// proceed regardless of any tenant on the context.
func TestSendDiscordEmbed_LegacyChannelBypassesTenantCheck(t *testing.T) {
	f := &fakeEmbedSender{result: channels.DiscordSendEmbedResult{MessageID: "m1"}}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)
	tool.SetChannelTenantChecker(func(_ string) (uuid.UUID, bool) { return uuid.Nil, true })

	// A context tenant is set; channel is legacy (uuid.Nil), so mismatch doesn't apply.
	ctx := store.WithTenantID(embedCtx(), uuid.New())
	res := tool.Execute(ctx, map[string]any{
		"embeds": []any{map[string]any{"title": "x"}},
	})
	if res == nil || res.IsError {
		t.Fatalf("expected success on legacy channel, got %+v", res)
	}
	if !f.called {
		t.Fatal("sender should run for legacy channel regardless of ctx tenant")
	}
}

func TestSendDiscordEmbed_SenderError(t *testing.T) {
	f := &fakeEmbedSender{err: errors.New("HTTP 403")}
	tool := NewSendDiscordEmbedTool()
	tool.SetDiscordEmbedSender(f.fn)

	res := tool.Execute(embedCtx(), map[string]any{
		"embeds": []any{map[string]any{"title": "x"}},
	})
	if res == nil || !res.IsError || !strings.Contains(res.ForLLM, "HTTP 403") {
		t.Fatalf("expected wrapped error, got %+v", res)
	}
}
