package backends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/workstation"
	"golang.org/x/crypto/ssh"
)

// SSHSession wraps a pooled *ssh.Client and satisfies workstation.Session.
// Each Exec call opens a fresh ssh.Session on the same client (ssh.Session is one-shot).
type SSHSession struct {
	id      string
	client  *ssh.Client
	release func()
	wsKey   string
}

// ID returns the session identifier.
func (s *SSHSession) ID() string { return s.id }

// Exec opens a new ssh.Session on the pooled client, runs the command, and returns a Stream.
// The command string is composed from req.Cmd, req.Args, and optional req.CWD prefix.
// Env vars are set via Setenv; when the SSH server rejects Setenv (requires AcceptEnv server config),
// we fall back to prepending "export K=V;" to the command string so vars still reach the process.
func (s *SSHSession) Exec(ctx context.Context, req workstation.ExecRequest) (workstation.Stream, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh[%s]: new session: %w", s.wsKey, err)
	}

	// Attempt Setenv for each env var. OpenSSH rejects Setenv without AcceptEnv server config.
	// For rejected vars, build an "export K=V;" prefix that is prepended to the command string.
	var envPrefixBuilder strings.Builder
	for k, v := range req.Env {
		if setErr := sess.Setenv(k, v); setErr != nil {
			slog.Debug("workstation.ssh_setenv_rejected_using_export_fallback",
				"workstation_key", s.wsKey,
				"key", k,
				"err", setErr,
			)
			// Fallback: prepend as shell export so the var reaches the remote process.
			fmt.Fprintf(&envPrefixBuilder, "export %s=%s; ", shellQuote(k), shellQuote(v))
		}
	}

	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("ssh[%s]: stdout pipe: %w", s.wsKey, err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("ssh[%s]: stderr pipe: %w", s.wsKey, err)
	}

	cmdStr := buildCmdString(req)
	if envPrefixBuilder.Len() > 0 {
		// Prepend rejected-env exports so CLAUDE_CONFIG_DIR and other vars are available.
		cmdStr = envPrefixBuilder.String() + cmdStr
	}
	if err := sess.Start(cmdStr); err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("ssh[%s]: start %q: %w", s.wsKey, cmdStr, err)
	}

	stream := &SSHStream{
		sess:    sess,
		stdout:  stdout,
		stderr:  stderr,
		waitErr: make(chan error, 1),
	}
	// Kick off Wait in background so pipes drain naturally.
	go func() {
		stream.waitErr <- sess.Wait()
	}()

	return stream, nil
}

// Close releases the pooled client reference. After Close the session must not be used.
func (s *SSHSession) Close(_ context.Context) error {
	if s.release != nil {
		s.release()
		s.release = nil
	}
	return nil
}

// buildCmdString composes a shell command string from an ExecRequest.
// CWD is prepended as "cd <cwd> && <cmd> <args>".
// Note: SSH protocol delivers a single string to the remote shell — no true argv.
func buildCmdString(req workstation.ExecRequest) string {
	parts := make([]string, 0, 1+len(req.Args))
	parts = append(parts, shellQuote(req.Cmd))
	for _, a := range req.Args {
		parts = append(parts, shellQuote(a))
	}
	cmd := strings.Join(parts, " ")
	if req.CWD != "" {
		cmd = fmt.Sprintf("cd %s && %s", shellQuote(req.CWD), cmd)
	}
	return cmd
}

// shellQuote wraps a string in single quotes, escaping internal single quotes.
// Prevents trivial shell injection when building the command string.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SSHStream wraps an *ssh.Session and exposes workstation.Stream.
type SSHStream struct {
	sess    *ssh.Session
	stdout  io.Reader
	stderr  io.Reader
	waitErr chan error // receives sess.Wait() result (buffered 1)
}

// Stdout returns the command's standard output reader.
func (s *SSHStream) Stdout() io.Reader { return s.stdout }

// Stderr returns the command's standard error reader.
func (s *SSHStream) Stderr() io.Reader { return s.stderr }

// Wait blocks until the remote command exits and returns its exit code.
// Exit code is extracted from *ssh.ExitError; other errors propagate as-is.
func (s *SSHStream) Wait() (int, error) {
	err := <-s.waitErr
	if err == nil {
		return 0, nil
	}
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus(), nil
	}
	return -1, err
}

// Kill sends SIGKILL to the remote process and closes the underlying session.
func (s *SSHStream) Kill() error {
	// Best-effort signal; server may reject if AllowTcpForwarding is off etc.
	_ = s.sess.Signal(ssh.SIGKILL)
	return s.sess.Close()
}
