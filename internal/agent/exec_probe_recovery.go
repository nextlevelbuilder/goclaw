package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func isReadOnlyExecProbe(toolName string, args map[string]any) bool {
	if toolName != "exec" && toolName != "bash" {
		return false
	}
	command, _ := args["command"].(string)
	return looksLikeEnvironmentProbe(command)
}

func looksLikeEnvironmentProbe(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}

	indicators := []string{
		"which ",
		"command -v ",
		"--version",
		"docker info",
		"docker version",
		"docker ps",
		"git --version",
		"gh --version",
		"python --version",
		"python3 --version",
		"pip --version",
		"pip3 --version",
		"pip list",
		"pip3 list",
		"npm --version",
		"npm list",
		"node --version",
		"uname",
		"/etc/os-release",
		"lsb_release",
		"hostname",
		"whoami",
		"id",
		"free -",
		"nproc",
	}
	for _, indicator := range indicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

func isReadOnlyExecProbeMiss(command string, result *tools.Result) bool {
	if result == nil || strings.TrimSpace(command) == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(result.ForLLM))
	if lower == "" {
		return false
	}
	switch {
	case strings.Contains(lower, "command not found"),
		strings.Contains(lower, "no such file or directory"):
		return true
	case result.IsError && strings.Contains(lower, "not installed"):
		return true
	case result.IsError && lower == "exit status 1":
		cmdLower := strings.ToLower(strings.TrimSpace(command))
		return strings.Contains(cmdLower, "which ") || strings.Contains(cmdLower, "command -v ")
	default:
		return false
	}
}

func extractProbedBinary(command string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ""
	}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bwhich\s+([a-z0-9._-]+)`),
		regexp.MustCompile(`(?i)\bcommand\s+-v\s+([a-z0-9._-]+)`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(trimmed)
		if len(matches) == 2 {
			return strings.ToLower(matches[1])
		}
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

func (l *Loop) maybeArmReadOnlyExecProbeRecovery(
	state *pipeline.RunState,
	toolName string,
	args map[string]any,
	result *tools.Result,
) *providers.Message {
	if state == nil || result == nil {
		return nil
	}
	if !isReadOnlyExecProbe(toolName, args) {
		return nil
	}
	command, _ := args["command"].(string)
	if !isReadOnlyExecProbeMiss(command, result) {
		return nil
	}
	binary := extractProbedBinary(command)
	if binary == "" {
		return nil
	}

	constraint := pipeline.Constraint{
		Kind:       pipeline.ConstraintBinaryMissing,
		Subject:    binary,
		Severity:   pipeline.SeverityHard,
		Resolution: pipeline.ResolutionHumanRequired,
		Sticky:     true,
		AddedAt:    state.Iteration,
		Message:    fmt.Sprintf("%s is not installed in this environment", binary),
	}
	appendToolConstraint(result, constraint)

	if !state.EnsureConstraintStore().Add(constraint) {
		return nil
	}
	state.Turn.ArmNeedsHuman(pipeline.TurnCloseoutReasonConstraintNeedsHuman)
	return &providers.Message{
		Role:    "system",
		Content: fmt.Sprintf("[System] Missing prerequisite detected: %s. Do not retry shell probes or commands that depend on it. This requires human action to resolve.", constraint.Message),
	}
}
