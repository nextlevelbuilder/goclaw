//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// syscallSIGTERM and syscallSIGKILL are the platform signals used by executeOnHost.
// Defined here (unix) and as zero-value stubs in shell_pgid_windows.go.
const (
	syscallSIGTERM = syscall.SIGTERM
	syscallSIGKILL = syscall.SIGKILL
)

// setProcessGroup sets Setpgid so the child process becomes its own process-group leader.
// This allows killProcessGroup to signal the entire process tree (shell + forked children).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends sig to the entire process group rooted at cmd.Process.Pid.
// Because Setpgid was set, pgid == pid, so kill(-pid, sig) reaches all forked children.
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}
