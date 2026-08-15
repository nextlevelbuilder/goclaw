package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// ListTabs returns open tabs filtered by the caller's tenant context.
func (m *Manager) ListTabs(ctx context.Context) ([]TabInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunningLocked() {
		return nil, fmt.Errorf("browser not running")
	}

	tenantID := tenantIDFromCtx(ctx)

	// Lightpanda: no server-side enumeration — return what we're tracking.
	// Use the cache populated at OpenTab; page.Info() is unreliable upstream
	// after the initial call.
	if m.backend == BackendLightpanda {
		tabs := make([]TabInfo, 0, len(m.pages))
		for tid := range m.pages {
			if !m.pageVisibleToTenantLocked(tid, tenantID) {
				continue
			}
			if cached, ok := m.pageInfos[tid]; ok {
				tabs = append(tabs, cached)
				continue
			}
			tabs = append(tabs, TabInfo{TargetID: tid})
		}
		return tabs, nil
	}

	// Use tenant-scoped browser context for page listing
	b, err := m.tenantBrowserLocked(tenantID)
	if err != nil {
		return nil, err
	}

	pages, err := b.Pages()
	if err != nil {
		if m.remoteURL != "" {
			if reconnErr := m.reconnectLocked(); reconnErr != nil {
				return nil, fmt.Errorf("list pages: %w (reconnect also failed: %v)", err, reconnErr)
			}
			m.logger.Info("auto-reconnected to remote Chrome")
			// Re-acquire tenant browser after reconnect (incognito contexts were reset)
			b, err = m.tenantBrowserLocked(tenantID)
			if err != nil {
				return nil, err
			}
			pages, err = b.Pages()
			if err != nil {
				return nil, fmt.Errorf("list pages after reconnect: %w", err)
			}
		} else {
			return nil, fmt.Errorf("list pages: %w", err)
		}
	}

	tabs := make([]TabInfo, 0, len(pages))
	for _, p := range pages {
		info, err := p.Info()
		if err != nil || info == nil {
			continue
		}
		tid := string(p.TargetID)
		m.pages[tid] = p
		if tenantID != "" {
			m.pageTenants[tid] = tenantID
		}
		tabs = append(tabs, TabInfo{
			TargetID: tid,
			URL:      info.URL,
			Title:    info.Title,
		})
	}
	return tabs, nil
}

// pageVisibleToTenantLocked reports whether a page is accessible to the given tenant.
// Master tenant and empty context see everything; scoped tenants see only their own pages.
// Must be called with mu held.
func (m *Manager) pageVisibleToTenantLocked(targetID, tenantID string) bool {
	if tenantID == "" || tenantID == MasterTenantID {
		return true
	}
	owner, ok := m.pageTenants[targetID]
	if !ok {
		return false // scoped tenant can't see pages with no recorded owner
	}
	return owner == tenantID
}

// OpenTab opens a new tab with the given URL.
// Pages are created within the tenant's incognito browser context for isolation.
// If the tenant already has maxPages open, the oldest idle page is closed first.
//
// Lightpanda: each tab gets its own dedicated CDP connection (lightpanda
// requires 1 conn per tab, and each connection is its own browser, so tenant
// isolation is automatic).
func (m *Manager) OpenTab(ctx context.Context, url string) (*TabInfo, error) {
	scope := scopeFromCtx(ctx)
	cookies, err := m.cookiesForURL(ctx, scope, url)
	if err != nil {
		return nil, fmt.Errorf("load browser cookies: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	tenantID := scope.Key()

	// Enforce max pages per tenant
	if m.maxPages > 0 {
		m.evictOldestIfOverLimitLocked(tenantID)
	}

	if m.backend == BackendLightpanda {
		return m.openTabLightpandaLocked(ctx, tenantID, url)
	}

	b, err := m.tenantBrowserLocked(tenantID)
	if err != nil {
		return nil, err
	}

	initialURL := url
	if len(cookies) > 0 {
		initialURL = "about:blank"
	}
	page, err := b.Page(proto.TargetCreateTarget{URL: initialURL})
	if err != nil {
		return nil, fmt.Errorf("open tab: %w", err)
	}
	if len(cookies) > 0 {
		if err := page.SetCookies(cookies); err != nil {
			_ = page.Close()
			return nil, fmt.Errorf("set browser cookies: %w", err)
		}
		if err := page.Navigate(url); err != nil {
			_ = page.Close()
			return nil, fmt.Errorf("navigate after setting cookies: %w", err)
		}
	}

	// Watchdog: close page on ctx cancel to unblock WaitStable CDP call.
	stopWatchdog := watchPageClose(ctx, page)
	if err := page.WaitStable(300 * time.Millisecond); err != nil {
		stopWatchdog()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("wait stable: %w", err)
	}
	stopWatchdog()
	info, _ := page.Info()
	tid := string(page.TargetID)
	m.pages[tid] = page
	m.touchPageLocked(tid)
	if tenantID != "" {
		m.pageTenants[tid] = tenantID
	}

	// Set up console listener
	m.setupConsoleListener(page, tid)

	tab := &TabInfo{TargetID: tid, URL: url}
	if info != nil {
		tab.URL = info.URL
		tab.Title = info.Title
	}
	return tab, nil
}

// openTabLightpandaLocked mints a fresh CDP connection, creates a browser
// context (required per-conn on lightpanda), creates a target, and attaches
// the resulting rod.Page. Must be called with mu held.
func (m *Manager) openTabLightpandaLocked(ctx context.Context, tenantID, targetURL string) (*TabInfo, error) {
	conn := rod.New().Context(ctx).ControlURL(m.cdpURL)
	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("lightpanda: connect: %w", err)
	}

	// Lightpanda requires Target.createBrowserContext per connection.
	bc, err := proto.TargetCreateBrowserContext{}.Call(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("lightpanda: create browser context: %w", err)
	}

	tgt, err := proto.TargetCreateTarget{
		URL:              targetURL,
		BrowserContextID: bc.BrowserContextID,
	}.Call(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("lightpanda: create target: %w", err)
	}

	page, err := conn.PageFromTarget(tgt.TargetID)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("lightpanda: page from target: %w", err)
	}

	// Best-effort stability wait — lightpanda may not fire every lifecycle event.
	stopWatchdog := watchPageClose(ctx, page)
	_ = page.WaitStable(300 * time.Millisecond)
	stopWatchdog()

	info, _ := page.Info()
	// Lightpanda numbers targets per-browser, and each conn is its own browser,
	// so every conn's first target is "FID-0000000001". Synthesize a globally
	// unique key for our maps; the upstream targetID is only needed inside this
	// function (for createTarget / PageFromTarget).
	m.nextLpTabSeq++
	tid := fmt.Sprintf("lp-%d", m.nextLpTabSeq)
	m.pages[tid] = page
	m.pageConns[tid] = conn
	m.touchPageLocked(tid)
	if tenantID != "" {
		m.pageTenants[tid] = tenantID
	}
	m.setupConsoleListener(page, tid)

	tab := TabInfo{TargetID: tid, URL: targetURL}
	if info != nil {
		tab.URL = info.URL
		tab.Title = info.Title
	}
	// Cache for ListTabs — page.Info() is unreliable on Lightpanda after the
	// initial post-open call.
	m.pageInfos[tid] = tab
	tabCopy := tab
	return &tabCopy, nil
}

// evictOldestIfOverLimitLocked closes the oldest idle page for a tenant if at or over maxPages.
// Must be called with mu held.
func (m *Manager) evictOldestIfOverLimitLocked(tenantID string) {
	isMaster := tenantID == "" || tenantID == MasterTenantID

	// Collect targetIDs belonging to this tenant
	var owned []string
	for tid := range m.pages {
		if isMaster {
			// Master tenant owns pages not in pageTenants
			if _, hasOwner := m.pageTenants[tid]; !hasOwner {
				owned = append(owned, tid)
			}
		} else {
			if m.pageTenants[tid] == tenantID {
				owned = append(owned, tid)
			}
		}
	}

	if len(owned) < m.maxPages {
		return
	}

	// Find the oldest page by lastUsed
	var oldestID string
	var oldestTime time.Time
	for _, tid := range owned {
		lu, ok := m.pageLastUsed[tid]
		if !ok {
			oldestID = tid
			break
		}
		if oldestID == "" || lu.Before(oldestTime) {
			oldestID = tid
			oldestTime = lu
		}
	}

	if oldestID == "" {
		return
	}

	m.closeManagedPageLocked(oldestID)
	m.logger.Info("evicted oldest page (max pages reached)", "targetId", oldestID, "tenant", tenantID)
}

// closeManagedPageLocked closes a page and removes all associated tracking.
// On Lightpanda, closing the dedicated CDP connection tears down the browser
// server-side (no page.Close() needed). Must be called with mu held.
func (m *Manager) closeManagedPageLocked(targetID string) {
	if conn, ok := m.pageConns[targetID]; ok {
		// Lightpanda: rod.Browser.Close() calls Browser.close which Lightpanda
		// rejects with UnknownMethod; the WS drops anyway and Lightpanda
		// auto-cleans the browser. Swallow the error.
		_ = conn.Close()
		delete(m.pageConns, targetID)
	} else if page, ok := m.pages[targetID]; ok {
		_ = page.Close()
	}
	delete(m.pages, targetID)
	delete(m.pageInfos, targetID)
	delete(m.console, targetID)
	delete(m.pageTenants, targetID)
	delete(m.pageLastUsed, targetID)
	m.refs.Remove(targetID)
}

// FocusTab activates a tab.
func (m *Manager) FocusTab(ctx context.Context, targetID string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.getPageForTenant(targetID, tenantID)
	if err != nil {
		return err
	}

	_, err = page.Activate()
	return err
}

// CloseTab closes a tab.
func (m *Manager) CloseTab(ctx context.Context, targetID string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Tenant ownership check — same semantics as getPageForTenant but without
	// the upstream refresh, so it works uniformly for both backends.
	if tenantID != "" && tenantID != MasterTenantID {
		if owner, ok := m.pageTenants[targetID]; ok && owner != tenantID {
			return fmt.Errorf("tab not found: %s", targetID)
		}
	}
	if _, ok := m.pages[targetID]; !ok {
		return fmt.Errorf("tab not found: %s", targetID)
	}

	m.closeManagedPageLocked(targetID)
	return nil
}

// ConsoleMessages returns captured console messages for a tab.
func (m *Manager) ConsoleMessages(ctx context.Context, targetID string) []ConsoleMessage {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate tenant ownership
	if tenantID != "" && tenantID != MasterTenantID {
		if owner, ok := m.pageTenants[targetID]; ok && owner != tenantID {
			return []ConsoleMessage{}
		}
	}

	msgs := m.console[targetID]
	if msgs == nil {
		return []ConsoleMessage{}
	}

	// Return copy and clear
	result := make([]ConsoleMessage, len(msgs))
	copy(result, msgs)
	m.console[targetID] = nil
	return result
}
