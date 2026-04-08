package nodehost

import (
	"testing"
)

// --- ApplyOutputTruncation tests ---

func TestApplyOutputTruncation_NotTruncated(t *testing.T) {
	r := &RunResult{Stdout: "hello", Stderr: "", Truncated: false}
	ApplyOutputTruncation(r)
	if r.Stdout != "hello" {
		t.Errorf("stdout should be unchanged: %q", r.Stdout)
	}
}

func TestApplyOutputTruncation_StdoutOnly(t *testing.T) {
	r := &RunResult{Stdout: "output", Stderr: "", Truncated: true}
	ApplyOutputTruncation(r)
	want := "output\n... (truncated)"
	if r.Stdout != want {
		t.Errorf("stdout = %q, want %q", r.Stdout, want)
	}
}

func TestApplyOutputTruncation_StderrPreferred(t *testing.T) {
	r := &RunResult{Stdout: "out", Stderr: "err", Truncated: true}
	ApplyOutputTruncation(r)
	wantStderr := "err\n... (truncated)"
	if r.Stderr != wantStderr {
		t.Errorf("stderr = %q, want %q", r.Stderr, wantStderr)
	}
	if r.Stdout != "out" {
		t.Errorf("stdout should be unchanged: %q", r.Stdout)
	}
}

func TestApplyOutputTruncation_WhitespaceOnlyStderr(t *testing.T) {
	r := &RunResult{Stdout: "out", Stderr: "  \n\t ", Truncated: true}
	ApplyOutputTruncation(r)
	// Whitespace-only stderr → suffix goes to stdout.
	want := "out\n... (truncated)"
	if r.Stdout != want {
		t.Errorf("stdout = %q, want %q", r.Stdout, want)
	}
}

// --- ResolvePlannedAllowlistArgv tests ---

func TestResolvePlannedArgv_ReturnsNilWhenNotAllowlist(t *testing.T) {
	got := ResolvePlannedAllowlistArgv(SecurityFull, nil, false, true, true, []ExecCommandSegment{
		{Raw: "echo hi", Argv: []string{"echo", "hi"}},
	})
	if got != nil {
		t.Errorf("expected nil for non-allowlist security, got %v", got)
	}
}

func TestResolvePlannedArgv_ReturnsNilWhenApprovedByAsk(t *testing.T) {
	got := ResolvePlannedAllowlistArgv(SecurityAllowlist, nil, true, true, true, []ExecCommandSegment{
		{Raw: "echo hi", Argv: []string{"echo", "hi"}},
	})
	if got != nil {
		t.Errorf("expected nil when approved by ask, got %v", got)
	}
}

func TestResolvePlannedArgv_ReturnsNilForShellCommand(t *testing.T) {
	shell := "echo hi"
	got := ResolvePlannedAllowlistArgv(SecurityAllowlist, &shell, false, true, true, []ExecCommandSegment{
		{Raw: "echo hi", Argv: []string{"echo", "hi"}},
	})
	if got != nil {
		t.Errorf("expected nil for shell command, got %v", got)
	}
}

func TestResolvePlannedArgv_ReturnsNilForMultipleSegments(t *testing.T) {
	got := ResolvePlannedAllowlistArgv(SecurityAllowlist, nil, false, true, true, []ExecCommandSegment{
		{Raw: "echo hi", Argv: []string{"echo", "hi"}},
		{Raw: "echo bye", Argv: []string{"echo", "bye"}},
	})
	if got != nil {
		t.Errorf("expected nil for multiple segments, got %v", got)
	}
}

func TestResolvePlannedArgv_ReturnsArgv(t *testing.T) {
	got := ResolvePlannedAllowlistArgv(SecurityAllowlist, nil, false, true, true, []ExecCommandSegment{
		{Raw: "echo hi", Argv: []string{"echo", "hi"}},
	})
	if len(got) != 2 || got[0] != "echo" || got[1] != "hi" {
		t.Errorf("expected [echo hi], got %v", got)
	}
}

func TestResolvePlannedArgv_ReturnsNilForEmptyArgv(t *testing.T) {
	got := ResolvePlannedAllowlistArgv(SecurityAllowlist, nil, false, true, true, []ExecCommandSegment{
		{Raw: "", Argv: []string{}},
	})
	if got != nil {
		t.Errorf("expected nil for empty argv, got %v", got)
	}
}

// --- ResolveSystemRunExecArgv tests ---

func TestResolveExecArgv_UsesPlannedArgv(t *testing.T) {
	planned := []string{"planned", "cmd"}
	got := ResolveSystemRunExecArgv(planned, []string{"orig"}, SecurityAllowlist, false, false, true, true, nil, nil)
	if len(got) != 2 || got[0] != "planned" {
		t.Errorf("expected planned argv, got %v", got)
	}
}

func TestResolveExecArgv_FallsBackToArgv(t *testing.T) {
	got := ResolveSystemRunExecArgv(nil, []string{"orig", "cmd"}, SecurityAllowlist, false, false, true, true, nil, nil)
	if len(got) != 2 || got[0] != "orig" {
		t.Errorf("expected original argv, got %v", got)
	}
}

func TestResolveExecArgv_WindowsShellOverride(t *testing.T) {
	shell := "cmd /c echo hi"
	segments := []ExecCommandSegment{{Raw: "echo hi", Argv: []string{"echo", "hi"}}}
	got := ResolveSystemRunExecArgv(nil, []string{"cmd", "/c", "echo", "hi"}, SecurityAllowlist, true, false, true, true, &shell, segments)
	if len(got) != 2 || got[0] != "echo" || got[1] != "hi" {
		t.Errorf("expected segment argv override, got %v", got)
	}
}

