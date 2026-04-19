//go:build !windows

package tools

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestShellAbort_ProcessGroupKilled verifies that cancelling ctx after spawning a
// command that forks background children kills the entire process group within 4s
// and leaves no orphan sleep processes.
//
// The command `sh -c "sleep 60 & sleep 60 & wait"` spawns two background sleeps.
// With process-group kill, both background sleeps must die when ctx is cancelled.
func TestShellAbort_ProcessGroupKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill not supported on Windows")
	}

	// Marker to identify our specific sleep processes.
	marker := fmt.Sprintf("goclaw_abort_test_%d", time.Now().UnixNano())
	command := fmt.Sprintf("sleep 60 & echo 'marker=%s' & sleep 60 & wait", marker)

	tool := NewExecTool(t.TempDir(), false)
	tool.timeout = 10 * time.Second // generous outer timeout

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan *Result, 1)
	go func() {
		done <- tool.executeOnHost(ctx, command, t.TempDir())
	}()

	// Give the shell time to fork the sleep processes.
	time.Sleep(100 * time.Millisecond)

	// Cancel ctx — should trigger SIGTERM → 3s grace → SIGKILL.
	cancel()

	// Verify tool returns within 4s (3s grace + 1s buffer).
	select {
	case result := <-done:
		if result == nil {
			t.Fatal("expected non-nil result after abort")
		}
		// Result should indicate abortion, not normal completion.
		if !result.IsError {
			t.Errorf("expected IsError=true after abort, got ForLLM=%q", result.ForLLM)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("executeOnHost did not return within 4s after ctx cancel")
	}

	// Give the OS a moment to reap the killed processes.
	time.Sleep(200 * time.Millisecond)

	// Verify no orphan sleep processes remain. We check with `ps` filtering by
	// "sleep 60" (the exact argument). pgrep is not reliably available on macOS CI.
	orphans := findOrphanSleeps(t)
	if len(orphans) > 0 {
		t.Errorf("found %d orphan 'sleep 60' process(es) after abort: %v", len(orphans), orphans)
	}
}

// findOrphanSleeps returns PIDs of any remaining `sleep 60` processes.
// Uses `ps aux` output parsed in Go — avoids pgrep availability issues on macOS.
func findOrphanSleeps(t *testing.T) []string {
	t.Helper()

	out, err := exec.Command("ps", "aux").Output()
	if err != nil {
		t.Logf("ps aux failed (non-fatal): %v", err)
		return nil
	}

	var found []string
	for line := range strings.SplitSeq(string(out), "\n") {
		// Match lines containing "sleep 60" but not the grep/ps command itself.
		if strings.Contains(line, "sleep 60") && !strings.Contains(line, "ps aux") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				found = append(found, fields[1]) // PID is column 2 in ps aux
			}
		}
	}
	return found
}
