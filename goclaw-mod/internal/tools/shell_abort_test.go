//go:build !windows

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "sleep-pids")
	quotedPIDFile := shellSingleQuote(pidFile)
	command := fmt.Sprintf(
		"sleep 60 & echo $! >> %s; sleep 60 & echo $! >> %s; wait",
		quotedPIDFile,
		quotedPIDFile,
	)

	tool := NewExecTool(tmpDir, false)
	tool.timeout = 10 * time.Second // generous outer timeout

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan *Result, 1)
	go func() {
		done <- tool.executeOnHost(ctx, command, tmpDir)
	}()

	sleepPIDs := waitForRecordedPIDs(t, pidFile, 2, time.Second)

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

	// Verify no child sleep process from this test remains.
	orphans := findLivePIDs(t, sleepPIDs)
	if len(orphans) > 0 {
		t.Errorf("found %d live sleep process(es) after abort: %v", len(orphans), orphans)
	}
}

func waitForRecordedPIDs(t *testing.T, pidFile string, want int, timeout time.Duration) []string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		pids := readRecordedPIDs(t, pidFile)
		if len(pids) >= want {
			return pids[:want]
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleep child PIDs not recorded within %s; got %v", timeout, pids)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readRecordedPIDs(t *testing.T, pidFile string) []string {
	t.Helper()

	data, err := os.ReadFile(pidFile)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read pid file: %v", err)
	}
	return strings.Fields(string(data))
}

func findLivePIDs(t *testing.T, pids []string) []string {
	t.Helper()

	var found []string
	for _, pid := range pids {
		out, err := exec.Command("ps", "-p", pid, "-o", "pid=").Output()
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(out)) != "" {
			found = append(found, pid)
		}
	}
	return found
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
