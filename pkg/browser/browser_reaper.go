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

	if !m.isRunningLocked() {
		return
	}

	now := time.Now()
	for targetID, lastUsed := range m.pageLastUsed {
		if now.Sub(lastUsed) <= m.idleTimeout {
			continue
		}

		if _, ok := m.pages[targetID]; !ok {
			delete(m.pageLastUsed, targetID)
			continue
		}

		idleFor := now.Sub(lastUsed).Round(time.Second)
		m.closeManagedPageLocked(targetID)
		m.logger.Info("reaper: closed idle page", "targetId", targetID, "idle", idleFor)
	}
}
