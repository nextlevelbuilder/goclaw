package nodehost

// SystemRunApprovalFileOperand describes a mutable file operand in a command.
type SystemRunApprovalFileOperand struct {
	ArgvIndex int    `json:"argvIndex"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
}

// SystemRunApprovalPlan describes a parsed command plan for exec approval.
type SystemRunApprovalPlan struct {
	Argv               []string                      `json:"argv"`
	Cwd                *string                       `json:"cwd,omitempty"`
	CommandText        string                        `json:"commandText"`
	CommandPreview     *string                       `json:"commandPreview,omitempty"`
	AgentID            *string                       `json:"agentId,omitempty"`
	SessionKey         *string                       `json:"sessionKey,omitempty"`
	MutableFileOperand *SystemRunApprovalFileOperand `json:"mutableFileOperand,omitempty"`
}

// SystemRunParams mirrors the TS SystemRunParams for invoking a system command.
type SystemRunParams struct {
	Command              []string               `json:"command"`
	RawCommand           *string                `json:"rawCommand,omitempty"`
	SystemRunPlan        *SystemRunApprovalPlan  `json:"systemRunPlan,omitempty"`
	Cwd                  *string                `json:"cwd,omitempty"`
	Env                  map[string]string       `json:"env,omitempty"`
	TimeoutMs            *int                   `json:"timeoutMs,omitempty"`
	NeedsScreenRecording *bool                  `json:"needsScreenRecording,omitempty"`
	AgentID              *string                `json:"agentId,omitempty"`
	SessionKey           *string                `json:"sessionKey,omitempty"`
	Approved             *bool                  `json:"approved,omitempty"`
	ApprovalDecision     *string                `json:"approvalDecision,omitempty"`
	RunID                *string                `json:"runId,omitempty"`
	SuppressNotifyOnExit *bool                  `json:"suppressNotifyOnExit,omitempty"`
}

// RunResult holds the outcome of a system command execution.
type RunResult struct {
	ExitCode  *int    `json:"exitCode,omitempty"`
	TimedOut  bool    `json:"timedOut"`
	Success   bool    `json:"success"`
	Stdout    string  `json:"stdout"`
	Stderr    string  `json:"stderr"`
	Error     *string `json:"error,omitempty"`
	Truncated bool    `json:"truncated"`
}

// ExecEventPayload is sent as a WebSocket event during command execution.
type ExecEventPayload struct {
	SessionKey           string `json:"sessionKey"`
	RunID                string `json:"runId"`
	Host                 string `json:"host"`
	Command              string `json:"command,omitempty"`
	ExitCode             *int   `json:"exitCode,omitempty"`
	TimedOut             *bool  `json:"timedOut,omitempty"`
	Success              *bool  `json:"success,omitempty"`
	Output               string `json:"output,omitempty"`
	Reason               string `json:"reason,omitempty"`
	SuppressNotifyOnExit *bool  `json:"suppressNotifyOnExit,omitempty"`
}

// ExecFinishedResult holds the final result of an exec operation.
type ExecFinishedResult struct {
	Stdout   string  `json:"stdout,omitempty"`
	Stderr   string  `json:"stderr,omitempty"`
	Error    *string `json:"error,omitempty"`
	ExitCode *int    `json:"exitCode,omitempty"`
	TimedOut *bool   `json:"timedOut,omitempty"`
	Success  *bool   `json:"success,omitempty"`
}

// ExecFinishedEventParams is emitted when a command finishes execution.
type ExecFinishedEventParams struct {
	SessionKey           string             `json:"sessionKey"`
	RunID                string             `json:"runId"`
	CommandText          string             `json:"commandText"`
	Result               ExecFinishedResult `json:"result"`
	SuppressNotifyOnExit *bool              `json:"suppressNotifyOnExit,omitempty"`
}

// SkillBinTrustEntry represents a trusted skill binary.
type SkillBinTrustEntry struct {
	Name         string `json:"name"`
	ResolvedPath string `json:"resolvedPath"`
}

// SkillBinsProvider retrieves the current set of trusted skill binaries.
type SkillBinsProvider interface {
	Current(force bool) ([]SkillBinTrustEntry, error)
}

// ExecApprovalDecision represents the user's approval choice.
type ExecApprovalDecision string

const (
	ApprovalAllowOnce   ExecApprovalDecision = "allow-once"
	ApprovalAllowAlways ExecApprovalDecision = "allow-always"
)

// ResolveExecApprovalDecision parses a string into a valid ExecApprovalDecision.
// Returns nil if the value is not recognized.
func ResolveExecApprovalDecision(v string) *ExecApprovalDecision {
	switch v {
	case "allow-once", "allow-always":
		d := ExecApprovalDecision(v)
		return &d
	default:
		return nil
	}
}

// SystemRunPolicyEventReason describes why a run was denied.
type SystemRunPolicyEventReason string

const (
	EventReasonSecurityDeny    SystemRunPolicyEventReason = "security=deny"
	EventReasonApprovalRequired SystemRunPolicyEventReason = "approval-required"
	EventReasonAllowlistMiss   SystemRunPolicyEventReason = "allowlist-miss"
)

// SystemRunPolicyDecision is the result of evaluating exec policy.
type SystemRunPolicyDecision struct {
	Allowed                    bool                        `json:"allowed"`
	AnalysisOk                 bool                        `json:"analysisOk"`
	AllowlistSatisfied         bool                        `json:"allowlistSatisfied"`
	ShellWrapperBlocked        bool                        `json:"shellWrapperBlocked"`
	WindowsShellWrapperBlocked bool                        `json:"windowsShellWrapperBlocked"`
	RequiresAsk                bool                        `json:"requiresAsk"`
	ApprovalDecision           *ExecApprovalDecision       `json:"approvalDecision"`
	ApprovedByAsk              bool                        `json:"approvedByAsk"`
	EventReason                SystemRunPolicyEventReason  `json:"eventReason,omitempty"`
	ErrorMessage               string                      `json:"errorMessage,omitempty"`
}
