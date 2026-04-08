package nodehost

// ExecSecurity represents the security mode for command execution.
type ExecSecurity string

const (
	SecurityDeny      ExecSecurity = "deny"
	SecurityAllowlist ExecSecurity = "allowlist"
	SecurityFull      ExecSecurity = "full"
)

// ExecAsk represents the approval ask mode.
type ExecAsk string

const (
	AskOff    ExecAsk = "off"
	AskOnMiss ExecAsk = "on-miss"
	AskAlways ExecAsk = "always"
)

// RequiresExecApproval determines whether a command needs user approval
// based on the ask mode, security mode, and allowlist analysis results.
func RequiresExecApproval(ask ExecAsk, security ExecSecurity, analysisOk, allowlistSatisfied bool) bool {
	switch ask {
	case AskAlways:
		return true
	case AskOnMiss:
		return security == SecurityAllowlist && (!analysisOk || !allowlistSatisfied)
	default:
		return false
	}
}

// EvaluateSystemRunPolicyParams contains the inputs for policy evaluation.
type EvaluateSystemRunPolicyParams struct {
	Security               ExecSecurity
	Ask                    ExecAsk
	AnalysisOk             bool
	AllowlistSatisfied     bool
	ApprovalDecision       *ExecApprovalDecision
	Approved               bool
	IsWindows              bool
	CmdInvocation          bool
	ShellWrapperInvocation bool
}

// EvaluateSystemRunPolicy evaluates whether a system command should be allowed.
// Returns a decision with the reason for denial if not allowed.
func EvaluateSystemRunPolicy(p EvaluateSystemRunPolicyParams) SystemRunPolicyDecision {
	shellWrapperBlocked := p.Security == SecurityAllowlist && p.ShellWrapperInvocation
	windowsShellWrapperBlocked := shellWrapperBlocked && p.IsWindows && p.CmdInvocation

	analysisOk := p.AnalysisOk
	allowlistSatisfied := p.AllowlistSatisfied
	if shellWrapperBlocked {
		analysisOk = false
		allowlistSatisfied = false
	}

	approvedByAsk := p.ApprovalDecision != nil || p.Approved

	base := SystemRunPolicyDecision{
		AnalysisOk:                 analysisOk,
		AllowlistSatisfied:         allowlistSatisfied,
		ShellWrapperBlocked:        shellWrapperBlocked,
		WindowsShellWrapperBlocked: windowsShellWrapperBlocked,
		ApprovalDecision:           p.ApprovalDecision,
		ApprovedByAsk:              approvedByAsk,
	}

	// Branch 1: security=deny always blocks.
	if p.Security == SecurityDeny {
		base.Allowed = false
		base.RequiresAsk = false
		base.EventReason = EventReasonSecurityDeny
		base.ErrorMessage = "SYSTEM_RUN_DISABLED: security=deny"
		return base
	}

	// Branch 2: ask policy requires approval.
	requiresAsk := RequiresExecApproval(p.Ask, p.Security, analysisOk, allowlistSatisfied)
	base.RequiresAsk = requiresAsk
	if requiresAsk && !approvedByAsk {
		base.Allowed = false
		base.EventReason = EventReasonApprovalRequired
		base.ErrorMessage = "SYSTEM_RUN_DENIED: approval required"
		return base
	}

	// Branch 3: allowlist miss without approval.
	if p.Security == SecurityAllowlist && (!analysisOk || !allowlistSatisfied) && !approvedByAsk {
		base.Allowed = false
		base.EventReason = EventReasonAllowlistMiss
		base.ErrorMessage = FormatSystemRunAllowlistMissMessage(shellWrapperBlocked, windowsShellWrapperBlocked)
		return base
	}

	// All checks passed.
	base.Allowed = true
	return base
}

// FormatSystemRunAllowlistMissMessage returns the appropriate denial message
// based on whether shell wrappers are involved.
func FormatSystemRunAllowlistMissMessage(shellWrapperBlocked, windowsShellWrapperBlocked bool) string {
	if windowsShellWrapperBlocked {
		return "SYSTEM_RUN_DENIED: allowlist miss " +
			"(Windows shell wrappers like cmd.exe /c require approval; " +
			"approve once/always or run with --ask on-miss|always)"
	}
	if shellWrapperBlocked {
		return "SYSTEM_RUN_DENIED: allowlist miss " +
			"(shell wrappers like sh/bash/zsh -c require approval; " +
			"approve once/always or run with --ask on-miss|always)"
	}
	return "SYSTEM_RUN_DENIED: allowlist miss"
}
