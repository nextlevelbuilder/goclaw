package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- Interface compliance ---

func TestChromeEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*ChromeEngine)(nil)
}

func TestChromePage_ImplementsPage(t *testing.T) {
	var _ Page = (*ChromePage)(nil)
}

func TestChromeElement_ImplementsElement(t *testing.T) {
	var _ Element = (*ChromeElement)(nil)
}

func TestMockEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*mockEngine)(nil)
}

func TestMockPage_ImplementsPage(t *testing.T) {
	var _ Page = (*mockPage)(nil)
}

func TestMockElement_ImplementsElement(t *testing.T) {
	var _ Element = (*mockElement)(nil)
}

// --- Engine lifecycle via mock ---

func TestEngine_LaunchAndConnect(t *testing.T) {
	eng := newMockEngine()
	eng.connected = false

	err := eng.Launch(LaunchOpts{Headless: true, ProfileDir: "/tmp/profile"})
	if err != nil {
		t.Fatalf("Launch error: %v", err)
	}
	if !eng.IsConnected() {
		t.Error("should be connected after Launch")
	}
	if eng.launched.ProfileDir != "/tmp/profile" {
		t.Errorf("ProfileDir not passed: %q", eng.launched.ProfileDir)
	}
}

func TestEngine_Close(t *testing.T) {
	eng := newMockEngine()
	if err := eng.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if eng.IsConnected() {
		t.Error("should be disconnected after Close")
	}
}

func TestEngine_Incognito(t *testing.T) {
	eng := newMockEngine()
	inc, err := eng.Incognito()
	if err != nil {
		t.Fatalf("Incognito error: %v", err)
	}
	if !strings.HasPrefix(inc.Name(), "mock-incognito-") {
		t.Errorf("expected 'mock-incognito-*', got %q", inc.Name())
	}
	if !inc.IsConnected() {
		t.Error("incognito engine should be connected")
	}
}

func TestEngine_NewPage(t *testing.T) {
	eng := newMockEngine()
	page, err := eng.NewPage(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("NewPage error: %v", err)
	}
	if page.TargetID() != "mock-tab-1" {
		t.Errorf("expected 'mock-tab-1', got %q", page.TargetID())
	}
	info, _ := page.Info()
	if info.URL != "https://example.com" {
		t.Errorf("expected URL, got %q", info.URL)
	}
}

func TestEngine_Pages(t *testing.T) {
	eng := newMockEngine()
	eng.NewPage(context.Background(), "https://a.com")
	eng.NewPage(context.Background(), "https://b.com")

	pages, err := eng.Pages()
	if err != nil {
		t.Fatalf("Pages error: %v", err)
	}
	if len(pages) != 2 {
		t.Errorf("expected 2 pages, got %d", len(pages))
	}
}

func TestEngine_NotConnected(t *testing.T) {
	eng := newMockEngine()
	eng.connected = false

	_, err := eng.NewPage(context.Background(), "https://x.com")
	if err == nil {
		t.Error("expected error when not connected")
	}

	_, err = eng.Pages()
	if err == nil {
		t.Error("expected error when not connected")
	}
}

// --- Manager with mock engine ---

func TestManager_StartWithMockEngine(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	status := m.Status()
	if !status.Running {
		t.Error("should be running")
	}
	if status.Engine != "mock" {
		t.Errorf("expected engine 'mock', got %q", status.Engine)
	}
}

func TestManager_StopWithMockEngine(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	_ = m.Start(context.Background())
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	status := m.Status()
	if status.Running {
		t.Error("should not be running after Stop")
	}
}

func TestManager_OpenTabWithMock(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	tab, err := m.OpenTab(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("OpenTab error: %v", err)
	}
	if tab.TargetID == "" {
		t.Error("targetID should not be empty")
	}
	if tab.URL != "https://example.com" {
		t.Errorf("expected URL 'https://example.com', got %q", tab.URL)
	}

	tabs, err := m.ListTabs(context.Background())
	if err != nil {
		t.Fatalf("ListTabs error: %v", err)
	}
	if len(tabs) != 1 {
		t.Errorf("expected 1 tab, got %d", len(tabs))
	}
}

func TestManager_CloseTabWithMock(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	tab, _ := m.OpenTab(context.Background(), "https://example.com")
	if err := m.CloseTab(context.Background(), tab.TargetID); err != nil {
		t.Fatalf("CloseTab error: %v", err)
	}

	// Page should be removed from pages map
	m.mu.Lock()
	_, exists := m.pages[tab.TargetID]
	m.mu.Unlock()
	if exists {
		t.Error("page should be removed after CloseTab")
	}
}

func TestManager_FocusTabWithMock(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	tab, _ := m.OpenTab(context.Background(), "https://example.com")
	if err := m.FocusTab(context.Background(), tab.TargetID); err != nil {
		t.Fatalf("FocusTab error: %v", err)
	}

	// Verify mock page was activated
	m.mu.Lock()
	page := m.pages[tab.TargetID]
	m.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if !mp.activated {
		t.Error("page should be activated")
	}
}

func TestManager_ConsoleMessagesWithMock(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	tab, _ := m.OpenTab(context.Background(), "https://example.com")

	// Simulate console message via mock
	m.mu.Lock()
	page := m.pages[tab.TargetID]
	m.mu.Unlock()
	mp := page.(*mockPage)
	mp.emitConsole(ConsoleMessage{Level: "error", Text: "test error"})

	// Wait briefly for goroutine delivery
	time.Sleep(10 * time.Millisecond)

	msgs := m.ConsoleMessages(context.Background(), tab.TargetID)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Level != "error" || msgs[0].Text != "test error" {
		t.Errorf("unexpected message: %+v", msgs[0])
	}

	// Second call should return empty (cleared after first read)
	msgs2 := m.ConsoleMessages(context.Background(), tab.TargetID)
	if len(msgs2) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(msgs2))
	}
}

func TestManager_EvictOldestPage(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)
	m.maxPages = 2

	tab1, _ := m.OpenTab(context.Background(), "https://a.com")
	time.Sleep(5 * time.Millisecond) // ensure different timestamps
	_, _ = m.OpenTab(context.Background(), "https://b.com")
	time.Sleep(5 * time.Millisecond)
	_, _ = m.OpenTab(context.Background(), "https://c.com")

	// tab1 should have been evicted
	m.mu.Lock()
	_, exists := m.pages[tab1.TargetID]
	count := len(m.pages)
	m.mu.Unlock()
	if exists {
		t.Error("oldest tab should be evicted")
	}
	if count != 2 {
		t.Errorf("expected 2 pages, got %d", count)
	}
}

func TestManager_TenantIsolation(t *testing.T) {
	eng := newMockEngine()
	m := newTestManager(eng)

	ctxA := WithTenantID(context.Background(), "tenant-a")
	ctxB := WithTenantID(context.Background(), "tenant-b")

	tabA, _ := m.OpenTab(ctxA, "https://a.com")
	_, _ = m.OpenTab(ctxB, "https://b.com")

	// Tenant B should not see tenant A's page
	m.mu.Lock()
	_, err := m.getPageForTenant(tabA.TargetID, "tenant-b")
	m.mu.Unlock()
	if err == nil {
		t.Error("tenant-b should not access tenant-a's page")
	}
}

// --- pollCondition ---

func TestPollCondition_Success(t *testing.T) {
	count := 0
	err := pollCondition(context.Background(), time.Second, 10*time.Millisecond, func() (bool, error) {
		count++
		return count >= 3, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count < 3 {
		t.Errorf("expected at least 3 polls, got %d", count)
	}
}

func TestPollCondition_Timeout(t *testing.T) {
	err := pollCondition(context.Background(), 50*time.Millisecond, 10*time.Millisecond, func() (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPollCondition_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := pollCondition(ctx, time.Second, 10*time.Millisecond, func() (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}
