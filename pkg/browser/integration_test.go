//go:build integration

package browser

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Integration tests require a real Chrome browser.
// Run with: go test -v -tags integration -timeout 60s ./pkg/browser/...
//
// Set BROWSER_REMOTE_URL=ws://host:port to test against remote Chrome.

func newIntegrationManager(t *testing.T) *Manager {
	t.Helper()
	// Use os.MkdirTemp instead of t.TempDir() because Chrome profile dirs
	// may have lock files that prevent automatic cleanup.
	workspace, err := os.MkdirTemp("", "goclaw-integration-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup after Chrome is stopped
		os.RemoveAll(workspace)
	})

	opts := []Option{
		WithHeadless(true),
		WithWorkspace(workspace),
		WithIdleTimeout(0), // disable reaper for tests
	}

	if remote := os.Getenv("BROWSER_REMOTE_URL"); remote != "" {
		opts = append(opts, WithRemoteURL(remote))
	}
	if bin := os.Getenv("BROWSER_BINARY"); bin != "" {
		opts = append(opts, WithBinaryPath(bin))
	}

	return New(opts...)
}

func TestIntegration_StartStop(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	status := m.Status()
	if !status.Running {
		t.Fatal("expected Running=true")
	}
	if status.Engine != "chrome" {
		t.Errorf("expected engine 'chrome', got %q", status.Engine)
	}

	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	status = m.Status()
	if status.Running {
		t.Fatal("expected Running=false after Stop")
	}
}

func TestIntegration_OpenTab_Navigate_Snapshot(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	if tab.TargetID == "" {
		t.Fatal("expected non-empty targetID")
	}
	t.Logf("Tab opened: targetID=%s url=%s", tab.TargetID, tab.URL)

	// Wait for page to load
	time.Sleep(500 * time.Millisecond)

	// Snapshot
	snap, err := m.Snapshot(ctx, tab.TargetID, DefaultSnapshotOptions())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Snapshot == "" {
		t.Fatal("expected non-empty snapshot")
	}
	if !strings.Contains(strings.ToLower(snap.Title), "example") {
		t.Logf("Title: %q (may not contain 'example')", snap.Title)
	}
	t.Logf("Snapshot: %d chars, %d refs, %d interactive", len(snap.Snapshot), snap.Stats.Refs, snap.Stats.Interactive)

	// Navigate
	if err := m.Navigate(ctx, tab.TargetID, "https://example.org"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Screenshot
	data, err := m.Screenshot(ctx, tab.TargetID, false)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if len(data) < 100 {
		t.Errorf("expected screenshot data, got %d bytes", len(data))
	}
	t.Logf("Screenshot: %d bytes", len(data))
}

func TestIntegration_Tabs(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	tab1, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab 1: %v", err)
	}

	tab2, err := m.OpenTab(ctx, "https://example.org")
	if err != nil {
		t.Fatalf("OpenTab 2: %v", err)
	}

	tabs, err := m.ListTabs(ctx)
	if err != nil {
		t.Fatalf("ListTabs: %v", err)
	}
	if len(tabs) < 2 {
		t.Errorf("expected at least 2 tabs, got %d", len(tabs))
	}
	t.Logf("Tabs: %d", len(tabs))

	// Focus tab
	if err := m.FocusTab(ctx, tab1.TargetID); err != nil {
		t.Fatalf("FocusTab: %v", err)
	}

	// Close tab
	if err := m.CloseTab(ctx, tab2.TargetID); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}

	tabs, _ = m.ListTabs(ctx)
	t.Logf("Tabs after close: %d", len(tabs))
}

func TestIntegration_Cookies(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Set cookie
	if err := m.SetCookie(ctx, tab.TargetID, &Cookie{
		Name:   "test_cookie",
		Value:  "hello123",
		Domain: ".example.com",
		Path:   "/",
	}); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}

	// Get cookies
	cookies, err := m.GetCookies(ctx, tab.TargetID)
	if err != nil {
		t.Fatalf("GetCookies: %v", err)
	}

	found := false
	for _, c := range cookies {
		if c.Name == "test_cookie" && c.Value == "hello123" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("test_cookie not found in %d cookies", len(cookies))
	}
	t.Logf("Cookies: %d", len(cookies))

	// Clear cookies
	if err := m.ClearCookies(ctx, tab.TargetID); err != nil {
		t.Fatalf("ClearCookies: %v", err)
	}

	cookies, _ = m.GetCookies(ctx, tab.TargetID)
	for _, c := range cookies {
		if c.Name == "test_cookie" {
			t.Error("test_cookie should be cleared")
		}
	}

}

func TestIntegration_Storage(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Set localStorage
	if err := m.SetStorage(ctx, tab.TargetID, true, "lang", "vi"); err != nil {
		t.Fatalf("SetStorage: %v", err)
	}

	// Get localStorage
	items, err := m.GetStorage(ctx, tab.TargetID, true)
	if err != nil {
		t.Fatalf("GetStorage: %v", err)
	}
	if items["lang"] != "vi" {
		t.Errorf("expected lang=vi, got %q", items["lang"])
	}
	t.Logf("Storage items: %v", items)

	// Clear localStorage
	if err := m.ClearStorage(ctx, tab.TargetID, true); err != nil {
		t.Fatalf("ClearStorage: %v", err)
	}

	items, _ = m.GetStorage(ctx, tab.TargetID, true)
	if items["lang"] != "" {
		t.Errorf("expected empty after clear, got %q", items["lang"])
	}
}

func TestIntegration_JSErrors(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Inject a JS error
	_, _ = m.Evaluate(ctx, tab.TargetID, `() => setTimeout(function(){ throw new Error("test error from integration"); }, 0)`)
	time.Sleep(200 * time.Millisecond)

	errors, err := m.GetJSErrors(ctx, tab.TargetID)
	if err != nil {
		t.Fatalf("GetJSErrors: %v", err)
	}
	t.Logf("JS Errors: %d", len(errors))
	for _, e := range errors {
		t.Logf("  - %s (line %d)", e.Text, e.Line)
	}
}

func TestIntegration_Evaluate(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	result, err := m.Evaluate(ctx, tab.TargetID, `() => document.title`)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty title")
	}
	t.Logf("document.title = %q", result)

	result, err = m.Evaluate(ctx, tab.TargetID, `() => window.location.href`)
	if err != nil {
		t.Fatalf("Evaluate href: %v", err)
	}
	t.Logf("window.location.href = %q", result)
}

func TestIntegration_Profiles(t *testing.T) {
	m := newIntegrationManager(t)
	ctx := context.Background()
	sm := NewStorageManager(m.workspace, nil)

	// List profiles (should be empty)
	profiles, err := sm.ListProfiles("default")
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	t.Logf("Initial profiles: %d", len(profiles))

	// Open with profile to create profile dir
	m.mu.Lock()
	m.activeProfile = "test-profile"
	m.mu.Unlock()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start with profile: %v", err)
	}

	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	t.Logf("Tab with profile: %s", tab.TargetID)

	m.Stop(ctx)

	// Check profile was created
	profiles, err = sm.ListProfiles("default")
	if err != nil {
		t.Fatalf("ListProfiles after: %v", err)
	}
	t.Logf("Profiles after browse: %d", len(profiles))
	for _, p := range profiles {
		t.Logf("  - %s (%s)", p.Name, p.Size)
	}

	// Get usage
	usage, err := sm.GetUsage("default")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	t.Logf("Usage: %d bytes", usage)
}

func TestIntegration_BrowserTool(t *testing.T) {
	m := newIntegrationManager(t)
	sm := NewStorageManager(m.workspace, nil)
	tool := NewBrowserTool(m, sm, nil, nil, nil)
	ctx := context.Background()

	// status
	r := tool.Execute(ctx, map[string]any{"action": "status"})
	if r.IsError {
		t.Fatalf("status: %s", r.ForLLM)
	}
	t.Logf("status: %s", r.ForLLM)

	// start
	r = tool.Execute(ctx, map[string]any{"action": "start"})
	if r.IsError {
		t.Fatalf("start: %s", r.ForLLM)
	}

	// open
	r = tool.Execute(ctx, map[string]any{"action": "open", "targetUrl": "https://example.com"})
	if r.IsError {
		t.Fatalf("open: %s", r.ForLLM)
	}
	t.Logf("open: %s", r.ForLLM)

	// snapshot
	r = tool.Execute(ctx, map[string]any{"action": "snapshot"})
	if r.IsError {
		t.Fatalf("snapshot: %s", r.ForLLM)
	}
	t.Logf("snapshot: %d chars", len(r.ForLLM))

	// getCookies
	r = tool.Execute(ctx, map[string]any{"action": "getCookies"})
	if r.IsError {
		t.Fatalf("getCookies: %s", r.ForLLM)
	}
	t.Logf("cookies: %s", r.ForLLM)

	// profiles
	r = tool.Execute(ctx, map[string]any{"action": "profiles"})
	if r.IsError {
		t.Fatalf("profiles: %s", r.ForLLM)
	}
	t.Logf("profiles: %s", r.ForLLM)

	// errors
	r = tool.Execute(ctx, map[string]any{"action": "errors"})
	if r.IsError {
		t.Fatalf("errors: %s", r.ForLLM)
	}

	// stop
	r = tool.Execute(ctx, map[string]any{"action": "stop"})
	if r.IsError {
		t.Fatalf("stop: %s", r.ForLLM)
	}
}

// TestIntegration_CustomBinary verifies that a custom browser binary (Brave, Edge, etc.) works.
// Run with: BROWSER_BINARY="/Applications/Brave Browser.app/Contents/MacOS/Brave Browser" go test -v -tags integration -run TestIntegration_CustomBinary ./pkg/browser/...
// If BROWSER_BINARY is not set, it uses the default Chrome as a smoke test for the binary path mechanism.
func TestIntegration_CustomBinary(t *testing.T) {
	workspace, err := os.MkdirTemp("", "goclaw-custombinary-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(workspace) })

	bin := os.Getenv("BROWSER_BINARY")
	if bin == "" {
		// Fallback: use Chrome's own binary to test the mechanism
		bin = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(bin); err != nil {
			t.Skip("no BROWSER_BINARY set and Chrome not found at default path")
		}
	}

	t.Logf("Using browser binary: %s", bin)

	m := New(
		WithHeadless(true),
		WithWorkspace(workspace),
		WithBinaryPath(bin),
		WithIdleTimeout(0),
	)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop(ctx)

	status := m.Status()
	if !status.Running {
		t.Fatal("expected Running=true")
	}
	t.Logf("Status: engine=%s running=%v", status.Engine, status.Running)

	// Open tab
	tab, err := m.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab: %v", err)
	}
	t.Logf("Tab: targetID=%s url=%s", tab.TargetID, tab.URL)

	time.Sleep(500 * time.Millisecond)

	// Snapshot
	snap, err := m.Snapshot(ctx, tab.TargetID, DefaultSnapshotOptions())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	t.Logf("Snapshot: %d chars, title=%q", len(snap.Snapshot), snap.Title)

	// Evaluate JS
	result, err := m.Evaluate(ctx, tab.TargetID, `() => navigator.userAgent`)
	if err != nil {
		t.Fatalf("Evaluate userAgent: %v", err)
	}
	t.Logf("User-Agent: %s", result)

	// Check if it's actually the binary we expected
	if strings.Contains(bin, "Brave") && !strings.Contains(result, "Brave") {
		t.Logf("WARNING: User-Agent doesn't contain 'Brave': %s", result)
	}

	// Cookies work on any Chromium-based browser
	if err := m.SetCookie(ctx, tab.TargetID, &Cookie{
		Name: "test", Value: "custom-binary", Domain: ".example.com", Path: "/",
	}); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	cookies, err := m.GetCookies(ctx, tab.TargetID)
	if err != nil {
		t.Fatalf("GetCookies: %v", err)
	}
	found := false
	for _, c := range cookies {
		if c.Name == "test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cookie not found after SetCookie")
	}
	t.Logf("Cookies: %d (custom binary CDP works!)", len(cookies))
}
