package browser

import (
	"context"
	"fmt"
)

// --- Cookies ---

// GetCookies returns all cookies for the page identified by targetID.
func (m *Manager) GetCookies(ctx context.Context, targetID string) ([]*Cookie, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return page.GetCookies()
}

// SetCookie sets a cookie on the page identified by targetID.
func (m *Manager) SetCookie(ctx context.Context, targetID string, c *Cookie) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.SetCookie(c)
}

// ClearCookies clears all cookies for the page identified by targetID.
func (m *Manager) ClearCookies(ctx context.Context, targetID string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.ClearCookies()
}

// --- Storage ---

// GetStorage returns localStorage or sessionStorage items for a page.
func (m *Manager) GetStorage(ctx context.Context, targetID string, isLocal bool) (map[string]string, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return page.GetStorageItems(isLocal)
}

// SetStorage sets an item in localStorage or sessionStorage.
func (m *Manager) SetStorage(ctx context.Context, targetID string, isLocal bool, key, value string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.SetStorageItem(isLocal, key, value)
}

// ClearStorage clears localStorage or sessionStorage for a page.
func (m *Manager) ClearStorage(ctx context.Context, targetID string, isLocal bool) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.ClearStorage(isLocal)
}

// --- JS Errors ---

// GetJSErrors returns captured JavaScript exceptions for a page.
func (m *Manager) GetJSErrors(ctx context.Context, targetID string) ([]*JSError, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return page.GetJSErrors()
}

// --- Emulation ---

// Emulate sets device/viewport emulation on a page.
func (m *Manager) Emulate(ctx context.Context, targetID string, opts EmulateOpts) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.Emulate(opts)
}

// SetExtraHeaders sets additional HTTP headers for all requests on a page.
func (m *Manager) SetExtraHeaders(ctx context.Context, targetID string, headers map[string]string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.SetExtraHeaders(headers)
}

// SetOffline enables or disables offline mode for a page.
func (m *Manager) SetOffline(ctx context.Context, targetID string, offline bool) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.SetOffline(offline)
}

// PDF generates a PDF from the page.
func (m *Manager) PDF(ctx context.Context, targetID string, landscape bool) ([]byte, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return page.PDF(landscape)
}

// --- Raw CDP Input ---

// DispatchMouseEvent sends a native CDP mouse event to a page.
func (m *Manager) DispatchMouseEvent(ctx context.Context, targetID, typ string, x, y float64, button string, clickCount int) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.DispatchMouseEvent(typ, x, y, button, clickCount)
}

// DispatchKeyEvent sends a native CDP keyboard event to a page.
// vkCode is the Windows virtual key code (e.g. 13 for Enter, 8 for Backspace).
func (m *Manager) DispatchKeyEvent(ctx context.Context, targetID, typ string, key, code, text string, modifiers int, vkCode int) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return page.DispatchKeyEvent(typ, key, code, text, modifiers, vkCode)
}

// --- Attach ---

// StartWithAttach connects to an existing browser at the given CDP URL without managing its lifecycle.
func (m *Manager) StartWithAttach(ctx context.Context, cdpURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close existing engine if running
	if m.engine.IsConnected() {
		m.closeTenantEnginesLocked()
		_ = m.engine.Close()
		m.resetMapsLocked()
	}

	m.engine = NewChromeEngine(m.logger)
	if err := m.engine.Launch(LaunchOpts{AttachURL: cdpURL}); err != nil {
		return fmt.Errorf("attach to browser: %w", err)
	}

	// Start reaper if configured
	if m.idleTimeout > 0 && m.stopReaper == nil {
		m.stopReaper = make(chan struct{})
		go m.runReaper()
	}

	return nil
}
