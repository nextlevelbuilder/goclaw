//go:build windows

package cronexec

import "os/exec"

// setProcessGroup is a no-op on Windows; process-group semantics differ and we
// fall back to killing the root process directly.
func setProcessGroup(cmd *exec.Cmd) {}

// signalGroup kills the root process. Windows lacks POSIX process-group signals,
// so child processes are not guaranteed to be reaped; cron commands on Windows
// should avoid spawning long-lived background children.
func signalGroup(cmd *exec.Cmd, _ bool) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
