//go:build !windows

package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestLocalExtractParser_TimeoutReapsProcessGroup proves a timeout kills the
// whole process group (not just the direct child): the stub forks a grandchild
// sleep, records its PID, and the test asserts that PID is gone afterwards.
func TestLocalExtractParser_TimeoutReapsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	body := fmt.Sprintf("#!/bin/sh\nsleep 30 &\necho $! > %s\nwait\n", pidFile)
	slow := writeStubScript(t, dir, "pdftotext", body)

	p := newStubParser(LocalExtractConfig{Enabled: true, Timeout: 1 * time.Second},
		stubLookPath(map[string]string{"pdftotext": slow}))

	got, err := p.Extract(context.Background(), "/tmp/slow.pdf", mimePDF)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if got != "" {
		t.Errorf("expected no partial text on timeout, got %d bytes", len(got))
	}

	pids := waitForRecordedPIDs(t, pidFile, 1, 2*time.Second)
	time.Sleep(200 * time.Millisecond) // let the OS reap the killed group
	if orphans := findLivePIDs(t, pids); len(orphans) > 0 {
		t.Errorf("grandchild sleep not reaped after timeout: %v", orphans)
	}
}
