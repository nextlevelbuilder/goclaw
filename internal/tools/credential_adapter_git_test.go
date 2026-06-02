package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func gitAdapterInstance(t *testing.T) CredentialAdapter {
	t.Helper()
	a := AdapterFor("git")
	if a.Name() != "git" {
		t.Fatalf("git adapter not registered; AdapterFor(git)=%q", a.Name())
	}
	return a
}

// 1. Sniffing — locks AC8
func TestGitAdapter_ShouldInject(t *testing.T) {
	a := gitAdapterInstance(t)
	yes := []string{"clone", "fetch", "pull", "push", "submodule"}
	no := []string{"status", "log", "diff", "commit", "init", "config", "--version", ""}
	for _, sub := range yes {
		if !a.ShouldInject([]string{sub, "x"}) {
			t.Errorf("ShouldInject(%q) = false, want true", sub)
		}
	}
	for _, sub := range no {
		if a.ShouldInject([]string{sub}) {
			t.Errorf("ShouldInject(%q) = true, want false", sub)
		}
	}
	if a.ShouldInject(nil) {
		t.Errorf("ShouldInject(nil) = true, want false")
	}
}

// 2. Host parsing
func TestParseHostFromGitURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
		err  bool
	}{
		{"https://github.com/o/r.git", "github.com", false},
		{"https://gitea.example.com:8443/o/r.git", "gitea.example.com:8443", false},
		{"git@github.com:o/r.git", "github.com", false},
		{"ssh://git@gitlab.com:2222/o/r.git", "gitlab.com:2222", false},
		{"ssh://git@gitlab.com/o/r.git", "gitlab.com", false},
		{"", "", true},
		{"not-a-url", "", true},
		{"http://", "", true},
	}
	for _, tc := range tests {
		got, err := parseHostFromGitURL(tc.url)
		if tc.err {
			if err == nil {
				t.Errorf("parseHostFromGitURL(%q) err=nil, want err", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseHostFromGitURL(%q) err=%v", tc.url, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseHostFromGitURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// 3. Clone host resolution
func TestResolveTargetHost_Clone(t *testing.T) {
	host, err := resolveTargetHost(context.Background(), []string{"clone", "--depth=1", "https://github.com/o/r.git"}, "")
	if err != nil || host != "github.com" {
		t.Fatalf("clone with URL: host=%q err=%v, want github.com nil", host, err)
	}
	if _, err := resolveTargetHost(context.Background(), []string{"clone", "--depth=1"}, ""); err == nil {
		t.Fatalf("clone with no URL: err=nil, want err")
	}
}

// 4. fetch/pull/push host resolution via fake git stub
func TestResolveTargetHost_RemoteName(t *testing.T) {
	tmp := t.TempDir()
	// Fake git script: outputs "https://stub.example.com/o/r.git" for any config --get remote.<x>.url
	stub := filepath.Join(tmp, "git")
	script := "#!/bin/sh\necho https://stub.example.com/o/r.git\n"
	if runtime.GOOS == "windows" {
		t.Skip("shell script stub not supported on windows")
	}
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Prepend tmp to PATH so `git` resolves to our stub.
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	os.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

	cases := [][]string{
		{"fetch"},
		{"fetch", "origin"},
		{"pull", "upstream"},
		{"push", "origin", "main"},
		{"push", "--repo=mirror"},
		{"submodule", "update", "--init"},
	}
	for _, argv := range cases {
		host, err := resolveTargetHost(context.Background(), argv, tmp)
		if err != nil {
			t.Errorf("resolveTargetHost(%v) err=%v", argv, err)
			continue
		}
		if host != "stub.example.com" {
			t.Errorf("resolveTargetHost(%v) host=%q, want stub.example.com", argv, host)
		}
	}
}

// 5. Host mismatch — AC3
func TestGitAdapter_HostMismatch(t *testing.T) {
	a := gitAdapterInstance(t)
	credType := "pat"
	hostScope := "github.com"
	blob, _ := json.Marshal(map[string]string{"token": "ghp_secrettoken12345"})
	cred := &store.SecureCLIUserCredential{
		CredentialType: &credType,
		HostScope:      &hostScope,
		EncryptedEnv:   blob,
	}
	_, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://gitlab.com/o/r.git"})
	if err == nil {
		t.Fatalf("expected host mismatch error")
	}
	var mm *errCredentialHostMismatch
	if !errors.As(err, &mm) {
		t.Fatalf("err=%v (%T), want *errCredentialHostMismatch", err, err)
	}
	if mm.credHost != "github.com" || mm.targetHost != "gitlab.com" {
		t.Errorf("mismatch fields: cred=%q target=%q", mm.credHost, mm.targetHost)
	}
	// Error string must not leak token.
	if strings.Contains(err.Error(), "ghp_secrettoken12345") {
		t.Errorf("error leaks token: %v", err)
	}
}

// 6. PAT injection shape — env approach, no argv
func TestGitAdapter_PreparePAT(t *testing.T) {
	a := gitAdapterInstance(t)
	credType := "pat"
	hostScope := "github.com"
	blob, _ := json.Marshal(map[string]string{"token": "ghp_abc"})
	cred := &store.SecureCLIUserCredential{
		CredentialType: &credType,
		HostScope:      &hostScope,
		EncryptedEnv:   blob,
	}
	inj, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://github.com/o/r.git"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if inj == nil {
		t.Fatal("nil Injection")
	}
	if len(inj.ArgvPrefix) != 0 {
		t.Errorf("ArgvPrefix=%v, want empty (PAT goes through env)", inj.ArgvPrefix)
	}
	wantEnv := map[string]string{
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0": "Authorization: Bearer ghp_abc",
	}
	if !reflect.DeepEqual(inj.Env, wantEnv) {
		t.Errorf("Env=%v, want %v", inj.Env, wantEnv)
	}
	if len(inj.ScrubValues) != 1 || inj.ScrubValues[0] != "ghp_abc" {
		t.Errorf("ScrubValues=%v, want [ghp_abc]", inj.ScrubValues)
	}
	if inj.Cleanup != nil {
		t.Errorf("Cleanup must be nil for PAT path")
	}
}

// 8a. CRLF rejection
func TestGitAdapter_PreparePAT_RejectsCRLF(t *testing.T) {
	a := gitAdapterInstance(t)
	credType := "pat"
	hostScope := "github.com"
	blob, _ := json.Marshal(map[string]string{"token": "ghp_abc\r\nX-Injected: evil"})
	cred := &store.SecureCLIUserCredential{
		CredentialType: &credType,
		HostScope:      &hostScope,
		EncryptedEnv:   blob,
	}
	_, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://github.com/o/r.git"})
	if err == nil {
		t.Fatalf("expected CRLF rejection")
	}
	if !errors.Is(err, errTokenControlChar) {
		t.Errorf("err=%v, want errTokenControlChar", err)
	}
}

// 8b. Empty + oversize rejection
func TestGitAdapter_PreparePAT_RejectsEmptyAndOversize(t *testing.T) {
	a := gitAdapterInstance(t)
	credType := "pat"
	hostScope := "github.com"

	emptyBlob, _ := json.Marshal(map[string]string{"token": ""})
	cred := &store.SecureCLIUserCredential{CredentialType: &credType, HostScope: &hostScope, EncryptedEnv: emptyBlob}
	if _, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://github.com/o/r.git"}); !errors.Is(err, errEmptyToken) {
		t.Errorf("empty token: err=%v, want errEmptyToken", err)
	}

	bigBlob, _ := json.Marshal(map[string]string{"token": strings.Repeat("a", 5000)})
	cred.EncryptedEnv = bigBlob
	if _, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://github.com/o/r.git"}); !errors.Is(err, errTokenTooLong) {
		t.Errorf("oversize token: err=%v, want errTokenTooLong", err)
	}
}

// 8c. IDN normalization
func TestNormalizeHost_IDN(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"github.com", "github.com"},
		{"GitHub.Com.", "github.com"},
		{"  GitHub.Com  ", "github.com"},
		{"gitea.example.com:8443", "gitea.example.com:8443"},
	}
	for _, tc := range tests {
		got, err := normalizeHost(tc.in)
		if err != nil {
			t.Errorf("normalizeHost(%q) err=%v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Empty after trim
	if _, err := normalizeHost("  "); err == nil {
		t.Errorf("normalizeHost empty: err=nil, want err")
	}
	// IDN punycode round trip — both forms normalize to the same ASCII output.
	asciiFromUnicode, err := normalizeHost("gitlab.中国")
	if err != nil {
		t.Fatalf("normalizeHost unicode: %v", err)
	}
	asciiFromPuny, err := normalizeHost("gitlab.xn--fiqs8s")
	if err != nil {
		t.Fatalf("normalizeHost puny: %v", err)
	}
	if asciiFromUnicode != asciiFromPuny {
		t.Errorf("unicode=%q puny=%q — must match after normalize", asciiFromUnicode, asciiFromPuny)
	}
}

// 8d. Embedded userinfo rejection
func TestParseHostFromGitURL_RejectsUserinfo(t *testing.T) {
	if _, err := parseHostFromGitURL("https://attacker.com@github.com/o/r.git"); err == nil {
		t.Errorf("expected error for embedded userinfo")
	}
	if _, err := parseHostFromGitURL("https://user:pass@github.com/o/r.git"); err == nil {
		t.Errorf("expected error for userinfo with password")
	}
	// scp-form git@host:path is NOT userinfo (no '@' arrives as URL userinfo); allowed.
	if _, err := parseHostFromGitURL("git@github.com:o/r.git"); err != nil {
		t.Errorf("scp-form should parse: %v", err)
	}
	// ssh:// with git@ is the conventional form — allow because it's the only
	// supported form for SSH and removing it would break Phase 4.
	if _, err := parseHostFromGitURL("ssh://git@gitlab.com/o/r.git"); err != nil {
		t.Errorf("ssh://git@host should parse: %v", err)
	}
}

// 9. Legacy env passthrough
func TestGitAdapter_LegacyEnvCredentialPassthrough(t *testing.T) {
	a := gitAdapterInstance(t)
	// credential_type=NULL → passthrough
	cred := &store.SecureCLIUserCredential{
		EncryptedEnv: []byte(`{"GIT_TOKEN":"x"}`),
	}
	inj, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://github.com/o/r.git"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(inj.Env) != 0 || len(inj.ArgvPrefix) != 0 {
		t.Errorf("legacy env cred must passthrough, got %+v", inj)
	}
	// Explicit credential_type="env" → same.
	envType := "env"
	cred.CredentialType = &envType
	inj, err = a.Prepare(context.Background(), &store.SecureCLIBinary{}, cred,
		[]string{"clone", "https://github.com/o/r.git"})
	if err != nil {
		t.Fatalf("Prepare env: %v", err)
	}
	if len(inj.Env) != 0 {
		t.Errorf("env cred must passthrough, got %+v", inj)
	}
}

// (Phase 4 implements ssh_key; coverage moved to
// credential_adapter_git_ssh_test.go.)

// 13. CVE-2018-17456 regression: malicious .git/config must not execute
// `ext::sh -c …` payload. Our `git config --get` path returns the literal
// URL string for parseHostFromGitURL to reject, NEVER hands it to
// `git remote get-url` (which would evaluate the protocol handler).
func TestResolveTargetHost_RejectsExtProtocolInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /tmp marker; POSIX only")
	}
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Init a minimal repo so `git -C <tmp>` recognizes it.
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tmp, "pwn-marker")
	// Plant malicious config — would execute `touch <marker>` if URL ever passes
	// through git's protocol handler.
	cfg := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = ext::sh -c "touch ` + marker + `"
`
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveTargetHost(context.Background(), []string{"fetch"}, tmp)
	// We expect either an error or a non-host string that parseHostFromGitURL
	// rejects. EITHER way the marker file must NOT exist.
	_ = err
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("CVE-2018-17456 regression: ext::sh payload executed — marker %s exists", marker)
	}
}

// Sanity: ensure the test stub of `git --version` is real (avoids accidental
// stub bleed from the RemoteName test affecting the CVE test).
func TestGit_ConfigGet_UsesRealGit(t *testing.T) {
	out, err := exec.Command("git", "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "git version") {
		t.Skipf("real git unavailable: out=%q err=%v", out, err)
	}
}

// 14. DenyArgs in the `git` preset must block `-c http.…` overrides that
// would shadow the adapter's host-scoped extraheader.
func TestSecureCLI_DenyArgs_BlocksHttpCConfig(t *testing.T) {
	preset, ok := CLIPresets["git"]
	if !ok {
		t.Fatal("git preset missing")
	}
	denyJSON, err := json.Marshal(preset.DenyArgs)
	if err != nil {
		t.Fatal(err)
	}

	mustBlock := [][]string{
		{"-c", "http.extraHeader=Authorization: Bearer evil", "clone", "https://github.com/o/r.git"},
		{"-c", "credential.helper=store", "clone", "https://github.com/o/r.git"},
		{"-c", "core.sshCommand=ssh -i /etc/shadow", "clone", "git@github.com:o/r.git"},
		{"config", "--global", "user.email", "x@y"},
		{"config", "--system", "credential.helper", "store"},
		{"daemon", "--export-all"},
	}
	for _, args := range mustBlock {
		if p := matchesBinaryDeny(args, denyJSON); p == "" {
			t.Errorf("DenyArgs did NOT block %v", args)
		}
	}

	mustAllow := [][]string{
		{"clone", "https://github.com/o/r.git"},
		{"fetch", "origin"},
		{"push", "origin", "main"},
		{"submodule", "update", "--init", "--recursive"},
		{"status"},
		{"config", "user.email"}, // local-only config read; not --global/--system
	}
	for _, args := range mustAllow {
		if p := matchesBinaryDeny(args, denyJSON); p != "" {
			t.Errorf("DenyArgs falsely blocked %v with pattern %q", args, p)
		}
	}
}

// sortedHelperForTest is only here to keep `sort` import used.
var _ = sort.Strings
