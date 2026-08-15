//go:build !windows

package cronexec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun_StdoutOK(t *testing.T) {
	res := Run(context.Background(), Spec{Argv: []string{"sh", "-c", "printf 'hello world'"}})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok (err=%v)", res.Status, res.Err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}
	if res.Summary != "hello world" {
		t.Errorf("summary = %q, want %q", res.Summary, "hello world")
	}
}

func TestRun_NonZeroExitIsError(t *testing.T) {
	res := Run(context.Background(), Spec{Argv: []string{"sh", "-c", "echo oops 1>&2; exit 7"}})
	if res.Status != StatusError {
		t.Fatalf("status = %q, want error", res.Status)
	}
	if res.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", res.ExitCode)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "code 7") {
		t.Errorf("err = %v, want it to mention exit code 7", res.Err)
	}
	if !strings.Contains(res.Err.Error(), "oops") {
		t.Errorf("err = %v, want it to include stderr (oops)", res.Err)
	}
	if !strings.Contains(res.Summary, "oops") {
		t.Errorf("summary = %q, want it to include stderr", res.Summary)
	}
}

func TestRun_Timeout(t *testing.T) {
	start := time.Now()
	res := Run(context.Background(), Spec{
		Argv:    []string{"sh", "-c", "sleep 10"},
		Timeout: 200 * time.Millisecond,
	})
	if res.Status != StatusError {
		t.Fatalf("status = %q, want error", res.Status)
	}
	if res.Termination != TermTimeout {
		t.Errorf("termination = %q, want %q", res.Termination, TermTimeout)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s, expected the watchdog to kill it promptly", elapsed)
	}
}

func TestRun_NoOutputTimeout(t *testing.T) {
	res := Run(context.Background(), Spec{
		Argv:            []string{"sh", "-c", "sleep 10"},
		NoOutputTimeout: 200 * time.Millisecond,
	})
	if res.Status != StatusError {
		t.Fatalf("status = %q, want error", res.Status)
	}
	if res.Termination != TermNoOutputTimeout {
		t.Errorf("termination = %q, want %q", res.Termination, TermNoOutputTimeout)
	}
}

func TestRun_NoOutputTimeoutNotTrippedWhenProducingOutput(t *testing.T) {
	// Emits a line every 50ms for ~300ms, then exits 0. A 200ms no-output
	// timeout must NOT fire because output keeps arriving.
	res := Run(context.Background(), Spec{
		Argv:            []string{"sh", "-c", "for i in 1 2 3 4 5 6; do echo tick; sleep 0.05; done"},
		NoOutputTimeout: 200 * time.Millisecond,
		Timeout:         5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok (term=%q err=%v)", res.Status, res.Termination, res.Err)
	}
}

func TestRun_Stdin(t *testing.T) {
	res := Run(context.Background(), Spec{
		Argv:  []string{"sh", "-c", "cat"},
		Input: "piped-input",
	})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	if res.Summary != "piped-input" {
		t.Errorf("summary = %q, want %q", res.Summary, "piped-input")
	}
}

func TestRun_Env(t *testing.T) {
	res := Run(context.Background(), Spec{
		Argv: []string{"sh", "-c", "printf '%s' \"$CRON_TEST_VAR\""},
		Env:  map[string]string{"CRON_TEST_VAR": "from-env"},
	})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	if res.Summary != "from-env" {
		t.Errorf("summary = %q, want %q", res.Summary, "from-env")
	}
}

func TestRun_OutputCap(t *testing.T) {
	res := Run(context.Background(), Spec{
		Argv:           []string{"sh", "-c", "yes x | head -c 100000"},
		OutputMaxBytes: 1024,
		Timeout:        5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok (err=%v)", res.Status, res.Err)
	}
	if len(res.Stdout) > 1024 {
		t.Errorf("stdout length = %d, want capped at 1024", len(res.Stdout))
	}
}

func TestRun_EmptyArgv(t *testing.T) {
	res := Run(context.Background(), Spec{})
	if res.Status != StatusError || res.Err == nil {
		t.Fatalf("empty argv should error, got status=%q err=%v", res.Status, res.Err)
	}
}

func TestRun_StdoutPreferredOverStderr(t *testing.T) {
	res := Run(context.Background(), Spec{Argv: []string{"sh", "-c", "echo out; echo err 1>&2"}})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want ok", res.Status)
	}
	if !strings.Contains(res.Summary, "stdout:") || !strings.Contains(res.Summary, "stderr:") {
		t.Errorf("summary should label both streams, got %q", res.Summary)
	}
}
