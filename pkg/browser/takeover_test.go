package browser

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/browsertakeover"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func takeoverCtx(channel, user string) context.Context {
	ctx := tools.WithToolChannel(context.Background(), channel)
	if user != "" {
		ctx = store.WithUserID(ctx, user)
	}
	return ctx
}

func TestTakeover_VisibleOnlyForLocalWindowedChrome(t *testing.T) {
	cases := map[string]struct {
		opts []Option
		want bool
	}{
		"default":  {nil, true},
		"headless": {[]Option{WithHeadless(true)}, false},
		"remote":   {[]Option{WithRemoteURL("ws://chrome:9222")}, false},
	}
	for name, c := range cases {
		if got := New(c.opts...).Visible(); got != c.want {
			t.Errorf("%s: Visible() = %v, want %v", name, got, c.want)
		}
	}
}

func TestTakeover_RefusedWhereTheUserCannotActOrIsNotInTheWebUI(t *testing.T) {
	headless := NewBrowserTool(New(WithHeadless(true)))
	res := headless.Execute(takeoverCtx("ws", "u1"), map[string]any{"action": "takeover"})
	if !res.IsError || !strings.Contains(res.ForLLM, "visible browser") {
		t.Fatalf("headless: %+v", res)
	}

	windowed := NewBrowserTool(New())
	res = windowed.Execute(takeoverCtx("telegram", "u1"), map[string]any{"action": "takeover"})
	if !res.IsError || !strings.Contains(res.ForLLM, "Web UI") {
		t.Fatalf("non-ws channel: %+v", res)
	}
	res = windowed.Execute(takeoverCtx("ws", ""), map[string]any{"action": "takeover"})
	if !res.IsError || !strings.Contains(res.ForLLM, "signed-in") {
		t.Fatalf("no user: %+v", res)
	}
}

func TestTakeover_TabIsGuardedAndMaskedAfterwards(t *testing.T) {
	m := New()
	m.enterSecretSession("tab1", "someone", "https://a.example.com/", false, 5) // pre-existing session of another owner
	owner := secretOwnerKey(BrowserScope{}, "s1")
	m.enterTakeoverSession("tab1", owner, "https://a.example.com/login")

	// Same origin keeps the existing session, so its owner still decides who may touch the tab.
	m.mu.Lock()
	s := m.secretTabs["tab1"]
	m.mu.Unlock()
	if s == nil || !s.maskAll {
		t.Fatalf("take over must mark the tab to mask every input: %+v", s)
	}

	m.enterTakeoverSession("tab2", owner, "https://login.example.com/mfa")
	ctx := context.Background()
	for _, op := range []string{"evaluate", "wait_fn", "console", "screenshot"} {
		if err := m.guardSecretTab(ctx, "tab2", op, "s1"); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("%s must be closed after take over: %v", op, err)
		}
	}
	for _, op := range []string{"snapshot", "click", "type", "navigate"} {
		if err := m.guardSecretTab(ctx, "tab2", op, "s1"); err != nil {
			t.Errorf("%s must stay allowed: %v", op, err)
		}
	}
	if err := m.guardSecretTab(ctx, "tab2", "snapshot", "other-run"); err == nil || !strings.Contains(err.Error(), "tab not found") {
		t.Errorf("another run must not touch the tab: %v", err)
	}
}

func TestTakeover_NoWebUISessionFailsFast(t *testing.T) {
	// The tool needs a page to get as far as the manager, which needs Chrome;
	// the manager contract itself is covered in internal/browsertakeover.
	mgr := browsertakeover.New()
	if _, err := mgr.Request(context.Background(), browsertakeover.Spec{User: "u1", TargetID: "t"}); err == nil {
		t.Fatal("an unconfigured manager must refuse")
	}
}
