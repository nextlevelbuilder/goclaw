package wechat

import (
	"fmt"
	"sync"
	"time"
)

const (
	// SessionExpiredErrCode is the server error code for expired sessions.
	SessionExpiredErrCode = -14
	// sessionPauseDuration is the cooldown period after session expiry.
	sessionPauseDuration = 1 * time.Hour
)

// sessionGuard tracks session pause state per account.
type sessionGuard struct {
	mu         sync.RWMutex
	pauseUntil map[string]time.Time
}

func newSessionGuard() *sessionGuard {
	return &sessionGuard{
		pauseUntil: make(map[string]time.Time),
	}
}

// Pause marks the session as paused for one hour.
func (g *sessionGuard) Pause(accountID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pauseUntil[accountID] = time.Now().Add(sessionPauseDuration)
}

// IsPaused returns true if the session is still paused.
func (g *sessionGuard) IsPaused(accountID string) bool {
	g.mu.RLock()
	until, ok := g.pauseUntil[accountID]
	g.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(until) {
		g.mu.Lock()
		delete(g.pauseUntil, accountID)
		g.mu.Unlock()
		return false
	}
	return true
}

// RemainingPauseMs returns milliseconds remaining until pause expires.
func (g *sessionGuard) RemainingPauseMs(accountID string) int64 {
	g.mu.RLock()
	until, ok := g.pauseUntil[accountID]
	g.mu.RUnlock()
	if !ok {
		return 0
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		g.mu.Lock()
		delete(g.pauseUntil, accountID)
		g.mu.Unlock()
		return 0
	}
	return remaining.Milliseconds()
}

// AssertActive throws if the session is paused.
func (g *sessionGuard) AssertActive(accountID string) error {
	if g.IsPaused(accountID) {
		remainingMin := (g.RemainingPauseMs(accountID) + 59999) / 60000
		return fmt.Errorf("session paused for accountId=%s, %d min remaining (errcode %d)",
			accountID, remainingMin, SessionExpiredErrCode)
	}
	return nil
}
