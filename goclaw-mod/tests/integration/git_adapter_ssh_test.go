//go:build integration

package integration

// Phase 4 (issue #82) — SSH path end-to-end lifecycle test.
//
// Why no real sshd: spawning an OpenSSH daemon in CI is heavy (image must
// carry openssh-server) and the lifecycle properties we care about
// (tmpfile 0600, cleanup-on-success, cleanup-on-exec-failure, env shape
// reaching the child) are observable with a fake exec that just reads
// GIT_SSH_COMMAND and reports back. Real sshd is reserved for manual
// security review per Phase 6.
//
// This file proves:
//   - The keypath surfaced via Injection.ScrubValues[0] is a real 0600
//     tmpfile on disk while the exec runs.
//   - cleanup() removes it. The file is gone after cleanup, regardless of
//     exec success/failure.
//   - GIT_SSH_COMMAND propagates through the env passed to the child.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func makeTestKeyJSON(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(block.Bytes)
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\n" + encoded + "\n-----END OPENSSH PRIVATE KEY-----\n"
	blob, _ := json.Marshal(map[string]string{"key": pem})
	return blob
}

func sshCredFor(t *testing.T, host string) *store.SecureCLIUserCredential {
	t.Helper()
	ct, hs := "ssh_key", host
	return &store.SecureCLIUserCredential{
		CredentialType: &ct,
		HostScope:      &hs,
		EncryptedEnv:   makeTestKeyJSON(t),
	}
}

// TestGitAdapter_SSH_TmpfileLifecycle (AC2): keypath exists during exec, is
// 0600, and is removed after cleanup runs.
func TestGitAdapter_SSH_TmpfileLifecycle(t *testing.T) {
	t.Parallel()

	cred := sshCredFor(t, "github.com")
	adapter := tools.AdapterFor("git")

	inj, err := adapter.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@github.com:o/r.git", "/tmp/dst"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() {
		if inj.Cleanup != nil {
			_ = inj.Cleanup()
		}
	}()

	if len(inj.ScrubValues) != 1 {
		t.Fatalf("want 1 scrub value (keypath), got %v", inj.ScrubValues)
	}
	keyPath := inj.ScrubValues[0]

	// Exists + 0600 (POSIX only) during the exec window.
	st, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("keypath stat: %v", err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("keypath perms = %o, want 0600", st.Mode().Perm())
	}

	// Simulate the exec: spawn `env` (or `cmd /c set` on Windows) to
	// observe GIT_SSH_COMMAND propagation. We don't run real git — the
	// adapter's contract is "env reaches child", which is provable without
	// involving git's transport layer.
	env := append(os.Environ())
	for k, v := range inj.Env {
		env = append(env, k+"="+v)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "set")
	} else {
		cmd = exec.Command("env")
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("env exec: %v", err)
	}
	if !strings.Contains(string(out), "GIT_SSH_COMMAND=ssh -i "+keyPath) {
		t.Fatalf("GIT_SSH_COMMAND not propagated to child env:\n%s", out)
	}

	// Cleanup → file gone.
	if err := inj.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("post-cleanup stat want ErrNotExist, got %v", err)
	}
}

// TestGitAdapter_SSH_CleanupOnExecFailure: cleanup runs even when the
// caller's exec exits non-zero. Mirrors what credentialed_exec.go does via
// `defer inj.Cleanup()`.
func TestGitAdapter_SSH_CleanupOnExecFailure(t *testing.T) {
	t.Parallel()

	cred := sshCredFor(t, "github.com")
	adapter := tools.AdapterFor("git")

	inj, err := adapter.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@github.com:o/r.git", "/tmp/dst"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	keyPath := inj.ScrubValues[0]

	// Mimic credentialed_exec.go: defer cleanup, run command, observe exit.
	func() {
		defer inj.Cleanup()
		// Force a non-zero exit.
		cmd := exec.Command(falseBin(), "anyarg")
		_ = cmd.Run() // err expected, ignored
	}()

	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("post-failure cleanup did not remove keypath; stat err = %v", err)
	}
}

// TestGitAdapter_SSH_HostMismatch_NoTmpfile: rejected host mismatch must
// NOT leave an orphaned tmpfile in os.TempDir().
func TestGitAdapter_SSH_HostMismatch_NoTmpfile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	cred := sshCredFor(t, "github.com")
	adapter := tools.AdapterFor("git")

	// This assertion observes os.TempDir(), which is process-global. Keep it
	// serial so other SSH lifecycle tests cannot create legitimate temp keys
	// between the before/after snapshots.
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "goclaw-gitkey-*"))
	_, err := adapter.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@gitlab.com:o/r.git"})
	if err == nil {
		t.Fatal("expected host mismatch error")
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "goclaw-gitkey-*"))
	if len(after) > len(before) {
		t.Fatalf("orphaned tmpfile after rejected Prepare: before=%d after=%d new=%v",
			len(before), len(after), after)
	}
}

func falseBin() string {
	if runtime.GOOS == "windows" {
		// cmd.exe /c exit 1 — but `exit` is a builtin, use a guaranteed-fail call.
		return "cmd"
	}
	return "false"
}
