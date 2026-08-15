// Regression tests pinning the success/failure-path output scrubber to the
// per-request bag (AddScrubValuesCtx), not the package-global slice.
//
// Why: adapter ScrubValues are registered into the per-request bag during
// Prepare. If formatCredentialedResult / executeCredentialedSandbox call the
// non-Ctx ScrubCredentials, the bag is ignored — so non-GitHub PATs (GitLab
// `glpat-…`, Bitbucket app passwords, Gitea tokens, Azure DevOps PATs) and
// SSH key tmpfile paths would leak into stdout/stderr returned to the agent.
// Locks AC6 against future regressions.
package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// 1. Success path: adapter ScrubValues registered via bag are stripped from
//    stdout in the final *Result.ForLLM. Uses a sentinel that does NOT match
//    any built-in credentialPatterns regex (no `ghp_`, no `aws_`, no
//    `://user:pass@`) so the bag is the only thing that could redact it.
func TestFormatCredentialedResult_HonorsScrubBag_Success(t *testing.T) {
	ctx := WithScrubBag(context.Background())
	secret := "glpat-NONSENSE_GITLAB_PAT_VALUE_XYZ123" // not matched by global regex
	AddScrubValuesCtx(ctx, secret)

	// Avoid any keyword-style prefix (`token=`, `password:`, `authorization=`, …)
	// so the global credentialPatterns regex doesn't redact this — only the
	// per-request bag can. That's the whole point of this regression test.
	stdout := "cloning into 'repo'…\nremote: rejected by " + secret + " for user\ndone.\n"
	res := formatCredentialedResult("/usr/bin/git", []string{"clone", "x"}, stdout, "", nil, ctx, time.Minute)

	if res == nil {
		t.Fatal("nil result")
	}
	if strings.Contains(res.ForLLM, secret) {
		t.Fatalf("non-GitHub PAT leaked into ForLLM: %q", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder in scrubbed output, got %q", res.ForLLM)
	}
}

// 2. Failure path: same guarantee on the error branch (exit != 0). This is
//    the more dangerous case in practice — error messages often quote the
//    failing URL/header that contains the token.
func TestFormatCredentialedResult_HonorsScrubBag_Failure(t *testing.T) {
	ctx := WithScrubBag(context.Background())
	keyPath := "/tmp/goclaw-gitkey-SENTINEL_PATH_VALUE_xyz789"
	AddScrubValuesCtx(ctx, keyPath)

	stderr := "Load key \"" + keyPath + "\": Permission denied\nfatal: Could not read from remote\n"
	// Synthesize a real *exec.ExitError so the type-assertion branch fires.
	cmd := exec.Command("sh", "-c", "exit 128")
	_ = cmd.Run()
	exitErr := &exec.ExitError{ProcessState: cmd.ProcessState}
	res := formatCredentialedResult("/usr/bin/git", []string{"clone", "x"}, "", stderr, exitErr, ctx, time.Minute)

	if res == nil {
		t.Fatal("nil result")
	}
	if strings.Contains(res.ForLLM, keyPath) {
		t.Fatalf("SSH key tmpfile path leaked into ForLLM error: %q", res.ForLLM)
	}
	if strings.Contains(res.ForUser, keyPath) {
		t.Fatalf("SSH key tmpfile path leaked into ForUser error: %q", res.ForUser)
	}
}

// 3. Negative control: without a bag in ctx, the same secret leaks (proves
//    the test is actually exercising the bag path, not getting silently
//    scrubbed by a global match).
func TestFormatCredentialedResult_NoBag_LeaksNonGlobalSecret(t *testing.T) {
	ctx := context.Background() // no WithScrubBag
	secret := "glpat-NONSENSE_GITLAB_PAT_VALUE_CONTROL"

	// Same evasion as test 1 — no keyword-style prefix, so a global regex
	// match would have to come from the secret value itself (which it doesn't).
	stdout := "remote: rejected by " + secret + " for user\n"
	res := formatCredentialedResult("/usr/bin/git", []string{"clone", "x"}, stdout, "", nil, ctx, time.Minute)

	if !strings.Contains(res.ForLLM, secret) {
		t.Fatalf("control failed — secret was scrubbed without a bag, so test 1's coverage claim is wrong. ForLLM=%q", res.ForLLM)
	}
}

// 4. Sanity: empty ctx + empty bag must not panic and must return the regex
//    pass intact (well-known ghp_… still gets redacted).
func TestFormatCredentialedResult_EmptyBag_StillRunsGlobalRegex(t *testing.T) {
	ctx := WithScrubBag(context.Background())
	// classic GitHub PAT shape — matched by the package's regex pass
	classicPAT := "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	stdout := "token=" + classicPAT + "\n"
	res := formatCredentialedResult("/usr/bin/git", nil, stdout, "", nil, ctx, time.Minute)

	if strings.Contains(res.ForLLM, classicPAT) {
		t.Fatalf("classic PAT not redacted by regex pass: %q", res.ForLLM)
	}
}

// 5. Defensive: confirm the timeout path doesn't surface stderr at all
//    (it returns the timeout marker, not the captured output), so no leak vector.
func TestFormatCredentialedResult_TimeoutPath_NoStderrLeak(t *testing.T) {
	ctx, cancel := context.WithTimeout(WithScrubBag(context.Background()), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // make sure deadline elapsed

	stderr := "remote auth token=ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n"
	res := formatCredentialedResult("/usr/bin/git", nil, "", stderr,
		errExitForTimeoutTest{}, ctx, 1*time.Nanosecond)

	// On timeout the formatter returns a fixed string mentioning the binary,
	// not the captured stderr — so no token in output regardless.
	if strings.Contains(res.ForLLM, "ghp_") {
		t.Fatalf("timeout path leaked captured stderr: %q", res.ForLLM)
	}
}

// errExitForTimeoutTest is a marker error for the timeout-path test. The real
// formatCredentialedResult checks ctx.Err() before unwrapping, so any
// non-ExitError suffices.
type errExitForTimeoutTest struct{}

func (errExitForTimeoutTest) Error() string { return "context deadline exceeded" }
