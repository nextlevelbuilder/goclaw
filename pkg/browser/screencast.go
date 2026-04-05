package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// ScreencastSession manages a live screencast stream for a browser page.
type ScreencastSession struct {
	mu        sync.Mutex
	targetID  string
	ch        chan<- ScreencastFrame
	stopCh    chan struct{}
	active    bool
	fps       int
	quality   int
	startedAt time.Time
}

// StartScreencast begins streaming JPEG frames from the specified page.
// Frames are sent to the returned channel. Close the session with StopScreencast.
func (m *Manager) StartScreencast(ctx context.Context, targetID string, fps, quality int) (<-chan ScreencastFrame, error) {
	if fps <= 0 {
		fps = 10
	}
	if quality <= 0 {
		quality = 80
	}

	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("screencast: %w", err)
	}

	ch := make(chan ScreencastFrame, fps*2) // buffer 2 seconds
	if err := page.StartScreencast(fps, quality, m.viewportWidth, m.viewportHeight, ch); err != nil {
		close(ch)
		return nil, fmt.Errorf("start screencast: %w", err)
	}

	return ch, nil
}

// StopScreencast stops the screencast for the specified page.
func (m *Manager) StopScreencast(ctx context.Context, targetID string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("stop screencast: %w", err)
	}
	return page.StopScreencast()
}

// decodeScreencastFrame decodes a base64-encoded JPEG frame from CDP.
func decodeScreencastFrame(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}
