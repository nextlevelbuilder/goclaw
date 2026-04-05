package browser

import "time"

// runReaper periodically closes pages that have been idle longer than idleTimeout.
// Runs as a goroutine; exits when stopReaper is closed.
func (m *Manager) runReaper() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopReaper:
			return
		case <-ticker.C:
			m.reapIdlePages()
		}
	}
}

// reapIdlePages closes pages idle longer than idleTimeout.
func (m *Manager) reapIdlePages() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return
	}

	now := time.Now()
	for targetID, lastUsed := range m.pageLastUsed {
		if now.Sub(lastUsed) <= m.idleTimeout {
			continue
		}

		page, ok := m.pages[targetID]
		if !ok {
			delete(m.pageLastUsed, targetID)
			continue
		}

		if err := page.Close(); err != nil {
			m.logger.Warn("reaper: failed to close idle page", "targetId", targetID, "error", err)
			continue
		}

		delete(m.pages, targetID)
		delete(m.console, targetID)
		delete(m.pageTenants, targetID)
		delete(m.pageLastUsed, targetID)
		m.refs.Remove(targetID)
		m.logger.Info("reaper: closed idle page", "targetId", targetID, "idle", now.Sub(lastUsed).Round(time.Second))
	}
}
