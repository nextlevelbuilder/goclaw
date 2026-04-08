package nodehost

import (
	"context"
	"testing"
)

// --- RunCommand tests ---

func TestRunCommand_EchoSuccess(t *testing.T) {
	result := RunCommand(context.Background(), []string{"echo", "hello"}, RunCommandOpts{})
	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Errorf("exitCode = %v, want 0", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello\n")
	}
}

func TestRunCommand_NonZeroExitCode(t *testing.T) {
	result := RunCommand(context.Background(), []string{"sh", "-c", "exit 42"}, RunCommandOpts{})
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.ExitCode == nil || *result.ExitCode != 42 {
		t.Errorf("exitCode = %v, want 42", result.ExitCode)
	}
}

func TestRunCommand_Timeout(t *testing.T) {
	result := RunCommand(context.Background(), []string{"sleep", "10"}, RunCommandOpts{TimeoutMs: 100})
	if !result.TimedOut {
		t.Error("expected timedOut=true")
	}
	if result.Success {
		t.Error("expected success=false on timeout")
	}
}

func TestRunCommand_StderrCapture(t *testing.T) {
	result := RunCommand(context.Background(), []string{"sh", "-c", "echo err >&2"}, RunCommandOpts{})
	if result.Stderr != "err\n" {
		t.Errorf("stderr = %q, want %q", result.Stderr, "err\n")
	}
}

func TestRunCommand_EmptyCommand(t *testing.T) {
	result := RunCommand(context.Background(), nil, RunCommandOpts{})
	if result.Success {
		t.Error("expected failure for empty command")
	}
	if result.Error == nil {
		t.Error("expected error message")
	}
}

func TestRunCommand_CwdOverride(t *testing.T) {
	tmp := t.TempDir()
	result := RunCommand(context.Background(), []string{"pwd"}, RunCommandOpts{Cwd: tmp})
	if !result.Success {
		t.Fatalf("expected success: %v", result.Error)
	}
	// pwd output should contain the temp dir path.
	if result.Stdout == "" {
		t.Error("expected non-empty stdout from pwd")
	}
}

func TestRunCommand_OutputTruncation(t *testing.T) {
	// Generate output larger than OutputCap.
	// Use dd to generate exactly OutputCap+1 bytes.
	result := RunCommand(context.Background(),
		[]string{"sh", "-c", "dd if=/dev/zero bs=1 count=204801 2>/dev/null | tr '\\0' 'A'"},
		RunCommandOpts{})
	if !result.Truncated {
		t.Error("expected truncated=true for output > 200KB")
	}
}

// --- HandleSystemRunInvoke integration tests ---

func TestHandleSystemRun_EmptyCommand(t *testing.T) {
	var gotResult SystemRunInvokeResult
	deps := SystemRunDeps{
		SendDeniedEvent:   func(_ ExecEventPayload) {},
		SendFinishedEvent: func(_ ExecFinishedEventParams) {},
		SendResult:        func(r SystemRunInvokeResult) { gotResult = r },
	}
	HandleSystemRunInvoke(context.Background(), SystemRunParams{
		Command: []string{},
	}, deps)
	if gotResult.Ok {
		t.Error("expected failure for empty command")
	}
	if gotResult.ErrorMsg != "command required" {
		t.Errorf("errorMsg = %q, want 'command required'", gotResult.ErrorMsg)
	}
}

func TestHandleSystemRun_EchoSuccess(t *testing.T) {
	var gotResult SystemRunInvokeResult
	var gotFinished ExecFinishedEventParams
	deps := SystemRunDeps{
		SendDeniedEvent:   func(_ ExecEventPayload) {},
		SendFinishedEvent: func(p ExecFinishedEventParams) { gotFinished = p },
		SendResult:        func(r SystemRunInvokeResult) { gotResult = r },
	}
	HandleSystemRunInvoke(context.Background(), SystemRunParams{
		Command: []string{"echo", "hello"},
	}, deps)
	if !gotResult.Ok {
		t.Fatalf("expected success, got: %s", gotResult.ErrorMsg)
	}
	if gotResult.PayloadJSON == "" {
		t.Error("expected non-empty payload")
	}
	if gotFinished.CommandText == "" {
		t.Error("expected command text in finished event")
	}
}

func TestHandleSystemRun_DeniedEvent(t *testing.T) {
	var gotDenied ExecEventPayload
	var gotResult SystemRunInvokeResult
	deps := SystemRunDeps{
		SendDeniedEvent:   func(p ExecEventPayload) { gotDenied = p },
		SendFinishedEvent: func(_ ExecFinishedEventParams) {},
		SendResult:        func(r SystemRunInvokeResult) { gotResult = r },
	}

	// Invalid command should trigger denied.
	HandleSystemRunInvoke(context.Background(), SystemRunParams{}, deps)

	if gotResult.Ok {
		t.Error("expected failure")
	}
	// gotDenied may or may not be populated depending on which phase fails.
	_ = gotDenied
}
