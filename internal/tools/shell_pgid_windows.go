//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// syscallSIGTERM / syscallSIGKILL are stub values on Windows.
// killProcessGroup ignores the signal and calls cmd.Process.Kill() directly.
const (
	syscallSIGTERM = syscall.Signal(0x0f) // SIGTERM value; unused on Windows path
	syscallSIGKILL = syscall.Signal(0x09) // SIGKILL value; unused on Windows path
)

// setProcessGroup is a no-op on Windows; process groups work differently and
// Setpgid is not supported by the Windows syscall layer.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup falls back to terminating the direct child process on Windows.
// Process-tree kill on Windows requires the Job Objects API; single-process kill
// is sufficient for the current use case (host shell exec, no deep fork trees).
func killProcessGroup(cmd *exec.Cmd, _ syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
