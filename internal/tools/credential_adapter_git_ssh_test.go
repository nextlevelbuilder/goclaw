package tools

// Phase 4 unit tests for gitAdapter's ssh_key branch and ValidateSSHKey.
// Integration coverage (real exec, tmpfile lifecycle under a child process)
// lives in tests/integration/git_adapter_ssh_test.go.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// genEd25519PEM returns an unencrypted OpenSSH PEM-encoded ed25519 private key.
func genEd25519PEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 gen: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		base64Encode(block.Bytes) +
		"\n-----END OPENSSH PRIVATE KEY-----\n")
}

// genEd25519PEMWithPassphrase returns a passphrase-encrypted ed25519 key.
func genEd25519PEMWithPassphrase(t *testing.T, pw string) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 gen: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(pw))
	if err != nil {
		t.Fatalf("marshal encrypted key: %v", err)
	}
	return []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		base64Encode(block.Bytes) +
		"\n-----END OPENSSH PRIVATE KEY-----\n")
}

// base64Encode wraps stdlib base64 at 70 cols to mimic real OpenSSH PEM.
func base64Encode(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	var out strings.Builder
	for i := 0; i < len(s); i += 70 {
		end := i + 70
		if end > len(s) {
			end = len(s)
		}
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s[i:end])
	}
	return out.String()
}

// 1. AC5: passphrase keys rejected via sentinel.
func TestValidateSSHKey_RejectsPassphrase(t *testing.T) {
	pem := genEd25519PEMWithPassphrase(t, "topsecret")
	err := ValidateSSHKey(pem)
	if !errors.Is(err, ErrSSHKeyPassphraseUnsupported) {
		t.Fatalf("want ErrSSHKeyPassphraseUnsupported, got %v", err)
	}
}

// 2. Plain ed25519 passes.
func TestValidateSSHKey_AcceptsUnencrypted(t *testing.T) {
	if err := ValidateSSHKey(genEd25519PEM(t)); err != nil {
		t.Fatalf("ed25519 should pass: %v", err)
	}
}

// 3. Garbage rejected without exposing as passphrase.
func TestValidateSSHKey_RejectsGarbage(t *testing.T) {
	err := ValidateSSHKey([]byte("not a key"))
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrSSHKeyPassphraseUnsupported) {
		t.Fatal("garbage must not be classified as passphrase-protected")
	}
}

// 4. Empty rejected.
func TestValidateSSHKey_RejectsEmpty(t *testing.T) {
	if err := ValidateSSHKey(nil); err == nil {
		t.Fatal("empty must error")
	}
}

// 5. Prepare(ssh_key) shape: env, options, scrub, cleanup.
func TestGitAdapter_PrepareSSH(t *testing.T) {
	pem := genEd25519PEM(t)
	blob, _ := json.Marshal(map[string]string{"key": string(pem)})
	ct, hs := "ssh_key", "github.com"
	cred := &store.SecureCLIUserCredential{
		CredentialType: &ct,
		HostScope:      &hs,
		EncryptedEnv:   blob,
	}

	a := gitAdapter{}
	inj, err := a.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@github.com:o/r.git", "/tmp/dst"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(inj.ArgvPrefix) != 0 {
		t.Fatalf("argv leak: %v", inj.ArgvPrefix)
	}
	gsc := inj.Env["GIT_SSH_COMMAND"]
	if !strings.HasPrefix(gsc, "ssh -i ") {
		t.Fatalf("GIT_SSH_COMMAND prefix wrong: %q", gsc)
	}
	for _, need := range []string{
		"-o IdentitiesOnly=yes",
		"-o BatchMode=yes",
		"-o StrictHostKeyChecking=accept-new",
	} {
		if !strings.Contains(gsc, need) {
			t.Fatalf("GIT_SSH_COMMAND missing %q\nfull: %s", need, gsc)
		}
	}
	// Extract keypath: token after "-i ".
	rest := strings.TrimPrefix(gsc, "ssh -i ")
	keyPath := strings.SplitN(rest, " ", 2)[0]
	if !strings.Contains(keyPath, "goclaw-gitkey-") {
		t.Fatalf("keypath not in expected tmp prefix: %s", keyPath)
	}
	if !strings.HasPrefix(keyPath, os.TempDir()) {
		t.Fatalf("keypath not under TempDir(%s): %s", os.TempDir(), keyPath)
	}
	// 0600 perms (POSIX only).
	if runtime.GOOS != "windows" {
		st, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("stat keypath: %v", err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("keypath perms = %o, want 0600", st.Mode().Perm())
		}
	}
	if len(inj.ScrubValues) != 1 || inj.ScrubValues[0] != keyPath {
		t.Fatalf("ScrubValues = %v, want [%s]", inj.ScrubValues, keyPath)
	}
	if inj.Cleanup == nil {
		t.Fatal("Cleanup must be non-nil")
	}
	// File present pre-cleanup, gone after.
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("pre-cleanup stat: %v", err)
	}
	if err := inj.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(keyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("post-cleanup stat want ErrNotExist, got: %v", err)
	}
}

// 6. SSH path also enforces host match: scp-form mismatch.
func TestGitAdapter_PrepareSSH_HostMismatchSCPForm(t *testing.T) {
	pem := genEd25519PEM(t)
	blob, _ := json.Marshal(map[string]string{"key": string(pem)})
	ct, hs := "ssh_key", "github.com"
	cred := &store.SecureCLIUserCredential{
		CredentialType: &ct,
		HostScope:      &hs,
		EncryptedEnv:   blob,
	}
	a := gitAdapter{}
	_, err := a.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@gitlab.com:o/r.git"})
	if err == nil {
		t.Fatal("expected host mismatch")
	}
	var mismatch *errCredentialHostMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("want *errCredentialHostMismatch, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), string(pem)) {
		t.Fatal("key PEM leaked into error message")
	}
}

// 7. Cleanup is idempotent — Phase 2b lockstep test.
func TestGitAdapter_PrepareSSH_CleanupIdempotent(t *testing.T) {
	pem := genEd25519PEM(t)
	blob, _ := json.Marshal(map[string]string{"key": string(pem)})
	ct, hs := "ssh_key", "github.com"
	cred := &store.SecureCLIUserCredential{
		CredentialType: &ct,
		HostScope:      &hs,
		EncryptedEnv:   blob,
	}
	a := gitAdapter{}
	inj, err := a.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@github.com:o/r.git"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := inj.Cleanup(); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	if err := inj.Cleanup(); err != nil {
		t.Fatalf("second cleanup must be nil, got %v", err)
	}
}

// 8. Blob missing 'key' field rejected before any tmpfile is created.
func TestGitAdapter_PrepareSSH_RejectsMalformedBlob(t *testing.T) {
	ct, hs := "ssh_key", "github.com"
	cred := &store.SecureCLIUserCredential{
		CredentialType: &ct,
		HostScope:      &hs,
		EncryptedEnv:   []byte(`{"private_key":"-----BEGIN..."}`),
	}
	a := gitAdapter{}
	_, err := a.Prepare(context.Background(), nil, cred,
		[]string{"clone", "git@github.com:o/r.git"})
	if err == nil {
		t.Fatal("expected error for missing 'key' field")
	}
}
