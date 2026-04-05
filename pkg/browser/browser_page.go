package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// Snapshot takes an accessibility snapshot of a page.
func (m *Manager) Snapshot(ctx context.Context, targetID string, opts SnapshotOptions) (*SnapshotResult, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	// Single-frame snapshot (specific frame or default main frame)
	if opts.FrameID != "" {
		nodes, err := page.GetAXTreeForFrame(opts.FrameID)
		if err != nil {
			return nil, fmt.Errorf("get AX tree for frame %s: %w", opts.FrameID, err)
		}
		snap := FormatSnapshot(nodes, opts)
		info, _ := page.Info()
		snap.TargetID = targetID
		if info != nil {
			snap.URL = info.URL
			snap.Title = info.Title
		}
		m.refs.Store(targetID, snap.Refs)
		return snap, nil
	}

	// Multi-frame snapshot: main frame + all child frames
	if opts.IncludeFrames {
		return m.snapshotWithFrames(page, targetID, opts)
	}

	// Default: main frame only
	nodes, err := page.GetAXTree()
	if err != nil {
		return nil, fmt.Errorf("get AX tree: %w", err)
	}

	snap := FormatSnapshot(nodes, opts)
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

// snapshotWithFrames collects AX trees from main frame + child iframes.
func (m *Manager) snapshotWithFrames(page Page, targetID string, opts SnapshotOptions) (*SnapshotResult, error) {
	frames, err := page.GetFrameTree()
	if err != nil {
		// Fallback to main-only if frame tree unavailable
		nodes, err2 := page.GetAXTree()
		if err2 != nil {
			return nil, fmt.Errorf("get AX tree: %w", err2)
		}
		snap := FormatSnapshot(nodes, opts)
		m.refs.Store(targetID, snap.Refs)
		return snap, nil
	}

	if len(frames) == 0 {
		nodes, err := page.GetAXTree()
		if err != nil {
			return nil, fmt.Errorf("get AX tree: %w", err)
		}
		snap := FormatSnapshot(nodes, opts)
		m.refs.Store(targetID, snap.Refs)
		return snap, nil
	}

	// Collect nodes from each frame
	var allFrames []frameNodes
	mainFrameID := frames[0].FrameID

	// Main frame first (use default GetAXTree for reliability)
	mainNodes, err := page.GetAXTree()
	if err != nil {
		return nil, fmt.Errorf("get main AX tree: %w", err)
	}
	allFrames = append(allFrames, frameNodes{
		FrameID: mainFrameID,
		URL:     frames[0].URL,
		Nodes:   mainNodes,
	})

	// Child frames
	for _, fr := range frames[1:] {
		// For cross-origin iframes, use the OOPIF target ID which GetAXTreeForFrame
		// will resolve by attaching to the iframe's CDP target.
		queryID := fr.FrameID
		if fr.OOPIFTarget != "" {
			queryID = fr.OOPIFTarget
		}
		childNodes, err := page.GetAXTreeForFrame(queryID)
		if err != nil {
			// Skip inaccessible frames (cross-origin restrictions, etc.)
			continue
		}
		if len(childNodes) == 0 {
			continue
		}
		allFrames = append(allFrames, frameNodes{
			FrameID: fr.FrameID,
			URL:     fr.URL,
			Nodes:   childNodes,
		})
	}

	snap := FormatMultiFrameSnapshot(allFrames, opts)
	info, _ := page.Info()
	snap.TargetID = targetID
	if info != nil {
		snap.URL = info.URL
		snap.Title = info.Title
	}

	m.refs.Store(targetID, snap.Refs)
	return snap, nil
}

// ListFrames returns the frame hierarchy for a page.
func (m *Manager) ListFrames(ctx context.Context, targetID string) ([]FrameInfo, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return page.GetFrameTree()
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
func (m *Manager) Navigate(ctx context.Context, targetID, url string) error {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
	m.mu.Unlock()

	if err != nil {
		return err
	}

	if err := page.Navigate(url); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitStable(300 * time.Millisecond); err != nil {
		return fmt.Errorf("wait stable after navigate: %w", err)
	}
	return nil
}
