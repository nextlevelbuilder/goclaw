package nodehost

import (
	"testing"
)

// buildPolicyParams returns default params matching the TS test helper.
func buildPolicyParams(overrides func(*EvaluateSystemRunPolicyParams)) EvaluateSystemRunPolicyParams {
	p := EvaluateSystemRunPolicyParams{
		Security:               SecurityAllowlist,
		Ask:                    AskOff,
		AnalysisOk:             true,
		AllowlistSatisfied:     true,
		ApprovalDecision:       nil,
		Approved:               false,
		IsWindows:              false,
		CmdInvocation:          false,
		ShellWrapperInvocation: false,
	}
	if overrides != nil {
		overrides(&p)
	}
	return p
}

// --- resolveExecApprovalDecision tests (ported from TS) ---

func TestResolveExecApprovalDecision_AcceptsKnown(t *testing.T) {
	tests := []struct {
		input string
		want  ExecApprovalDecision
	}{
		{"allow-once", ApprovalAllowOnce},
		{"allow-always", ApprovalAllowAlways},
	}
	for _, tt := range tests {
		got := ResolveExecApprovalDecision(tt.input)
		if got == nil {
			t.Errorf("ResolveExecApprovalDecision(%q) = nil, want %q", tt.input, tt.want)
			continue
		}
		if *got != tt.want {
			t.Errorf("ResolveExecApprovalDecision(%q) = %q, want %q", tt.input, *got, tt.want)
		}
	}
}

func TestResolveExecApprovalDecision_NormalizesUnknown(t *testing.T) {
	for _, input := range []string{"deny", ""} {
		got := ResolveExecApprovalDecision(input)
		if got != nil {
			t.Errorf("ResolveExecApprovalDecision(%q) = %q, want nil", input, *got)
		}
	}
}

// --- formatSystemRunAllowlistMissMessage tests (ported from TS) ---

func TestFormatAllowlistMiss_Default(t *testing.T) {
	msg := FormatSystemRunAllowlistMissMessage(false, false)
	want := "SYSTEM_RUN_DENIED: allowlist miss"
	if msg != want {
		t.Errorf("got %q, want %q", msg, want)
	}
}

func TestFormatAllowlistMiss_ShellWrapper(t *testing.T) {
	msg := FormatSystemRunAllowlistMissMessage(true, false)
	if want := "shell wrappers like sh/bash/zsh -c require approval"; !contains(msg, want) {
		t.Errorf("message %q should contain %q", msg, want)
	}
}

func TestFormatAllowlistMiss_WindowsShellWrapper(t *testing.T) {
	msg := FormatSystemRunAllowlistMissMessage(true, true)
	if want := "Windows shell wrappers like cmd.exe /c require approval"; !contains(msg, want) {
		t.Errorf("message %q should contain %q", msg, want)
	}
}

// --- evaluateSystemRunPolicy tests (ported 1:1 from exec-policy.test.ts) ---

func TestPolicy_DeniesWhenSecurityDeny(t *testing.T) {
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.Security = SecurityDeny
	}))
	if d.Allowed {
		t.Fatal("expected denied")
	}
	if d.EventReason != EventReasonSecurityDeny {
		t.Errorf("eventReason = %q, want %q", d.EventReason, EventReasonSecurityDeny)
	}
	if d.ErrorMessage != "SYSTEM_RUN_DISABLED: security=deny" {
		t.Errorf("errorMessage = %q", d.ErrorMessage)
	}
}

func TestPolicy_RequiresApprovalWhenAskAlways(t *testing.T) {
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.Ask = AskAlways
	}))
	if d.Allowed {
		t.Fatal("expected denied")
	}
	if d.EventReason != EventReasonApprovalRequired {
		t.Errorf("eventReason = %q, want %q", d.EventReason, EventReasonApprovalRequired)
	}
	if !d.RequiresAsk {
		t.Error("requiresAsk should be true")
	}
}

func TestPolicy_AllowsMissWithExplicitApproval(t *testing.T) {
	decision := ApprovalAllowOnce
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.Ask = AskOnMiss
		p.AnalysisOk = false
		p.AllowlistSatisfied = false
		p.ApprovalDecision = &decision
	}))
	if !d.Allowed {
		t.Fatal("expected allowed")
	}
	if !d.ApprovedByAsk {
		t.Error("approvedByAsk should be true")
	}
}

func TestPolicy_DeniesAllowlistMissWithoutApproval(t *testing.T) {
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.AnalysisOk = false
		p.AllowlistSatisfied = false
	}))
	if d.Allowed {
		t.Fatal("expected denied")
	}
	if d.EventReason != EventReasonAllowlistMiss {
		t.Errorf("eventReason = %q, want %q", d.EventReason, EventReasonAllowlistMiss)
	}
	if d.ErrorMessage != "SYSTEM_RUN_DENIED: allowlist miss" {
		t.Errorf("errorMessage = %q", d.ErrorMessage)
	}
}

func TestPolicy_ShellWrappersAsAllowlistMiss(t *testing.T) {
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.ShellWrapperInvocation = true
	}))
	if d.Allowed {
		t.Fatal("expected denied")
	}
	if !d.ShellWrapperBlocked {
		t.Error("shellWrapperBlocked should be true")
	}
	if want := "shell wrappers like sh/bash/zsh -c"; !contains(d.ErrorMessage, want) {
		t.Errorf("errorMessage %q should contain %q", d.ErrorMessage, want)
	}
}

func TestPolicy_WindowsCmdWrapperGuidance(t *testing.T) {
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.IsWindows = true
		p.CmdInvocation = true
		p.ShellWrapperInvocation = true
	}))
	if d.Allowed {
		t.Fatal("expected denied")
	}
	if !d.ShellWrapperBlocked {
		t.Error("shellWrapperBlocked should be true")
	}
	if !d.WindowsShellWrapperBlocked {
		t.Error("windowsShellWrapperBlocked should be true")
	}
	if want := "Windows shell wrappers like cmd.exe /c"; !contains(d.ErrorMessage, want) {
		t.Errorf("errorMessage %q should contain %q", d.ErrorMessage, want)
	}
}

func TestPolicy_AllowsWhenChecksPass(t *testing.T) {
	d := EvaluateSystemRunPolicy(buildPolicyParams(func(p *EvaluateSystemRunPolicyParams) {
		p.Ask = AskOnMiss
	}))
	if !d.Allowed {
		t.Fatal("expected allowed")
	}
	if d.RequiresAsk {
		t.Error("requiresAsk should be false")
	}
	if !d.AnalysisOk {
		t.Error("analysisOk should be true")
	}
	if !d.AllowlistSatisfied {
		t.Error("allowlistSatisfied should be true")
	}
}

// --- requiresExecApproval tests ---

func TestRequiresExecApproval(t *testing.T) {
	tests := []struct {
		name               string
		ask                ExecAsk
		security           ExecSecurity
		analysisOk         bool
		allowlistSatisfied bool
		want               bool
	}{
		{"always requires", AskAlways, SecurityAllowlist, true, true, true},
		{"off never requires", AskOff, SecurityAllowlist, false, false, false},
		{"on-miss with miss", AskOnMiss, SecurityAllowlist, false, false, true},
		{"on-miss satisfied", AskOnMiss, SecurityAllowlist, true, true, false},
		{"on-miss non-allowlist", AskOnMiss, SecurityFull, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequiresExecApproval(tt.ask, tt.security, tt.analysisOk, tt.allowlistSatisfied)
			if got != tt.want {
				t.Errorf("RequiresExecApproval() = %v, want %v", got, tt.want)
			}
		})
	}
}

// contains checks if s contains substr (avoids importing strings for one use).
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
