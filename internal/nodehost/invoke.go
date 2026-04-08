package nodehost

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// OutputCap is the maximum bytes captured across both stdout and stderr combined.
// Matches the TS value of 200_000 (not 200*1024).
const OutputCap = 200_000

// RunCommandOpts holds options for process execution.
type RunCommandOpts struct {
	Cwd       string
	Env       []string // os.Environ() format: "KEY=VALUE"
	TimeoutMs int
}

// RunCommand spawns a process and captures output with capping and timeout.
func RunCommand(ctx context.Context, argv []string, opts RunCommandOpts) *RunResult {
	if len(argv) == 0 {
		errMsg := "empty command"
		return &RunResult{Error: &errMsg, Success: false}
	}

	// Apply timeout if specified.
	if opts.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		errMsg := err.Error()
		return &RunResult{Error: &errMsg, Success: false}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		errMsg := err.Error()
		return &RunResult{Error: &errMsg, Success: false}
	}

	if err := cmd.Start(); err != nil {
		errMsg := err.Error()
		return &RunResult{Error: &errMsg, Success: false}
	}

	// Read stdout and stderr concurrently with a shared combined cap.
	// TS uses a single outputLen counter across both streams.
	stdoutData, _ := io.ReadAll(stdoutPipe)
	stderrData, _ := io.ReadAll(stderrPipe)

	totalLen := len(stdoutData) + len(stderrData)
	truncated := false
	if totalLen > OutputCap {
		truncated = true
		// Trim proportionally, favoring stdout.
		remaining := OutputCap
		if len(stdoutData) > remaining {
			stdoutData = stdoutData[:remaining]
			remaining = 0
		} else {
			remaining -= len(stdoutData)
		}
		if len(stderrData) > remaining {
			stderrData = stderrData[:remaining]
		}
	}

	waitErr := cmd.Wait()
	timedOut := ctx.Err() != nil

	exitCode := resolveExitCode(waitErr, cmd)
	success := exitCode != nil && *exitCode == 0 && !timedOut

	result := &RunResult{
		ExitCode:  exitCode,
		TimedOut:  timedOut,
		Success:   success,
		Stdout:    string(stdoutData),
		Stderr:    string(stderrData),
		Truncated: truncated,
	}
	if waitErr != nil && !timedOut && exitCode == nil {
		errMsg := waitErr.Error()
		result.Error = &errMsg
	}
	return result
}

// resolveExitCode extracts the exit code from a process error.
func resolveExitCode(err error, cmd *exec.Cmd) *int {
	if err == nil {
		code := 0
		return &code
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return &code
	}
	// Try to get exit code from ProcessState.
	if cmd.ProcessState != nil {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			code := status.ExitStatus()
			return &code
		}
	}
	return nil
}

