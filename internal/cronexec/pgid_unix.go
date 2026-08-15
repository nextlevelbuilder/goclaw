//go:build !windows

package cronexec

import (
	"os/exec"
	"syscall"
)

// setProcessGroup makes the child its own process-group leader so signalGroup
// can reach the whole tree (shell + forked children) with one kill(2).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends SIGTERM (or SIGKILL when kill is true) to the process group
// rooted at cmd.Process.Pid. Because Setpgid was set, pgid == pid, so kill(-pid)
// reaches every forked child.
func signalGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}
