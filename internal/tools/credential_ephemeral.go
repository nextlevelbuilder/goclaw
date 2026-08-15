package tools

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
)

// materializeEphemeral writes content to a per-call 0600 tmpfile and returns a
// cleanup closure that removes it idempotently. The path is safe to pass via
// env vars (PGPASSFILE, KUBECONFIG, GIT_SSH_COMMAND's -i flag) but NOT via
// argv — argv is world-readable on Linux via /proc/<pid>/cmdline.
//
// Memfd is intentionally NOT used. /proc/self/fd/N resolves "self" against the
// calling process; for child processes (and grandchildren like git→ssh) the
// kernel resolves against the child, which sees EBADF unless the parent passed
// the fd via ExecCommand.ExtraFiles AND the child binary actually reads from
// that fd number. git/psql/docker/kubectl have no API to forward fds to their
// subprocesses, so tmpfile + defer remove is the safe, portable default.
//
// Callers MUST `defer cleanup()` — the helper alone is not enough; if the
// caller goroutine panics without firing the defer, the tmpfile leaks until
// reboot (or whatever cleans os.TempDir on the host).
//
// On Windows the explicit Chmod is a no-op (POSIX-only modes). ACL hardening
// is deferred to v2 with a documented limitation.
func materializeEphemeral(_ context.Context, content []byte, prefix string) (string, func() error, error) {
	f, err := os.CreateTemp("", "goclaw-"+prefix+"-*")
	if err != nil {
		return "", nil, fmt.Errorf("create ephemeral file: %w", err)
	}
	name := f.Name()

	// Idempotent cleanup latch — concurrent callers don't double-remove.
	var done atomic.Bool
	cleanup := func() error {
		if done.Swap(true) {
			return nil
		}
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	// Re-chmod 0600 even though CreateTemp already does this on POSIX —
	// explicit + future-proofs if the runtime ever changes the default.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = cleanup()
		return "", nil, fmt.Errorf("chmod ephemeral file: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = cleanup()
		return "", nil, fmt.Errorf("write ephemeral file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("close ephemeral file: %w", err)
	}
	return name, cleanup, nil
}
