package nodehost

import (
	"runtime"
	"strings"
)

// ExecAllowlistEntry represents a single allowlist pattern entry.
type ExecAllowlistEntry struct {
	Pattern string `json:"pattern"`
}

// CommandResolution holds the resolved path for a command binary.
type CommandResolution struct {
	ResolvedPath string `json:"resolvedPath,omitempty"`
}

// ExecCommandSegment represents a parsed segment of a command pipeline.
type ExecCommandSegment struct {
	Raw        string             `json:"raw"`
	Argv       []string           `json:"argv"`
	Resolution *CommandResolution `json:"resolution,omitempty"`
}

// SystemRunAllowlistAnalysis holds the result of allowlist evaluation.
type SystemRunAllowlistAnalysis struct {
	AnalysisOk         bool                 `json:"analysisOk"`
	AllowlistMatches   []ExecAllowlistEntry `json:"allowlistMatches"`
	AllowlistSatisfied bool                 `json:"allowlistSatisfied"`
	Segments           []ExecCommandSegment `json:"segments"`
}

// AllowlistEvaluator abstracts the platform-specific allowlist matching.
// Implementations handle shell tokenization and exec argv analysis.
type AllowlistEvaluator interface {
	EvaluateShellAllowlist(command string, allowlist []ExecAllowlistEntry) SystemRunAllowlistAnalysis
	EvaluateExecAllowlist(argv []string, allowlist []ExecAllowlistEntry) SystemRunAllowlistAnalysis
}

// ResolvePlannedAllowlistArgv determines if a planned allowlist argv should
// override the original argv. Returns nil if no override is needed.
func ResolvePlannedAllowlistArgv(
	security ExecSecurity,
	shellCommand *string,
	approvedByAsk bool,
	analysisOk bool,
	allowlistSatisfied bool,
	segments []ExecCommandSegment,
) []string {
	if security != SecurityAllowlist ||
		approvedByAsk ||
		shellCommand != nil ||
		!analysisOk ||
		!allowlistSatisfied ||
		len(segments) != 1 {
		return nil
	}
	planned := resolvePlannedSegmentArgv(segments[0])
	if len(planned) == 0 {
		return nil
	}
	return planned
}

// resolvePlannedSegmentArgv extracts the resolved argv from a single segment.
func resolvePlannedSegmentArgv(seg ExecCommandSegment) []string {
	if len(seg.Argv) == 0 {
		return nil
	}
	return seg.Argv
}

// ResolveSystemRunExecArgv determines the final argv for command execution.
func ResolveSystemRunExecArgv(
	plannedArgv []string,
	argv []string,
	security ExecSecurity,
	isWindows bool,
	approvedByAsk bool,
	analysisOk bool,
	allowlistSatisfied bool,
	shellCommand *string,
	segments []ExecCommandSegment,
) []string {
	execArgv := argv
	if plannedArgv != nil {
		execArgv = plannedArgv
	}

	if security == SecurityAllowlist &&
		isWindows &&
		!approvedByAsk &&
		shellCommand != nil &&
		analysisOk &&
		allowlistSatisfied &&
		len(segments) == 1 &&
		len(segments[0].Argv) > 0 {
		execArgv = segments[0].Argv
	}

	return execArgv
}

// IsWindows returns true if the current platform is Windows.
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// ApplyOutputTruncation appends a truncation suffix to the result
// if it was truncated. Appends to stderr if non-empty, otherwise stdout.
func ApplyOutputTruncation(result *RunResult) {
	if !result.Truncated {
		return
	}
	const suffix = "... (truncated)"
	if len(strings.TrimSpace(result.Stderr)) > 0 {
		result.Stderr = result.Stderr + "\n" + suffix
	} else {
		result.Stdout = result.Stdout + "\n" + suffix
	}
}

