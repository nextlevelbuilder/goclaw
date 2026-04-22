package browser

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// watchPageClose spawns a goroutine that closes page when ctx is cancelled.
// Returns a cancel func that stops the watchdog on normal-path close.
// Uses sync.Once so page.Close() is idempotent if both paths fire concurrently.
func watchPageClose(ctx context.Context, page *rod.Page) (stopWatchdog func()) {
	var once sync.Once
	closeFn := func() { _ = page.Close() }
	stopped := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			once.Do(closeFn)
		case <-stopped:
		}
	}()

	return func() {
		close(stopped)
	}
}

// Snapshot takes an accessibility snapshot of a page.
func (m *Manager) Snapshot(ctx context.Context, targetID string, opts SnapshotOptions) (*SnapshotResult, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	result, err := proto.AccessibilityGetFullAXTree{}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("get AX tree: %w", err)
	}

	snap := FormatSnapshot(result.Nodes, opts)
	info, _ := page.Info()
	snap.TargetID = targetID
	if info != nil {
		snap.URL = info.URL
		snap.Title = info.Title
	}

	// Cache refs
	m.refs.Store(targetID, snap.Refs)

	return snap, nil
}

// Screenshot captures a page screenshot as PNG bytes.
func (m *Manager) Screenshot(ctx context.Context, targetID string, fullPage bool) ([]byte, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	if fullPage {
		return page.Screenshot(fullPage, &proto.PageCaptureScreenshot{
			Format: proto.PageCaptureScreenshotFormatPng,
		})
	}
	return page.Screenshot(false, nil)
}

// Navigate navigates a page to a URL.
// A ctx-cancel watchdog closes the page if ctx is done during the blocking WaitStable call.
func (m *Manager) Navigate(ctx context.Context, targetID, url string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return err
	}

	// Watchdog: close page on ctx cancel to unblock any pending Rod CDP calls.
	stop := watchPageClose(ctx, page)
	defer stop()

	if err := page.Navigate(url); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitStable(300 * time.Millisecond); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("wait stable after navigate: %w", err)
	}
	return nil
}

// Close shuts down the browser if running.
func (m *Manager) Close() error {
	return m.Stop(context.Background())
}

// Refs returns the RefStore for external use (e.g. actions).
func (m *Manager) Refs() *RefStore {
	return m.refs
}
