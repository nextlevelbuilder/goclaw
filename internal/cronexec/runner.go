// Package cronexec runs deterministic shell-command cron payloads inside the
// gateway process WITHOUT invoking an LLM. It mirrors openclaw's
// src/cron/command-runner.ts: wall-clock timeout, a no-output watchdog, output
// capping, and process-group termination so a timed-out command's forked
// children do not survive.
package cronexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultOutputMaxBytes caps captured stdout/stderr per stream when the spec
	// does not set OutputMaxBytes. 16 KiB matches the cron run-log truncation.
	DefaultOutputMaxBytes = 16 * 1024
	// killGrace is how long to wait after SIGTERM before escalating to SIGKILL.
	killGrace = 5 * time.Second
	// watchdogInterval is how often the no-output watchdog wakes to check.
	watchdogInterval = 250 * time.Millisecond
)

// Run statuses.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Termination reasons (empty when the process exited on its own).
const (
	TermTimeout         = "timeout"
	TermNoOutputTimeout = "no-output-timeout"
)

// Spec is a deterministic command to run.
type Spec struct {
	Argv            []string
	Cwd             string
	Env             map[string]string
	Input           string
	Timeout         time.Duration // wall-clock; <=0 means no explicit limit (still bounded by ctx)
	NoOutputTimeout time.Duration // kill if no stdout/stderr for this long; <=0 disables
	OutputMaxBytes  int           // per-stream cap; <=0 uses DefaultOutputMaxBytes
}

// Result is the outcome of a command run.
type Result struct {
	Status      string // StatusOK or StatusError
	ExitCode    int    // process exit code (-1 if not started or signalled)
	Stdout      string
	Stderr      string
	Summary     string // stdout (preferred), else stderr, else combined
	Termination string // "", TermTimeout, or TermNoOutputTimeout
	Err         error  // non-nil when Status==StatusError
}

// Run executes spec and returns its outcome. It never returns an error itself;
// failures are reported via Result.Status/Result.Err so callers can record a
// run log uniformly.
func Run(ctx context.Context, spec Spec) Result {
	if len(spec.Argv) == 0 {
		return Result{Status: StatusError, ExitCode: -1, Err: errors.New("command requires non-empty argv")}
	}

	runCtx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}
	if len(spec.Env) > 0 {
		cmd.Env = mergedEnv(spec.Env)
	}
	if spec.Input != "" {
		cmd.Stdin = strings.NewReader(spec.Input)
	}

	maxBytes := spec.OutputMaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultOutputMaxBytes
	}
	stdout := newCappedBuffer(maxBytes)
	stderr := newCappedBuffer(maxBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	setProcessGroup(cmd) // platform-specific: own process group for tree kill

	if err := cmd.Start(); err != nil {
		return Result{Status: StatusError, ExitCode: -1, Err: fmt.Errorf("command failed to start: %w", err)}
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	start := time.Now()
	var watchC <-chan time.Time
	if spec.NoOutputTimeout > 0 {
		ticker := time.NewTicker(watchdogInterval)
		defer ticker.Stop()
		watchC = ticker.C
	}

	for {
		select {
		case err := <-waitErr:
			return finalize(stdout, stderr, err, "")
		case <-runCtx.Done():
			return finalize(stdout, stderr, terminate(cmd, waitErr), TermTimeout)
		case <-watchC:
			last := latest(stdout.LastWrite(), stderr.LastWrite())
			if last.IsZero() {
				last = start
			}
			if time.Since(last) >= spec.NoOutputTimeout {
				return finalize(stdout, stderr, terminate(cmd, waitErr), TermNoOutputTimeout)
			}
		}
	}
}

// terminate signals the process group (SIGTERM, then SIGKILL after killGrace),
// waits for the process to be reaped, and returns cmd.Wait()'s error.
func terminate(cmd *exec.Cmd, waitErr <-chan error) error {
	signalGroup(cmd, false)                                           // SIGTERM
	t := time.AfterFunc(killGrace, func() { signalGroup(cmd, true) }) // SIGKILL
	err := <-waitErr
	t.Stop()
	return err
}

func finalize(stdout, stderr *cappedBuffer, waitErr error, termination string) Result {
	outStr := stdout.String()
	errStr := stderr.String()

	exitCode := 0
	signalled := false
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode() // -1 when terminated by a signal
			if exitCode < 0 {
				signalled = true
			}
		} else {
			exitCode = -1
		}
	}

	res := Result{
		ExitCode:    exitCode,
		Stdout:      outStr,
		Stderr:      errStr,
		Summary:     buildSummary(outStr, errStr),
		Termination: termination,
	}

	if termination == "" && waitErr == nil && exitCode == 0 && !signalled {
		res.Status = StatusOK
		return res
	}
	res.Status = StatusError
	res.Err = commandError(termination, exitCode, errStr)
	return res
}

func commandError(termination string, exitCode int, stderr string) error {
	switch termination {
	case TermTimeout:
		return errors.New("command timed out")
	case TermNoOutputTimeout:
		return errors.New("command produced no output before the no-output timeout")
	}
	msg := fmt.Sprintf("command exited with code %d", exitCode)
	if s := strings.TrimSpace(stderr); s != "" {
		msg += ": " + truncate(s, 500)
	}
	return errors.New(msg)
}

// buildSummary mirrors openclaw: prefer stdout, fall back to stderr, and when
// both are present, label them.
func buildSummary(stdout, stderr string) string {
	so := strings.TrimSpace(stdout)
	se := strings.TrimSpace(stderr)
	switch {
	case so != "" && se != "":
		return "stdout:\n" + so + "\n\nstderr:\n" + se
	case so != "":
		return so
	default:
		return se
	}
}

func mergedEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

// cappedBuffer is an io.Writer that retains at most max bytes and records the
// time of the last write (for the no-output watchdog). Writes never error or
// block, so the child process is never throttled by a full buffer.
type cappedBuffer struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	max  int
	last time.Time
}

func newCappedBuffer(max int) *cappedBuffer {
	return &cappedBuffer{max: max}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = time.Now()
	if c.max > 0 {
		remaining := c.max - c.buf.Len()
		if remaining <= 0 {
			return len(p), nil // capped: discard but report full write
		}
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
			return len(p), nil
		}
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *cappedBuffer) LastWrite() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}
