package pipeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type ConstraintKind string

const (
	ConstraintBinaryMissing     ConstraintKind = "binary_missing"
	ConstraintCapacityExhausted ConstraintKind = "capacity_exhausted"
	ConstraintPolicyBlocked     ConstraintKind = "policy_blocked"
	ConstraintLowSignal         ConstraintKind = "low_signal"
	ConstraintAuthRequired      ConstraintKind = "auth_required"
	ConstraintResourceUnavail   ConstraintKind = "resource_unavailable"
	ConstraintRepeatedFailure   ConstraintKind = "repeated_failure"
)

type ConstraintSeverity string

const (
	SeveritySoft ConstraintSeverity = "soft"
	SeverityHard ConstraintSeverity = "hard"
)

type ConstraintResolution string

const (
	ResolutionSelfReroute   ConstraintResolution = "self_reroute"
	ResolutionHumanRequired ConstraintResolution = "human_required"
)

type Constraint struct {
	Kind       ConstraintKind       `json:"kind"`
	Subject    string               `json:"subject"`
	Message    string               `json:"message"`
	Severity   ConstraintSeverity   `json:"severity"`
	Resolution ConstraintResolution `json:"resolution"`
	Sticky     bool                 `json:"sticky"`
	AddedAt    int                  `json:"added_at"`
}

func (c Constraint) Key() string {
	return string(c.Kind) + ":" + c.Subject
}

func (c Constraint) Normalize(iteration int) Constraint {
	out := c
	if out.Severity == "" {
		out.Severity = defaultConstraintSeverity(out.Kind)
	}
	if out.Resolution == "" {
		out.Resolution = defaultConstraintResolution(out.Kind)
	}
	if !out.Sticky {
		out.Sticky = defaultConstraintSticky(out.Kind)
	}
	if out.AddedAt == 0 {
		out.AddedAt = iteration
	}
	return out
}

func defaultConstraintSeverity(kind ConstraintKind) ConstraintSeverity {
	switch kind {
	case ConstraintLowSignal, ConstraintResourceUnavail, ConstraintRepeatedFailure:
		return SeveritySoft
	default:
		return SeverityHard
	}
}

func defaultConstraintResolution(kind ConstraintKind) ConstraintResolution {
	switch kind {
	case ConstraintBinaryMissing, ConstraintPolicyBlocked, ConstraintAuthRequired:
		return ResolutionHumanRequired
	default:
		return ResolutionSelfReroute
	}
}

func defaultConstraintSticky(kind ConstraintKind) bool {
	switch kind {
	case ConstraintLowSignal, ConstraintRepeatedFailure, ConstraintResourceUnavail:
		return false
	default:
		return true
	}
}

func severityRank(severity ConstraintSeverity) int {
	switch severity {
	case SeverityHard:
		return 2
	case SeveritySoft:
		return 1
	default:
		return 0
	}
}

func sortConstraints(entries []Constraint) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		if entries[i].Subject != entries[j].Subject {
			return entries[i].Subject < entries[j].Subject
		}
		return entries[i].Message < entries[j].Message
	})
}

func matchesToolCall(c Constraint, toolName string, args map[string]any) bool {
	switch c.Kind {
	case ConstraintBinaryMissing:
		if toolName != "exec" && toolName != "bash" {
			return false
		}
		return commandUsesBinary(stringArg(args, "command"), c.Subject)
	case ConstraintCapacityExhausted:
		switch c.Subject {
		case "spawn.children", "spawn.concurrent", "spawn.depth":
			return toolName == "spawn"
		}
	case ConstraintPolicyBlocked:
		if toolName != "exec" && toolName != "bash" {
			return false
		}
		cmd := strings.TrimSpace(stringArg(args, "command"))
		return cmd != "" && strings.HasPrefix(cmd, c.Subject)
	case ConstraintAuthRequired:
		return toolName == c.Subject || strings.HasPrefix(c.Subject, toolName+".")
	case ConstraintRepeatedFailure:
		return ToolTargetKey(toolName, args) == c.Subject
	}
	return false
}

func commandUsesBinary(command, binary string) bool {
	command = strings.TrimSpace(command)
	binary = strings.TrimSpace(binary)
	if command == "" || binary == "" {
		return false
	}
	pattern := regexp.MustCompile(`(^|[[:space:];|&])(?:sudo[[:space:]]+)?` + regexp.QuoteMeta(binary) + `([[:space:]]|$)`)
	return pattern.MatchString(command)
}

func PrimaryToolTarget(toolName string, args map[string]any) string {
	switch toolName {
	case "web_fetch":
		return strings.TrimSpace(stringArg(args, "url"))
	case "read_file", "edit", "write_file", "read_document", "read_image", "read_audio", "read_video":
		return strings.TrimSpace(stringArg(args, "path"))
	case "exec", "bash":
		return strings.TrimSpace(stringArg(args, "command"))
	case "spawn":
		action := strings.TrimSpace(stringArg(args, "action"))
		if action != "" && action != "spawn" {
			return "action:" + action
		}
		return strings.TrimSpace(stringArg(args, "task"))
	default:
		for _, key := range []string{"path", "url", "id", "task", "query", "command"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func ToolTargetKey(toolName string, args map[string]any) string {
	target := PrimaryToolTarget(toolName, args)
	if target == "" {
		return toolName
	}
	return fmt.Sprintf("%s:%s", toolName, target)
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return value
}
