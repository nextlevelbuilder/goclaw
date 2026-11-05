package browser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-rod/rod/lib/proto"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
)

const testSecretValue = "pw-Sup3r-s3cret-77"

func axVal(s string) *proto.AccessibilityAXValue {
	v := &proto.AccessibilityAXValue{}
	b, _ := json.Marshal(map[string]string{"type": "string", "value": s})
	if err := json.Unmarshal(b, v); err != nil {
		panic(err)
	}
	return v
}

func TestCheckFillOrigin(t *testing.T) {
	cases := []struct {
		url  string
		host string
		ok   bool
	}{
		{"https://app.example.com/login", "app.example.com", true},
		{"http://example.com/login", "", false},
		{"http://localhost:3000/login", "localhost", true},
		{"http://127.0.0.1:8080/", "127.0.0.1", true},
		{"about:blank", "", false},
		{"file:///etc/passwd", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		host, err := checkFillOrigin(c.url)
		if (err == nil) != c.ok || host != c.host {
			t.Errorf("checkFillOrigin(%q) = %q, %v; want %q ok=%v", c.url, host, err, c.host, c.ok)
		}
	}
}

func TestSnapshotMasksFilledNodesByBackendID(t *testing.T) {
	nodes := []*proto.AccessibilityAXNode{
		{NodeID: "1", Role: axVal("WebArea"), ChildIDs: []proto.AccessibilityAXNodeID{"2", "3"}},
		{NodeID: "2", Role: axVal("textbox"), Name: axVal("Password"), Value: axVal(testSecretValue), BackendDOMNodeID: 42},
		{NodeID: "3", Role: axVal("textbox"), Name: axVal("Search"), Value: axVal("visible text"), BackendDOMNodeID: 43},
	}
	opts := DefaultSnapshotOptions()
	opts.MaskNodes = map[int]bool{42: true}
	snap := FormatSnapshot(nodes, opts)
	if strings.Contains(snap.Snapshot, testSecretValue) {
		t.Fatalf("snapshot leaks the value:\n%s", snap.Snapshot)
	}
	if !strings.Contains(snap.Snapshot, maskedValue) || !strings.Contains(snap.Snapshot, "visible text") {
		t.Fatalf("expected the masked node hidden and the other visible:\n%s", snap.Snapshot)
	}
	// The mask follows the node, not the ref: unmasked without MaskNodes.
	if plain := FormatSnapshot(nodes, DefaultSnapshotOptions()); !strings.Contains(plain.Snapshot, testSecretValue) {
		t.Fatal("control snapshot should show the value")
	}
}

func TestSecretSessionLifecycle(t *testing.T) {
	m := New()
	m.enterSecretSession("tab1", "o", "https://a.example.com/login", false, 7)
	if s := m.syncSecretURL("tab1", "https://a.example.com/login?x=1"); s == nil {
		t.Fatal("same origin must keep the session")
	}
	if got := m.secretTabs["tab1"].maskedIDs(); !got[7] {
		t.Fatalf("masked ids = %v", got)
	}
	if s := m.syncSecretURL("tab1", "https://evil.example.net/"); s != nil {
		t.Fatal("leaving the origin must end the session")
	}
	if _, ok := m.secretTabs["tab1"]; ok {
		t.Fatal("session not removed")
	}

	// A submitted form ends the session on the next navigation.
	m.enterSecretSession("tab2", "o", "https://a.example.com/login", true, 0)
	if m.syncSecretURL("tab2", "https://a.example.com/login") == nil {
		t.Fatal("no navigation yet, session must stay")
	}
	if m.syncSecretURL("tab2", "https://a.example.com/home") != nil {
		t.Fatal("navigation after submit must end the session")
	}

	// Closing the tab drops it.
	m.enterSecretSession("tab3", "o", "https://a.example.com/", false, 0)
	m.mu.Lock()
	m.closeManagedPageLocked("tab3")
	m.mu.Unlock()
	if _, ok := m.secretTabs["tab3"]; ok {
		t.Fatal("closing a tab must drop its session")
	}
}

func secretCtx(session string) context.Context {
	rt := secretvault.NewRuntime(nil, [16]byte{}, "u")
	rt.SessionKey = session
	return secretvault.WithRuntime(context.Background(), rt)
}

func TestGuardSecretTab_OwnerAndBlockedOps(t *testing.T) {
	m := New()
	m.enterSecretSession("tab1", secretOwnerKey(BrowserScope{}, "s1"), "https://a.example.com/", false, 0)
	ctx := secretCtx("s1")

	for _, op := range []string{"evaluate", "wait_fn", "console", "screenshot"} {
		if err := m.guardSecretTab(ctx, "tab1", op, "s1"); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("%s must be blocked, got %v", op, err)
		}
	}
	for _, op := range []string{"snapshot", "click", "type", "navigate"} {
		if err := m.guardSecretTab(ctx, "tab1", op, "s1"); err != nil {
			t.Errorf("%s must stay allowed, got %v", op, err)
		}
	}
	// Another run session cannot touch the tab at all.
	if err := m.guardSecretTab(ctx, "tab1", "snapshot", "s2"); err == nil || !strings.Contains(err.Error(), "tab not found") {
		t.Errorf("foreign session must be denied, got %v", err)
	}
	// Tabs without a secret are unaffected.
	if err := m.guardSecretTab(ctx, "other", "evaluate", "s1"); err != nil {
		t.Errorf("clean tab must not be guarded: %v", err)
	}
}

func TestExecuteBlocksPageReadsAndBindsTabs(t *testing.T) {
	m := New()
	m.enterSecretSession("tab1", secretOwnerKey(BrowserScope{}, "s1"), "https://a.example.com/", false, 0)
	tool := NewBrowserTool(m)

	evaluate := map[string]any{"action": "act", "targetId": "tab1", "request": map[string]any{"kind": "evaluate", "fn": "document.cookie"}}
	waitFn := map[string]any{"action": "act", "targetId": "tab1", "request": map[string]any{"kind": "wait", "fn": "1"}}
	for name, args := range map[string]map[string]any{
		"evaluate":   evaluate,
		"wait fn":    waitFn,
		"console":    {"action": "console", "targetId": "tab1"},
		"screenshot": {"action": "screenshot", "targetId": "tab1"},
	} {
		res := tool.Execute(secretCtx("s1"), args)
		if !res.IsError || !strings.Contains(res.ForLLM, "disabled") {
			t.Errorf("%s: want blocked, got %+v", name, res)
		}
	}
	res := tool.Execute(secretCtx("s2"), map[string]any{"action": "snapshot", "targetId": "tab1"})
	if !res.IsError || !strings.Contains(res.ForLLM, "tab not found") {
		t.Errorf("foreign session snapshot: %+v", res)
	}
}

func TestExecuteRedactsEveryResult(t *testing.T) {
	rt := secretvault.NewRuntime(nil, [16]byte{}, "u")
	rt.Redactor.AddValue(testSecretValue)
	ctx := secretvault.WithRuntime(context.Background(), rt)

	// The unknown-action error echoes its input, a stand-in for any error text
	// that carries page or driver output.
	res := NewBrowserTool(New()).Execute(ctx, map[string]any{"action": "x-" + testSecretValue})
	if !res.IsError {
		t.Fatal("expected an error")
	}
	if strings.Contains(res.ForLLM, testSecretValue) {
		t.Fatalf("result leaks the value: %q", res.ForLLM)
	}
}

func TestFillSecretRejectsBadInputBeforeTouchingThePage(t *testing.T) {
	m := New()
	if err := m.FillSecret(context.Background(), "tab", "e1", FillSecretOpts{Secret: "x"}); err == nil {
		t.Error("no runtime must be an error")
	}
	ctx := secretCtx("s1")
	if err := m.FillSecret(ctx, "tab", "e1", FillSecretOpts{Secret: "x", Field: "token"}); err == nil {
		t.Error("invalid field accepted")
	}
	if err := m.FillSecret(ctx, "tab", "e1", FillSecretOpts{}); err == nil {
		t.Error("missing secret name accepted")
	}
	res := NewBrowserTool(m).handleAct(ctx, map[string]any{"request": map[string]any{"kind": "fill_secret", "ref": "e1"}})
	if !res.IsError {
		t.Error("fill_secret without a secret name accepted")
	}
}
