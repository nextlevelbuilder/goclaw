package browser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestTool creates a BrowserTool with a mock engine for testing.
func newTestTool(sm *StorageManager) *BrowserTool {
	eng := newMockEngine()
	m := newTestManager(eng)
	return NewBrowserTool(m, sm, nil, nil, nil)
}

// openTestTab opens a tab in the test tool and returns the targetID.
func openTestTab(t *testing.T, tool *BrowserTool, url string) string {
	t.Helper()
	tab, err := tool.manager.OpenTab(context.Background(), url)
	if err != nil {
		t.Fatalf("OpenTab error: %v", err)
	}
	return tab.TargetID
}

// --- Attach ---

func TestHandleAttach_MissingCdpUrl(t *testing.T) {
	tool := newTestTool(nil)
	result := tool.handleAttach(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing cdpUrl")
	}
}

// --- Cookies ---

func TestHandleGetCookies(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	// Set a cookie via mock
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	mp.cookies = append(mp.cookies, &Cookie{Name: "session", Value: "abc123"})
	mp.mu.Unlock()

	result := tool.handleGetCookies(context.Background(), map[string]any{"targetId": tid})
	if result.IsError {
		t.Fatalf("getCookies error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "session") || !strings.Contains(result.ForLLM, "abc123") {
		t.Errorf("expected cookie data in result: %s", result.ForLLM)
	}
}

func TestHandleSetCookie(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	result := tool.handleSetCookie(context.Background(), map[string]any{
		"targetId": tid,
		"cookie": map[string]any{
			"name":   "token",
			"value":  "xyz",
			"domain": ".example.com",
		},
	})
	if result.IsError {
		t.Fatalf("setCookie error: %s", result.ForLLM)
	}

	// Verify cookie was set via mock
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if len(mp.cookies) != 1 || mp.cookies[0].Name != "token" {
		t.Errorf("expected cookie 'token', got %+v", mp.cookies)
	}
}

func TestHandleSetCookie_MissingName(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	result := tool.handleSetCookie(context.Background(), map[string]any{
		"targetId": tid,
		"cookie":   map[string]any{"value": "no-name"},
	})
	if !result.IsError {
		t.Error("expected error for missing cookie name")
	}
}

func TestHandleSetCookie_MissingObject(t *testing.T) {
	tool := newTestTool(nil)
	result := tool.handleSetCookie(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing cookie object")
	}
}

func TestHandleClearCookies(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	// Add a cookie first
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	mp.cookies = append(mp.cookies, &Cookie{Name: "x", Value: "y"})
	mp.mu.Unlock()

	result := tool.handleClearCookies(context.Background(), map[string]any{"targetId": tid})
	if result.IsError {
		t.Fatalf("clearCookies error: %s", result.ForLLM)
	}

	// Verify cleared
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.cookies != nil {
		t.Errorf("expected nil cookies, got %+v", mp.cookies)
	}
}

// --- Storage ---

func TestHandleGetStorage_Local(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	// Pre-populate local storage via mock
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	mp.storage["local"]["theme"] = "dark"
	mp.mu.Unlock()

	result := tool.handleGetStorage(context.Background(), map[string]any{
		"targetId":    tid,
		"storageKind": "local",
	})
	if result.IsError {
		t.Fatalf("getStorage error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "theme") || !strings.Contains(result.ForLLM, "dark") {
		t.Errorf("expected storage data: %s", result.ForLLM)
	}
}

func TestHandleGetStorage_Session(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	mp.storage["session"]["cart"] = "item1"
	mp.mu.Unlock()

	result := tool.handleGetStorage(context.Background(), map[string]any{
		"targetId":    tid,
		"storageKind": "session",
	})
	if result.IsError {
		t.Fatalf("getStorage error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "cart") {
		t.Errorf("expected session storage data: %s", result.ForLLM)
	}
}

func TestHandleSetStorage(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	result := tool.handleSetStorage(context.Background(), map[string]any{
		"targetId":     tid,
		"storageKind":  "local",
		"storageKey":   "lang",
		"storageValue": "vi",
	})
	if result.IsError {
		t.Fatalf("setStorage error: %s", result.ForLLM)
	}

	// Verify via mock
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.storage["local"]["lang"] != "vi" {
		t.Errorf("expected lang=vi, got %q", mp.storage["local"]["lang"])
	}
}

func TestHandleSetStorage_MissingKey(t *testing.T) {
	tool := newTestTool(nil)
	result := tool.handleSetStorage(context.Background(), map[string]any{
		"storageValue": "val",
	})
	if !result.IsError {
		t.Error("expected error for missing storageKey")
	}
}

func TestHandleClearStorage(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	// Pre-populate
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	mp.storage["local"]["a"] = "1"
	mp.storage["local"]["b"] = "2"
	mp.mu.Unlock()

	result := tool.handleClearStorage(context.Background(), map[string]any{
		"targetId":    tid,
		"storageKind": "local",
	})
	if result.IsError {
		t.Fatalf("clearStorage error: %s", result.ForLLM)
	}

	// Verify cleared
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if len(mp.storage["local"]) != 0 {
		t.Errorf("expected empty local storage, got %v", mp.storage["local"])
	}
}

// --- Profiles ---

func TestHandleProfiles_NoStorage(t *testing.T) {
	tool := newTestTool(nil) // nil storage
	result := tool.handleProfiles(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error when storage is nil")
	}
}

func TestHandleProfiles_Empty(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)
	tool := newTestTool(sm)

	result := tool.handleProfiles(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("profiles error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "[]") {
		t.Errorf("expected empty array, got: %s", result.ForLLM)
	}
}

func TestHandleProfiles_WithData(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)
	tool := newTestTool(sm)

	// Create a profile
	profileDir := filepath.Join(dir, "browser", "profiles", "default", "test-profile")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := tool.handleProfiles(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("profiles error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "test-profile") {
		t.Errorf("expected profile in result: %s", result.ForLLM)
	}
}

func TestHandleDeleteProfile_NoStorage(t *testing.T) {
	tool := newTestTool(nil)
	result := tool.handleDeleteProfile(context.Background(), map[string]any{"profile": "x"})
	if !result.IsError {
		t.Error("expected error when storage is nil")
	}
}

func TestHandleDeleteProfile_MissingName(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)
	tool := newTestTool(sm)

	result := tool.handleDeleteProfile(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing profile name")
	}
}

func TestHandleDeleteProfile_Success(t *testing.T) {
	dir := t.TempDir()
	sm := NewStorageManager(dir, nil)
	tool := newTestTool(sm)

	profileDir := filepath.Join(dir, "browser", "profiles", "default", "del-me")
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := tool.handleDeleteProfile(context.Background(), map[string]any{"profile": "del-me"})
	if result.IsError {
		t.Fatalf("deleteProfile error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "del-me") {
		t.Errorf("expected profile name in result: %s", result.ForLLM)
	}

	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Error("profile should be deleted from disk")
	}
}

// --- FocusTab ---

func TestHandleFocusTab_Success(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	result := tool.handleFocusTab(context.Background(), map[string]any{"targetId": tid})
	if result.IsError {
		t.Fatalf("focusTab error: %s", result.ForLLM)
	}

	// Verify mock page was activated
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if !mp.activated {
		t.Error("page should be activated after focusTab")
	}
}

func TestHandleFocusTab_MissingTargetId(t *testing.T) {
	tool := newTestTool(nil)
	result := tool.handleFocusTab(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing targetId")
	}
}

// --- Errors ---

func TestHandleErrors_Empty(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	result := tool.handleErrors(context.Background(), map[string]any{"targetId": tid})
	if result.IsError {
		t.Fatalf("errors error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "[]") {
		t.Errorf("expected empty array: %s", result.ForLLM)
	}
}

func TestHandleErrors_WithErrors(t *testing.T) {
	tool := newTestTool(nil)
	tid := openTestTab(t, tool, "https://example.com")

	// Inject JS errors via mock
	tool.manager.mu.Lock()
	page := tool.manager.pages[tid]
	tool.manager.mu.Unlock()
	mp := page.(*mockPage)
	mp.mu.Lock()
	mp.jsErrors = append(mp.jsErrors, &JSError{
		Text:   "TypeError: undefined is not a function",
		URL:    "https://example.com/app.js",
		Line:   42,
		Column: 10,
	})
	mp.mu.Unlock()

	result := tool.handleErrors(context.Background(), map[string]any{"targetId": tid})
	if result.IsError {
		t.Fatalf("errors error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "TypeError") {
		t.Errorf("expected error text in result: %s", result.ForLLM)
	}

	// Second call should return empty (errors cleared)
	result2 := tool.handleErrors(context.Background(), map[string]any{"targetId": tid})
	if result2.IsError {
		t.Fatalf("errors error: %s", result2.ForLLM)
	}
	if !strings.Contains(result2.ForLLM, "[]") {
		t.Errorf("expected empty after clear: %s", result2.ForLLM)
	}
}
