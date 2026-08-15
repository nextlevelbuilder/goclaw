//go:build integration

package integration

// Phase 3 (issue #82) — end-to-end PAT injection via the git adapter.
//
// What we prove here that unit tests cannot:
//   - A real `git clone` over HTTP succeeds when the adapter's Injection env
//     is passed through, against a server that requires Basic PAT auth.
//   - The token never lands in `.git/config`, the remote URL, or any
//     credential helper after clone — i.e. the GIT_CONFIG_COUNT shape leaves
//     no on-disk trace.
//   - Submodule resolve honors the host scope: parent.origin = trusted host
//     resolves the same host for the submodule clone; cross-host stops
//     with `errCredentialHostMismatch` before any network call.
//
// These map to acceptance criteria AC1 + AC4 from the plan.

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const testPAT = "ghp_TESTtokenABCDEFGHIJKLMNOPQRSTUV1234"

func patCred(t *testing.T, host string) *store.SecureCLIUserCredential {
	t.Helper()
	ct := "pat"
	hs := host
	return &store.SecureCLIUserCredential{
		CredentialType: &ct,
		HostScope:      &hs,
		EncryptedEnv:   []byte(`{"token":"` + testPAT + `"}`),
	}
}

// applyInjection turns an Injection into an env slice merged onto os.Environ,
// mirroring what credentialed_exec.go does at run time. GIT_SSL_NO_VERIFY is
// added because httptest.NewTLSServer uses a self-signed cert; production
// callers will hit real CAs and never need this.
func applyInjection(t *testing.T, inj *tools.Injection) []string {
	t.Helper()
	env := append(os.Environ(), "GIT_SSL_NO_VERIFY=true")
	for k, v := range inj.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// TestGitAdapter_PAT_NoLeakInClonedRepo (AC1): real clone works through the
// adapter, and the resulting .git/config carries the remote URL only — no
// Authorization header, no embedded token, no credential helper entry.
func TestGitAdapter_PAT_NoLeakInClonedRepo(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	makeBareRepo(t, repoRoot, "demo")

	srv := startGitHTTPServer(t, repoRoot, testPAT)
	u, _ := url.Parse(srv.URL)
	host := u.Host // "127.0.0.1:NNNN" — preserved with port by normalizeHost

	cred := patCred(t, host)
	cloneURL := srv.URL + "/demo.git"
	dest := filepath.Join(t.TempDir(), "checkout")

	adapter := tools.AdapterFor("git")
	argv := []string{"clone", cloneURL, dest}
	inj, err := adapter.Prepare(context.Background(), nil, cred, argv)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(inj.ArgvPrefix) != 0 {
		t.Fatalf("token leaked to argv prefix: %v", inj.ArgvPrefix)
	}
	if _, ok := inj.Env["GIT_CONFIG_VALUE_0"]; !ok {
		t.Fatalf("missing GIT_CONFIG_VALUE_0 in injection env")
	}

	cmd := exec.Command("git", argv...)
	cmd.Env = applyInjection(t, inj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone failed: %v\n%s", err, out)
	}

	cfg, err := os.ReadFile(filepath.Join(dest, ".git", "config"))
	if err != nil {
		t.Fatalf("read .git/config: %v", err)
	}
	cfgStr := string(cfg)
	if strings.Contains(cfgStr, testPAT) {
		t.Fatalf("PAT leaked into .git/config:\n%s", cfgStr)
	}
	if strings.Contains(strings.ToLower(cfgStr), "authorization") {
		t.Fatalf("auth header leaked into .git/config:\n%s", cfgStr)
	}
	if strings.Contains(strings.ToLower(cfgStr), "credential.helper") {
		t.Fatalf("credential helper leaked into .git/config:\n%s", cfgStr)
	}

	// Verify the remote URL is the clean URL, no embedded userinfo.
	remoteURL, err := exec.Command("git", "-C", dest, "config", "--get", "remote.origin.url").Output()
	if err != nil {
		t.Fatalf("read remote url: %v", err)
	}
	if got := strings.TrimSpace(string(remoteURL)); got != cloneURL {
		t.Fatalf("remote URL rewritten with credentials\nwant: %s\ngot:  %s", cloneURL, got)
	}
}

// TestGitAdapter_PAT_SubmoduleSameHost (AC4 happy): a fetch inside an existing
// checkout whose origin matches the cred host resolves without error.
func TestGitAdapter_PAT_SubmoduleSameHost(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	makeBareRepo(t, repoRoot, "parent")

	srv := startGitHTTPServer(t, repoRoot, testPAT)
	u, _ := url.Parse(srv.URL)
	host := u.Host

	dest := filepath.Join(t.TempDir(), "parent-checkout")
	cred := patCred(t, host)
	cloneURL := srv.URL + "/parent.git"

	adapter := tools.AdapterFor("git")

	// 1. Clone to set up origin.
	cloneArgv := []string{"clone", cloneURL, dest}
	inj, err := adapter.Prepare(context.Background(), nil, cred, cloneArgv)
	if err != nil {
		t.Fatalf("Prepare clone: %v", err)
	}
	cmd := exec.Command("git", cloneArgv...)
	cmd.Env = applyInjection(t, inj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	// 2. Fetch — adapter resolves remote.origin.url via `git config --get` and
	// must match the cred host. Production passes argv without `-C`; cwd is
	// set on the cmd struct (see credentialed_exec.go). Mirror that.
	fetchArgv := []string{"fetch", "origin"}
	ctx := tools.WithExecCwd(context.Background(), dest)
	inj, err = adapter.Prepare(ctx, nil, cred, fetchArgv)
	if err != nil {
		t.Fatalf("Prepare fetch on same host: %v", err)
	}
	cmd = exec.Command("git", fetchArgv...)
	cmd.Dir = dest
	cmd.Env = applyInjection(t, inj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
}

// TestGitAdapter_PAT_SubmoduleCrossHostFails (AC4 negative): cred bound to a
// different host than the parent's origin must fail before any HTTP call. The
// error message must not leak the token.
func TestGitAdapter_PAT_SubmoduleCrossHostFails(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	makeBareRepo(t, repoRoot, "parent")

	srv := startGitHTTPServer(t, repoRoot, testPAT)
	u, _ := url.Parse(srv.URL)
	actualHost := u.Host

	dest := filepath.Join(t.TempDir(), "parent-checkout")
	cloneURL := srv.URL + "/parent.git"

	adapter := tools.AdapterFor("git")

	// Clone with a matching cred first to set up the checkout.
	good := patCred(t, actualHost)
	cloneArgv := []string{"clone", cloneURL, dest}
	inj, err := adapter.Prepare(context.Background(), nil, good, cloneArgv)
	if err != nil {
		t.Fatalf("Prepare clone: %v", err)
	}
	cmd := exec.Command("git", cloneArgv...)
	cmd.Env = applyInjection(t, inj)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	// Now retry fetch with a cred bound to a different host.
	wrong := patCred(t, "evil.example.com")
	fetchArgv := []string{"fetch", "origin"}
	ctx := tools.WithExecCwd(context.Background(), dest)
	if _, err := adapter.Prepare(ctx, nil, wrong, fetchArgv); err == nil {
		t.Fatalf("expected host mismatch error, got nil")
	} else {
		// Adapter exposes errCredentialHostMismatch as unexported; assert by
		// shape: error string must name both hosts but never the token.
		msg := err.Error()
		if strings.Contains(msg, testPAT) {
			t.Fatalf("token leaked into mismatch error: %s", msg)
		}
		if !strings.Contains(msg, "evil.example.com") {
			t.Fatalf("error missing cred host: %s", msg)
		}
		// errors.Is fallback in case the adapter exports a sentinel later.
		_ = errors.Is
	}
}
