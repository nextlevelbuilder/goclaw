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

type fakeCreator struct {
	called   bool
	gotCh    string
	gotParam channels.DiscordThreadParams
	result   channels.DiscordThreadResult
	err      error
}

func (f *fakeCreator) fn(ctx context.Context, ch string, params channels.DiscordThreadParams) (channels.DiscordThreadResult, error) {
	f.called = true
	f.gotCh = ch
	f.gotParam = params
	return f.result, f.err
}

func baseCtx() context.Context {
	ctx := context.Background()
	ctx = WithToolChannel(ctx, "disc")
	ctx = WithToolChatID(ctx, "1111")
	ctx = WithToolPeerKind(ctx, "group")
	return ctx
}

func TestCreateDiscordThread_NoCreator(t *testing.T) {
	tool := NewCreateDiscordThreadTool()
	res := tool.Execute(baseCtx(), map[string]any{"name": "x"})
	if res == nil || !res.IsError {
		t.Fatalf("expected error result when creator is unset, got %+v", res)
	}
}

func TestCreateDiscordThread_NameValidation(t *testing.T) {
	f := &fakeCreator{}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)

	cases := map[string]any{
		"missing": nil,
		"empty":   "",
		"too-long": strings.Repeat("a", 101),
	}
	for label, name := range cases {
		args := map[string]any{}
		if name != nil {
			args["name"] = name
		}
		res := tool.Execute(baseCtx(), args)
		if res == nil || !res.IsError {
			t.Fatalf("%s: expected error result, got %+v", label, res)
		}
		if f.called {
			t.Fatalf("%s: creator should not be called for invalid name", label)
		}
	}
}

func TestCreateDiscordThread_ContextFallback(t *testing.T) {
	f := &fakeCreator{result: channels.DiscordThreadResult{ThreadID: "t1", Name: "hello", ParentChannelID: "1111"}}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)

	res := tool.Execute(baseCtx(), map[string]any{"name": "hello"})
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}
	if !f.called {
		t.Fatal("creator not called")
	}
	if f.gotCh != "disc" {
		t.Errorf("channel: got %q want %q", f.gotCh, "disc")
	}
	if f.gotParam.ChannelID != "1111" {
		t.Errorf("channel_id fallback: got %q want %q", f.gotParam.ChannelID, "1111")
	}
}

func TestCreateDiscordThread_DMRejected(t *testing.T) {
	f := &fakeCreator{}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)

	ctx := baseCtx()
	ctx = WithToolPeerKind(ctx, "direct")

	res := tool.Execute(ctx, map[string]any{"name": "hello"})
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for DM, got %+v", res)
	}
	if !strings.Contains(res.ForLLM, "DM") {
		t.Errorf("expected DM-specific error, got %q", res.ForLLM)
	}
	if f.called {
		t.Fatal("creator should not be called in DM")
	}
}

// TestCreateDiscordThread_TenantMismatch is the critical regression test for the
// cross-tenant thread creation gate.
func TestCreateDiscordThread_TenantMismatch(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()

	f := &fakeCreator{}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)
	tool.SetChannelTenantChecker(func(ch string) (uuid.UUID, bool) {
		if ch == "disc-b" {
			return tenantB, true
		}
		return uuid.Nil, false
	})

	ctx := baseCtx()
	ctx = WithToolChannel(ctx, "disc-b")
	ctx = store.WithTenantID(ctx, tenantA)

	res := tool.Execute(ctx, map[string]any{"name": "hello"})
	if res == nil || !res.IsError {
		t.Fatalf("expected tenant-mismatch error, got %+v", res)
	}
	if f.called {
		t.Fatal("creator must NOT be called on tenant mismatch")
	}
	if !strings.Contains(res.ForLLM, "tenant") {
		t.Errorf("expected tenant-specific error, got %q", res.ForLLM)
	}
}

func TestCreateDiscordThread_TenantMatch(t *testing.T) {
	tenantA := uuid.New()

	f := &fakeCreator{result: channels.DiscordThreadResult{ThreadID: "t1", Name: "ok", ParentChannelID: "1111"}}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)
	tool.SetChannelTenantChecker(func(ch string) (uuid.UUID, bool) {
		return tenantA, true
	})

	ctx := baseCtx()
	ctx = store.WithTenantID(ctx, tenantA)

	res := tool.Execute(ctx, map[string]any{"name": "ok"})
	if res == nil || res.IsError {
		t.Fatalf("expected success on tenant match, got %+v", res)
	}
	if !f.called {
		t.Fatal("creator should have been called")
	}
}

func TestCreateDiscordThread_LegacyChannelBypassesTenantCheck(t *testing.T) {
	tenantA := uuid.New()

	f := &fakeCreator{result: channels.DiscordThreadResult{ThreadID: "t1", Name: "ok", ParentChannelID: "1111"}}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)
	tool.SetChannelTenantChecker(func(ch string) (uuid.UUID, bool) {
		return uuid.Nil, true // legacy/config-based channel — no tenant
	})

	ctx := baseCtx()
	ctx = store.WithTenantID(ctx, tenantA)

	res := tool.Execute(ctx, map[string]any{"name": "ok"})
	if res == nil || res.IsError {
		t.Fatalf("expected success for legacy channel, got %+v", res)
	}
	if !f.called {
		t.Fatal("creator should have been called for legacy channel")
	}
}

func TestCreateDiscordThread_ChannelNotFound(t *testing.T) {
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator((&fakeCreator{}).fn)
	tool.SetChannelTenantChecker(func(ch string) (uuid.UUID, bool) {
		return uuid.Nil, false
	})

	ctx := baseCtx()
	res := tool.Execute(ctx, map[string]any{"name": "ok"})
	if res == nil || !res.IsError {
		t.Fatalf("expected error for unknown channel, got %+v", res)
	}
	if !strings.Contains(res.ForLLM, "not found") {
		t.Errorf("expected not-found error, got %q", res.ForLLM)
	}
}

func TestCreateDiscordThread_HappyPathJSON(t *testing.T) {
	f := &fakeCreator{result: channels.DiscordThreadResult{
		ThreadID:        "99",
		Name:            "discuss",
		ParentChannelID: "1111",
		IsForum:         true,
	}}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)

	args := map[string]any{
		"name":                 "discuss",
		"auto_archive_minutes": float64(4320),
		"private":              true,
		"initial_message":      "kickoff",
		"applied_tags":         []any{"tag-a", "tag-b"},
		"message_id":           "m1",
	}
	res := tool.Execute(baseCtx(), args)
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(res.ForLLM), &out); err != nil {
		t.Fatalf("json unmarshal: %v; payload=%s", err, res.ForLLM)
	}
	if out["thread_id"] != "99" || out["name"] != "discuss" || out["is_forum"] != true || out["parent_channel_id"] != "1111" {
		t.Errorf("unexpected result fields: %+v", out)
	}

	// Parameters propagated.
	if f.gotParam.AutoArchiveMinutes != 4320 || !f.gotParam.Private || f.gotParam.InitialMessage != "kickoff" || f.gotParam.MessageID != "m1" {
		t.Errorf("params not propagated: %+v", f.gotParam)
	}
	if len(f.gotParam.AppliedTags) != 2 || f.gotParam.AppliedTags[0] != "tag-a" || f.gotParam.AppliedTags[1] != "tag-b" {
		t.Errorf("applied_tags not propagated: %+v", f.gotParam.AppliedTags)
	}
}

func TestCreateDiscordThread_CreatorError(t *testing.T) {
	f := &fakeCreator{err: errors.New("discord 403")}
	tool := NewCreateDiscordThreadTool()
	tool.SetDiscordThreadCreator(f.fn)

	res := tool.Execute(baseCtx(), map[string]any{"name": "ok"})
	if res == nil || !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	if !strings.Contains(res.ForLLM, "discord 403") {
		t.Errorf("expected underlying error surfaced, got %q", res.ForLLM)
	}
}
