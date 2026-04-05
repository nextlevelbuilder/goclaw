// go:build integration

package browser_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

// TestScreencastIntegration launches real Chrome, opens a page, and captures screencast frames.
// Run with: go test -v -tags integration -run TestScreencastIntegration ./pkg/browser/
func TestScreencastIntegration(t *testing.T) {
	if os.Getenv("BROWSER_INTEGRATION") == "" {
		t.Skip("set BROWSER_INTEGRATION=1 to run real browser tests")
	}

	ctx := context.Background()

	mgr := browser.New(
		browser.WithHeadless(false),
		browser.WithIdleTimeout(0),
	)
	defer mgr.Close()

	// Start browser
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Open a page with some visual content
	tab, err := mgr.OpenTab(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	t.Logf("opened tab: %s (%s)", tab.TargetID, tab.URL)

	// Start screencast
	ch, err := mgr.StartScreencast(ctx, tab.TargetID, 5, 60)
	if err != nil {
		t.Fatalf("start screencast: %v", err)
	}
	t.Log("screencast started, waiting for frames...")

	// Trigger page changes to generate more frames — scroll and evaluate JS
	go func() {
		time.Sleep(500 * time.Millisecond)
		for i := range 5 {
			_, _ = mgr.Evaluate(ctx, tab.TargetID, fmt.Sprintf(
				`document.body.style.backgroundColor = "hsl(%d, 70%%, 80%%)"`, i*60))
			time.Sleep(300 * time.Millisecond)
		}
	}()

	// Collect frames
	var frames []browser.ScreencastFrame
	timeout := time.After(5 * time.Second)
	minFrames := 1 // CDP only sends frames on visual change

loop:
	for {
		select {
		case frame, ok := <-ch:
			if !ok {
				t.Log("channel closed")
				break loop
			}
			frames = append(frames, frame)
			t.Logf("frame #%d: %d bytes, session=%d, %.0fx%.0f",
				len(frames), len(frame.Data), frame.SessionID,
				frame.Metadata.DeviceWidth, frame.Metadata.DeviceHeight)
			if len(frames) >= 10 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	// Stop screencast
	if err := mgr.StopScreencast(ctx, tab.TargetID); err != nil {
		t.Fatalf("stop screencast: %v", err)
	}

	if len(frames) < minFrames {
		t.Fatalf("expected at least %d frames, got %d", minFrames, len(frames))
	}
	t.Logf("captured %d frames total", len(frames))

	// Save first frame as JPEG for visual verification
	outDir := filepath.Join(os.TempDir(), "goclaw_screencast_test")
	_ = os.MkdirAll(outDir, 0755)
	for i, f := range frames {
		path := filepath.Join(outDir, fmt.Sprintf("frame_%03d.jpg", i))
		if err := os.WriteFile(path, f.Data, 0644); err != nil {
			t.Errorf("write frame %d: %v", i, err)
		}
	}
	t.Logf("frames saved to %s", outDir)
}
