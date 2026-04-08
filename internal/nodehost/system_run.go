package nodehost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// SystemRunDeniedError is returned when the system run is denied by policy.
type SystemRunDeniedError struct {
	Reason  SystemRunPolicyEventReason
	Message string
}

func (e *SystemRunDeniedError) Error() string { return e.Message }

// SystemRunInvokeResult is the result sent back to the gateway.
type SystemRunInvokeResult struct {
	Ok          bool   `json:"ok"`
	PayloadJSON string `json:"payloadJSON,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
	ErrorMsg    string `json:"errorMessage,omitempty"`
}

// SystemRunDeps provides external dependencies for the system run pipeline.
type SystemRunDeps struct {
	// SendDeniedEvent dispatches an exec.denied event to the gateway.
	SendDeniedEvent func(payload ExecEventPayload)
	// SendFinishedEvent dispatches an exec.finished event.
	SendFinishedEvent func(params ExecFinishedEventParams)
	// SendResult sends the invoke result back.
	SendResult func(result SystemRunInvokeResult)
}

// SystemRunExecutionContext holds per-invocation metadata.
type SystemRunExecutionContext struct {
	SessionKey           string
	RunID                string
	CommandText          string
	SuppressNotifyOnExit bool
}

// HandleSystemRunInvoke executes the 3-phase system.run pipeline.
func HandleSystemRunInvoke(ctx context.Context, params SystemRunParams, deps SystemRunDeps) {
	// Phase 1: Parse
	parsed := parseSystemRunPhase(params, deps)
	if parsed == nil {
		return
	}

	// Phase 2: Policy
	policy := evaluateSystemRunPolicyPhase(parsed, deps)
	if policy == nil {
		return
	}

	// Phase 3: Execute
	executeSystemRunPhase(ctx, parsed, policy, deps)
}

// --- Phase 1: Parse ---

type parsedSystemRun struct {
	argv                 []string
	shellPayload         *string
	commandText          string
	execution            SystemRunExecutionContext
	approvalDecision     *ExecApprovalDecision
	env                  map[string]string
	cwd                  string
	timeoutMs            int
	approved             bool
	suppressNotifyOnExit bool
	approvalPlan         *SystemRunApprovalPlan
}

func parseSystemRunPhase(params SystemRunParams, deps SystemRunDeps) *parsedSystemRun {
	resolved := ResolveSystemRunCommandRequest(params.Command, params.RawCommand)
	if !resolved.Ok {
		deps.SendResult(SystemRunInvokeResult{Ok: false, ErrorCode: "INVALID_REQUEST", ErrorMsg: resolved.Message})
		return nil
	}
	if len(resolved.Argv) == 0 {
		deps.SendResult(SystemRunInvokeResult{Ok: false, ErrorCode: "INVALID_REQUEST", ErrorMsg: "command required"})
		return nil
	}

	sessionKey := "node"
	if params.SessionKey != nil && strings.TrimSpace(*params.SessionKey) != "" {
		sessionKey = strings.TrimSpace(*params.SessionKey)
	}
	runID := ""
	if params.RunID != nil && strings.TrimSpace(*params.RunID) != "" {
		runID = strings.TrimSpace(*params.RunID)
	}
	if runID == "" {
		id, _ := newUUID()
		runID = id
	}
	suppressNotify := params.SuppressNotifyOnExit != nil && *params.SuppressNotifyOnExit

	// Sanitize env overrides.
	envOverrides := SanitizeSystemRunEnvOverrides(params.Env, resolved.ShellPayload != nil)
	env := SanitizeEnv(envOverrides)

	cwd := ""
	if params.Cwd != nil {
		cwd = strings.TrimSpace(*params.Cwd)
	}
	timeoutMs := 0
	if params.TimeoutMs != nil {
		timeoutMs = *params.TimeoutMs
	}
	approved := params.Approved != nil && *params.Approved
	approvalDecision := ResolveExecApprovalDecision(stringOrEmpty(params.ApprovalDecision))

	return &parsedSystemRun{
		argv:         resolved.Argv,
		shellPayload: resolved.ShellPayload,
		commandText:  resolved.CommandText,
		execution: SystemRunExecutionContext{
			SessionKey:           sessionKey,
			RunID:                runID,
			CommandText:          resolved.CommandText,
			SuppressNotifyOnExit: suppressNotify,
		},
		approvalDecision:     approvalDecision,
		env:                  env,
		cwd:                  cwd,
		timeoutMs:            timeoutMs,
		approved:             approved,
		suppressNotifyOnExit: suppressNotify,
		approvalPlan:         params.SystemRunPlan,
	}
}

// --- Phase 2: Policy ---

type policyResult struct {
	decision SystemRunPolicyDecision
	parsed   *parsedSystemRun
}

func evaluateSystemRunPolicyPhase(parsed *parsedSystemRun, deps SystemRunDeps) *policyResult {
	decision := EvaluateSystemRunPolicy(EvaluateSystemRunPolicyParams{
		Security:               SecurityAllowlist, // default; real impl loads from config
		Ask:                    AskOff,
		AnalysisOk:             true,
		AllowlistSatisfied:     true,
		ApprovalDecision:       parsed.approvalDecision,
		Approved:               parsed.approved,
		IsWindows:              IsWindows(),
		CmdInvocation:          false,
		ShellWrapperInvocation: parsed.shellPayload != nil,
	})

	if !decision.Allowed {
		sendDenied(deps, parsed.execution, decision.EventReason, decision.ErrorMessage)
		return nil
	}

	// Harden execution paths if approved by ask.
	if decision.ApprovedByAsk {
		harden := HardenApprovedExecutionPaths(true, parsed.argv, parsed.shellPayload, parsed.cwd)
		if !harden.Ok {
			sendDenied(deps, parsed.execution, EventReasonApprovalRequired, harden.Message)
			return nil
		}
		parsed.argv = harden.Argv
		if harden.Cwd != "" {
			parsed.cwd = harden.Cwd
		}
	}

	return &policyResult{decision: decision, parsed: parsed}
}

// --- Phase 3: Execute ---

func executeSystemRunPhase(ctx context.Context, parsed *parsedSystemRun, policy *policyResult, deps SystemRunDeps) {
	// Revalidate approval plan if present.
	if parsed.approvalPlan != nil && parsed.approvalPlan.MutableFileOperand != nil {
		if !RevalidateApprovedMutableFileOperand(parsed.approvalPlan.MutableFileOperand, parsed.argv, parsed.cwd) {
			slog.Warn("security: system.run approval script drift blocked",
				"runId", parsed.execution.RunID)
			sendDenied(deps, parsed.execution, EventReasonApprovalRequired,
				"SYSTEM_RUN_DENIED: approval script operand changed before execution")
			return
		}
	}

	// Build env slice for exec.
	envSlice := make([]string, 0, len(parsed.env))
	for k, v := range parsed.env {
		envSlice = append(envSlice, k+"="+v)
	}

	// Execute.
	result := RunCommand(ctx, parsed.argv, RunCommandOpts{
		Cwd:       parsed.cwd,
		Env:       envSlice,
		TimeoutMs: parsed.timeoutMs,
	})
	ApplyOutputTruncation(result)

	// Build finished result.
	finishedResult := ExecFinishedResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		Error:    result.Error,
		ExitCode: result.ExitCode,
	}
	if result.TimedOut {
		timedOut := true
		finishedResult.TimedOut = &timedOut
	}
	success := result.Success
	finishedResult.Success = &success

	// Dispatch finished event.
	deps.SendFinishedEvent(ExecFinishedEventParams{
		SessionKey:           parsed.execution.SessionKey,
		RunID:                parsed.execution.RunID,
		CommandText:          parsed.execution.CommandText,
		Result:               finishedResult,
		SuppressNotifyOnExit: boolPtr(parsed.suppressNotifyOnExit),
	})

	// Send result.
	payloadJSON, _ := json.Marshal(result)
	deps.SendResult(SystemRunInvokeResult{Ok: true, PayloadJSON: string(payloadJSON)})
}

// --- Helpers ---

func sendDenied(deps SystemRunDeps, exec SystemRunExecutionContext, reason SystemRunPolicyEventReason, message string) {
	deps.SendDeniedEvent(ExecEventPayload{
		SessionKey:           exec.SessionKey,
		RunID:                exec.RunID,
		Host:                 "node",
		Command:              exec.CommandText,
		Reason:               string(reason),
		SuppressNotifyOnExit: boolPtr(exec.SuppressNotifyOnExit),
	})
	deps.SendResult(SystemRunInvokeResult{
		Ok:       false,
		ErrorCode: "UNAVAILABLE",
		ErrorMsg:  message,
	})
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func boolPtr(b bool) *bool {
	if !b {
		return nil
	}
	return &b
}

// EnvToSlice converts a map to os.Environ() format.
func EnvToSlice(env map[string]string) []string {
	s := make([]string, 0, len(env))
	for k, v := range env {
		s = append(s, fmt.Sprintf("%s=%s", k, v))
	}
	return s
}
