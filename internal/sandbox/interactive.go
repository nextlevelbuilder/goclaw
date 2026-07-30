// Package sandbox — interactive.go adds a long-lived exec primitive on top of the
// one-shot Exec: a process kept alive inside the container with a WRITABLE stdin
// and line-streamed stdout/stderr.
//
// It exists for bidirectional sessions with a coding CLI, e.g.
//
//	claude --input-format stream-json --output-format stream-json
//
// where user turns and permission answers must be written IN over the life of the
// process while events and permission requests stream OUT.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrSessionClosed is returned by WriteLine when the session has been closed or
// the process has already exited. Callers get this error instead of a panic or an
// opaque "file already closed" from the underlying pipe.
var ErrSessionClosed = errors.New("interactive session closed")

const (
	// interactiveWaitDelay bounds how long a wedged child may delay reaping after
	// its context is cancelled: os/exec force-closes the stdio pipes and gives up
	// on the I/O copying goroutines after this, so Wait() cannot hang forever on a
	// process that ignores the kill or on a grandchild holding a pipe open.
	// Mirrors the precedent in internal/providers/claude_cli_chat.go.
	interactiveWaitDelay = 5 * time.Second

	// interactiveCloseGrace bounds how long Close() waits for the process to be
	// reaped after killing it. It must exceed interactiveWaitDelay, which is what
	// actually guarantees progress; this is the backstop so shutdown returns an
	// error rather than blocking a caller indefinitely.
	interactiveCloseGrace = 15 * time.Second

	// interactiveEOFGrace is how long Close() waits for the process to exit of its
	// own accord after stdin is closed, BEFORE resorting to a kill. Long enough for
	// a CLI to finish its turn and flush a final event, short enough that a wedged
	// process can't drag out gateway shutdown.
	interactiveEOFGrace = 3 * time.Second
)

// ExecInteractive starts a long-lived process inside the container with a
// writable stdin and line-streamed stdout/stderr. It returns as soon as the
// process has STARTED; the caller drives it via the returned session and must
// Close() it to release the process.
//
// Env (WithEnv) and workDir are handled exactly as in Exec — same -e flags, same
// -w flag, same argv order (see buildDockerExecArgs); the only argv difference is
// the added -i, which keeps stdin attached.
//
// TIMEOUT: Config.TimeoutSec is deliberately NOT applied here. Exec wraps ctx in
// a 5-minute-by-default deadline because a one-shot command that runs that long
// is stuck; an interactive session is long-lived BY DESIGN (it lives as long as
// the user's conversation), so the same deadline would kill healthy sessions.
// The caller's ctx is therefore the only deadline: cancelling it kills the
// process, and a caller that wants a cap sets one on the ctx it passes in.
func (s *DockerSandbox) ExecInteractive(ctx context.Context, command []string, workDir string, opts ...ExecOption) (InteractiveSession, error) {
	s.touch()

	o := ApplyExecOpts(opts)
	args := buildDockerExecArgs(s.containerID, command, workDir, o.Env, true)

	// Own cancel func so Close() can kill the process without requiring the caller
	// to cancel their (possibly long-lived, shared) ctx.
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, "docker", args...)

	sess, err := startInteractiveSession(cmd, cancel, s.maxOutputBytes(), o)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("docker exec -i: %w", err)
	}
	// Keep the container off the pruner's idle list while the session is alive.
	sess.touch = s.touch

	slog.Debug("sandbox interactive session started", "id", s.containerID, "command", command)
	return sess, nil
}

// interactiveSession is the concrete InteractiveSession, driving an already-built
// *exec.Cmd. It is deliberately independent of Docker: startInteractiveSession is
// the seam that lets the state machine be tested against a local process
// (/bin/cat, sh -c …) with no Docker daemon involved.
type interactiveSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	// touch, when set, marks the owning sandbox as recently used on each write.
	touch func()

	// mu guards stdin (a pipe written from arbitrary caller goroutines) plus the
	// exit bookkeeping below.
	mu      sync.Mutex
	closed  bool // stdin no longer writable (closed by caller, or process exited)
	closing bool // Close() was called → a ctx-cancelled exit is expected, not a failure
	result  *ExecResult
	waitErr error
	// exitCode mirrors result.ExitCode so ExitCode() need not nil-check.
	exitCode  int
	stdinOnce sync.Once

	stdout, stderr *limitedBuffer
	outLW, errLW   *lineWriter

	done chan struct{}
}

var _ InteractiveSession = (*interactiveSession)(nil)
var _ Sandbox = (*DockerSandbox)(nil)

// startInteractiveSession attaches capture/streaming to cmd, starts it, and
// begins reaping it in the background. cancel (may be nil) is invoked to kill the
// process on Close.
//
// This is the test seam for the session state machine: tests build a plain local
// *exec.Cmd instead of a `docker exec` one and get the identical session
// behaviour without a Docker daemon.
func startInteractiveSession(cmd *exec.Cmd, cancel context.CancelFunc, maxOut int, o ExecOpts) (*interactiveSession, error) {
	if cancel == nil {
		cancel = func() {}
	}
	// Force-close the pipes if the process lingers after a kill, so Wait/Close
	// cannot hang on a wedged child.
	cmd.WaitDelay = interactiveWaitDelay

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	maxOut = normalizeMaxOutput(maxOut)
	s := &interactiveSession{
		cmd:    cmd,
		stdin:  stdin,
		cancel: cancel,
		stdout: &limitedBuffer{max: maxOut},
		stderr: &limitedBuffer{max: maxOut},
		done:   make(chan struct{}),
	}

	// Both streams are captured in full (capped) AND line-streamed, exactly as in
	// Exec. os/exec copies each stream on its own goroutine, and each stream has
	// its own lineWriter/limitedBuffer, so no state is shared between them.
	var stdoutW, stderrW io.Writer
	stdoutW, s.outLW = teeLines(s.stdout, o.StdoutLine)
	stderrW, s.errLW = teeLines(s.stderr, o.StderrLine)
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	go s.reap()
	return s, nil
}

// reap waits for the process, then publishes the result and closes done. It runs
// exactly once (started by startInteractiveSession), which is what makes Wait
// re-entrant and safe to call concurrently with Close.
func (s *interactiveSession) reap() {
	err := s.cmd.Wait()

	// cmd.Wait has returned, so the stdout/stderr copying goroutines are finished:
	// flushing the line writers and reading the buffers here is race-free. The
	// flush surfaces a final line the process emitted without a trailing newline.
	s.outLW.flushIfSet()
	s.errLW.flushIfSet()

	// The exit status always comes from ProcessState when the process was reaped
	// (-1 when it died from a signal). Reading it here rather than from the error
	// matters because os/exec reports a *ctx* error — not an *exec.ExitError —
	// when the process exits after its context was cancelled, which is exactly
	// what a graceful Close() does.
	exitCode := -1
	if ps := s.cmd.ProcessState; ps != nil {
		exitCode = ps.ExitCode()
	}

	var exitErr *exec.ExitError
	var waitErr error
	switch {
	case err == nil,
		// A non-zero exit is data, not an error — matching Exec, which reports it
		// via ExecResult.ExitCode.
		errors.As(err, &exitErr):
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The process was terminated because the session's context was cancelled.
		// Via Close() that is the normal end of a session, so it is not an error;
		// via the CALLER's ctx it is worth surfacing, since the caller may be
		// waiting on output that will now never arrive.
		s.mu.Lock()
		closing := s.closing
		s.mu.Unlock()
		if !closing {
			waitErr = fmt.Errorf("interactive session: %w", err)
		}
	default:
		// Failed to run or reap — includes exec.ErrWaitDelay, i.e. a wedged child
		// whose pipes had to be force-closed. Surface it, but still hand back
		// whatever output was captured.
		waitErr = fmt.Errorf("interactive session: %w", err)
	}
	result := execResultFrom(exitCode, s.stdout, s.stderr)

	s.mu.Lock()
	s.result = result
	s.waitErr = waitErr
	s.exitCode = exitCode
	s.closed = true // stdin is gone once the process is reaped
	s.mu.Unlock()

	close(s.done)
	// Release the ctx we derived in ExecInteractive; harmless if already cancelled.
	s.cancel()
}

// WriteLine sends one line to the process's stdin, appending a newline if s does
// not already end with one. Safe to call from any goroutine.
func (s *interactiveSession) WriteLine(line string) error {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSessionClosed
	}
	// The process can exit at any moment; check before touching the pipe so the
	// common case is a clear error rather than an EPIPE.
	select {
	case <-s.done:
		return ErrSessionClosed
	default:
	}

	if _, err := io.WriteString(s.stdin, line); err != nil {
		// Lost the race with process exit (EPIPE / already-closed pipe). Report it
		// as a closed session, keeping the error kind stable for callers.
		return fmt.Errorf("%w: %v", ErrSessionClosed, err)
	}
	if s.touch != nil {
		s.touch()
	}
	return nil
}

// Wait blocks until the process exits and returns its result. Re-entrant: every
// caller gets the same result.
func (s *interactiveSession) Wait() (*ExecResult, error) {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, s.waitErr
}

// Close closes stdin, kills the process, and waits (bounded) for it to be reaped.
// Idempotent and safe to call concurrently with Wait.
func (s *interactiveSession) Close() error {
	s.mu.Lock()
	s.closed = true
	s.closing = true
	stdin := s.stdin
	s.mu.Unlock()

	// Closing stdin is the graceful signal for a stream-json CLI: it finishes the
	// current turn and exits on its own. sync.Once keeps a second Close from
	// double-closing the pipe.
	s.stdinOnce.Do(func() {
		if stdin != nil {
			_ = stdin.Close()
		}
	})

	// Give it a bounded moment to ACT on that EOF. Cancelling immediately would
	// make the graceful signal above meaningless — verified against a real
	// `docker exec -i`, where an immediate cancel killed a process that would
	// otherwise have exited 0. For a stream-json CLI that difference matters: a
	// SIGKILL mid-flush can lose the final `result` event, which surfaces to the
	// user as "finished but returned no result".
	select {
	case <-s.done:
		return nil
	case <-time.After(interactiveEOFGrace):
	}

	// It ignored EOF — a CLI that won't leave must not outlive the session.
	s.cancel()

	select {
	case <-s.done:
		return nil
	case <-time.After(interactiveCloseGrace):
		// WaitDelay should have made this unreachable; returning beats blocking a
		// shutdown path forever.
		return fmt.Errorf("interactive session: process not reaped within %s of close", interactiveCloseGrace)
	}
}

// Done is closed once the process has exited.
func (s *interactiveSession) Done() <-chan struct{} { return s.done }

// ExitCode reports the process's exit status; valid only after Done is closed.
func (s *interactiveSession) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

// flushIfSet flushes a lineWriter that may be nil (no streaming callback given).
func (w *lineWriter) flushIfSet() {
	if w != nil {
		w.flush()
	}
}
