//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

// These tests require a running Lightpanda CDP sidecar. Set
// LIGHTPANDA_CDP_URL=ws://localhost:9222 (or wherever it lives) before running:
//
//   docker run -d --rm -p 9222:9222 lightpanda/browser:latest \
//     serve --host 0.0.0.0 --port 9222
//   LIGHTPANDA_CDP_URL=ws://localhost:9222 \
//     go test -tags integration -run Lightpanda ./tests/integration/

func newLightpandaManager(t *testing.T) *browser.Manager {
	t.Helper()
	url := mustEnv(t, "LIGHTPANDA_CDP_URL")

	m := browser.New(
		browser.WithRemoteURL(url),
		browser.WithBackend(browser.BackendLightpanda),
		browser.WithActionTimeout(15*time.Second),
		browser.WithIdleTimeout(0), // disable reaper for predictable tests
		browser.WithMaxPages(10),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("manager start: %v", err)
	}
	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})
	return m
}

func mustEnv(t *testing.T, key string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		t.Skipf("%s not set; skipping lightpanda integration test", key)
	}
	return v
}

func TestLightpanda_SingleTenant_Golden(t *testing.T) {
	m := newLightpandaManager(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	if tab.TargetID == "" {
		t.Fatal("expected non-empty TargetID")
	}
	if tab.URL != "https://example.com" {
		t.Errorf("expected URL https://example.com, got %q", tab.URL)
	}
	if tab.Title == "" {
		t.Errorf("expected non-empty Title (page.Info() should populate from initial post-open call)")
	}

	tabs, err := m.ListTabs(ctx)
	if err != nil {
		t.Fatalf("list tabs: %v", err)
	}
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].Title != tab.Title {
		t.Errorf("ListTabs should return cached title %q, got %q", tab.Title, tabs[0].Title)
	}

	if err := m.CloseTab(ctx, tab.TargetID); err != nil {
		t.Fatalf("close tab: %v", err)
	}
	tabs, _ = m.ListTabs(ctx)
	if len(tabs) != 0 {
		t.Fatalf("expected 0 tabs after close, got %d", len(tabs))
	}
}

// TestLightpanda_Snapshot_AndEval covers the agent's primary "see the page"
// workflow: AX-tree snapshot for structure + Eval (function form) for JS
// access. AX-tree decoding required Lightpanda upstream fix
// lightpanda-io/browser#2232.
func TestLightpanda_Snapshot_AndEval(t *testing.T) {
	m := newLightpandaManager(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}

	snap, err := m.Snapshot(ctx, tab.TargetID, browser.DefaultSnapshotOptions())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Snapshot == "" {
		t.Error("expected non-empty AX snapshot text")
	}
	if snap.Stats.Refs == 0 {
		t.Error("expected at least one ref in snapshot")
	}

	v, err := m.Evaluate(ctx, tab.TargetID, "() => document.title")
	if err != nil {
		t.Errorf("Evaluate(function form): %v", err)
	}
	if v == "" {
		t.Errorf("Evaluate(() => document.title) returned empty value")
	}
}

func TestLightpanda_MultiTenant_Isolation(t *testing.T) {
	m := newLightpandaManager(t)

	tenantA := "11111111-1111-1111-1111-111111111111"
	tenantB := "22222222-2222-2222-2222-222222222222"
	base := context.Background()
	ctxA := browser.WithTenantID(base, tenantA)
	ctxB := browser.WithTenantID(base, tenantB)

	tabA, err := m.OpenTab(ctxA, "https://example.com")
	if err != nil {
		t.Fatalf("tenant A open: %v", err)
	}
	tabB, err := m.OpenTab(ctxB, "https://example.com")
	if err != nil {
		t.Fatalf("tenant B open: %v", err)
	}

	listA, _ := m.ListTabs(ctxA)
	if len(listA) != 1 || listA[0].TargetID != tabA.TargetID {
		t.Errorf("tenant A should see only its own tab; got %v", listA)
	}
	listB, _ := m.ListTabs(ctxB)
	if len(listB) != 1 || listB[0].TargetID != tabB.TargetID {
		t.Errorf("tenant B should see only its own tab; got %v", listB)
	}

	// Tenant A must not be able to close tenant B's tab.
	if err := m.CloseTab(ctxA, tabB.TargetID); err == nil {
		t.Error("expected error when tenant A closes tenant B's tab; got nil")
	}
}

func TestLightpanda_Backend_ReportedCorrectly(t *testing.T) {
	m := newLightpandaManager(t)
	if got := m.Backend(); got != browser.BackendLightpanda {
		t.Errorf("expected backend %q, got %q", browser.BackendLightpanda, got)
	}
}

func TestLightpanda_Screenshot_BlockedByToolGuard(t *testing.T) {
	m := newLightpandaManager(t)
	tool := browser.NewBrowserTool(m)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}

	res := tool.Execute(ctx, map[string]any{
		"action":   "screenshot",
		"targetId": tab.TargetID,
	})
	if res == nil || !res.IsError {
		t.Fatal("expected screenshot to return an error result on lightpanda")
	}
	msg := strings.ToLower(res.ForLLM)
	if !strings.Contains(msg, "lightpanda") || !strings.Contains(msg, "snapshot") {
		t.Errorf("expected error to mention lightpanda + snapshot; got %q", res.ForLLM)
	}
}
