package browser

import (
	"context"
	"fmt"
	"time"
)

// ListTabs returns open tabs filtered by the caller's tenant context.
func (m *Manager) ListTabs(ctx context.Context) ([]TabInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.engine.IsConnected() {
		return nil, fmt.Errorf("browser not running")
	}

	tenantID := tenantIDFromCtx(ctx)

	// Use tenant-scoped engine for page listing
	eng, err := m.tenantEngineLocked(tenantID)
	if err != nil {
		return nil, err
	}

	pages, err := eng.Pages()
	if err != nil {
		if m.remoteURL != "" {
			if reconnErr := m.reconnectLocked(); reconnErr != nil {
				return nil, fmt.Errorf("list pages: %w (reconnect also failed: %v)", err, reconnErr)
			}
			m.logger.Info("auto-reconnected to remote Chrome")
			// Re-acquire tenant engine after reconnect (incognito contexts were reset)
			eng, err = m.tenantEngineLocked(tenantID)
			if err != nil {
				return nil, err
			}
			pages, err = eng.Pages()
			if err != nil {
				return nil, fmt.Errorf("list pages after reconnect: %w", err)
			}
		} else {
			return nil, fmt.Errorf("list pages: %w", err)
		}
	}

	// Resolve profileDir for this tenant — all pages in the same profile belong
	// to the same session, so we can inherit agentKey/sessionKey from the profile.
	profileDir := m.resolveProfileDir(ctx)

	tabs := make([]TabInfo, 0, len(pages))
	for _, p := range pages {
		info, err := p.Info()
		if err != nil || info == nil {
			continue
		}
		tid := p.TargetID()
		m.pages[tid] = p
		if tenantID != "" {
			m.pageTenants[tid] = tenantID
		}
		// Inherit agent/session keys from profileDir for pages that Chrome opened
		// internally (target="_blank", JS window.open) which bypassed OpenTab.
		if _, has := m.pageAgents[tid]; !has {
			if ak := m.profileAgents[profileDir]; ak != "" {
				m.pageAgents[tid] = ak
			}
		}
		if _, has := m.pageSessionKeys[tid]; !has {
			if sk := m.profileSessions[profileDir]; sk != "" {
				m.pageSessionKeys[tid] = sk
			}
		}
		if profileDir != "" {
			m.pageProfiles[tid] = profileDir
		}
		ak := m.pageAgents[tid]
		sk := m.pageSessionKeys[tid]
		tabs = append(tabs, TabInfo{
			TargetID:   tid,
			URL:        info.URL,
			Title:      info.Title,
			AgentKey:   ak,
			SessionKey: sk,
		})
	}
	return tabs, nil
}

// OpenTab opens a new tab with the given URL.
// Pages are created within the tenant's incognito engine for isolation.
// If the tenant already has maxPages open, the oldest idle page is closed first.
func (m *Manager) OpenTab(ctx context.Context, url string) (*TabInfo, error) {
	// Phase 1: under lock — evict, resolve engine, create page
	m.mu.Lock()
	tenantID := tenantIDFromCtx(ctx)
	if m.maxPages > 0 {
		m.evictOldestIfOverLimitLocked(tenantID)
	}
	eng, err := m.tenantEngineLocked(tenantID)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	profileDir := m.resolveProfileDir(ctx)
	m.mu.Unlock()

	// Phase 2: outside lock — create page + wait for load (can be slow)
	ctx = WithProfileDir(ctx, profileDir)
	page, err := eng.NewPage(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open tab: %w", err)
	}

	// Stealth + fingerprint scripts are injected in NewPage() via
	// EvalOnNewDocument (runs before any page JS). No post-load injection needed.

	// WaitStable with bounded timeout — rod's WaitStable blocks until the page
	// has no DOM/network activity for the given duration, which can hang forever
	// on busy pages. We cap it at 10s to avoid blocking the agent.
	waitDone := make(chan error, 1)
	go func() { waitDone <- page.WaitStable(300 * time.Millisecond) }()
	select {
	case err = <-waitDone:
		if err != nil {
			m.logger.Warn("WaitStable returned error (ignored)", "url", url, "error", err)
		}
	case <-ctx.Done():
		m.logger.Warn("WaitStable timed out, proceeding anyway", "url", url)
	case <-time.After(10 * time.Second):
		m.logger.Warn("WaitStable exceeded 10s hard cap, proceeding anyway", "url", url)
	}

	info, _ := page.Info()
	tid := page.TargetID()

	// Phase 3: under lock — register page in maps
	m.mu.Lock()
	m.pages[tid] = page
	m.touchPageLocked(tid)
	if tenantID != "" {
		m.pageTenants[tid] = tenantID
	}
	agentKey := agentKeyFromCtx(ctx)
	sessionKey := sessionKeyFromCtx(ctx)
	if agentKey != "" {
		m.pageAgents[tid] = agentKey
	}
	if sessionKey != "" {
		m.pageSessionKeys[tid] = sessionKey
	}
	// Track profileDir→page and profileDir→session/agent for cross-tab inheritance.
	// All pages in the same profileDir belong to the same session.
	if profileDir != "" {
		m.pageProfiles[tid] = profileDir
		if agentKey != "" {
			m.profileAgents[profileDir] = agentKey
		}
		if sessionKey != "" {
			m.profileSessions[profileDir] = sessionKey
		}
	}
	m.mu.Unlock()

	// Set up console listener via Page interface
	page.SetupConsoleListener(func(msg ConsoleMessage) {
		m.mu.Lock()
		msgs := m.console[tid]
		if len(msgs) >= 500 {
			msgs = msgs[1:]
		}
		m.console[tid] = append(msgs, msg)
		m.mu.Unlock()
	})

	tab := &TabInfo{TargetID: tid, URL: url}
	if info != nil {
		tab.URL = info.URL
		tab.Title = info.Title
	}
	return tab, nil
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

	if page, ok := m.pages[oldestID]; ok {
		_ = page.Close()
	}
	delete(m.pages, oldestID)
	delete(m.console, oldestID)
	delete(m.pageTenants, oldestID)
	delete(m.pageAgents, oldestID)
	delete(m.pageSessionKeys, oldestID)
	delete(m.pageLastUsed, oldestID)
	delete(m.pageProfiles, oldestID)
	m.refs.Remove(oldestID)
	m.logger.Info("evicted oldest page (max pages reached)", "targetId", oldestID, "tenant", tenantID)
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

	return page.Activate()
}

// CloseTab closes a tab.
func (m *Manager) CloseTab(ctx context.Context, targetID string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.getPageForTenant(targetID, tenantID)
	if err != nil {
		return err
	}

	delete(m.pages, targetID)
	delete(m.console, targetID)
	delete(m.pageTenants, targetID)
	delete(m.pageAgents, targetID)
	delete(m.pageSessionKeys, targetID)
	delete(m.pageLastUsed, targetID)
	delete(m.pageProfiles, targetID)
	m.refs.Remove(targetID)
	return page.Close()
}

// BackfillAgentKey associates a targetID with an agentKey if no mapping exists yet.
// This ensures tabs opened before agent tracking was deployed get associated
// the first time an agent interacts with them.
func (m *Manager) BackfillAgentKey(targetID, agentKey string) {
	if targetID == "" || agentKey == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pageAgents[targetID]; !ok {
		if _, exists := m.pages[targetID]; exists {
			m.pageAgents[targetID] = agentKey
		}
	}
}

// BackfillSessionKey associates a targetID with a sessionKey if no mapping exists yet.
func (m *Manager) BackfillSessionKey(targetID, sessionKey string) {
	if targetID == "" || sessionKey == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pageSessionKeys[targetID]; !ok {
		if _, exists := m.pages[targetID]; exists {
			m.pageSessionKeys[targetID] = sessionKey
		}
	}
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
