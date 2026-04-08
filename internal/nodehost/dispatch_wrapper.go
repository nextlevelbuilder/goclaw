package nodehost

import (
	"regexp"
	"runtime"
	"strings"
)

// MaxDispatchWrapperDepth is the maximum nesting depth for dispatch wrapper unwrapping.
const MaxDispatchWrapperDepth = 4

// DispatchWrapperUnwrapResult is the result of attempting to unwrap a dispatch wrapper.
type DispatchWrapperUnwrapResult struct {
	Kind    string   // "not-wrapper", "blocked", "unwrapped"
	Wrapper string   // wrapper name (empty for "not-wrapper")
	Argv    []string // unwrapped argv (only for "unwrapped")
}

// WrapperScanDirective controls the scan loop behavior.
type wrapperScanDirective int

const (
	scanContinue    wrapperScanDirective = iota
	scanConsumeNext                      // skip next token as option value
	scanStop                             // stop scanning, next token is the command
	scanInvalid                          // unrecognized flag, bail out
)

// IsEnvAssignment checks if a token is an environment variable assignment (FOO=bar).
func IsEnvAssignment(token string) bool {
	return envAssignmentRe.MatchString(token)
}

var envAssignmentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// scanWrapperInvocation is the generic option-scanning loop used by all dispatch wrappers.
func scanWrapperInvocation(
	argv []string,
	separators map[string]bool,
	onToken func(token, lower string) wrapperScanDirective,
	adjustCommandIndex func(idx int, argv []string) *int,
) []string {
	idx := 1
	expectsOptionValue := false
	for idx < len(argv) {
		token := strings.TrimSpace(safeIndex(argv, idx))
		if token == "" {
			idx++
			continue
		}
		if expectsOptionValue {
			expectsOptionValue = false
			idx++
			continue
		}
		if separators[token] {
			idx++
			break
		}
		directive := onToken(token, strings.ToLower(token))
		switch directive {
		case scanStop:
			goto done
		case scanInvalid:
			return nil
		case scanConsumeNext:
			expectsOptionValue = true
		}
		idx++
	}
done:
	if expectsOptionValue {
		return nil
	}
	cmdIdx := idx
	if adjustCommandIndex != nil {
		p := adjustCommandIndex(idx, argv)
		if p == nil {
			return nil
		}
		cmdIdx = *p
	}
	if cmdIdx >= len(argv) {
		return nil
	}
	return argv[cmdIdx:]
}

func safeIndex(argv []string, i int) string {
	if i < len(argv) {
		return argv[i]
	}
	return ""
}

func intPtr(v int) *int { return &v }

// --- Individual wrapper unwrappers ---

var envOptionsWithValue = newSet("-u", "--unset", "-c", "--chdir", "-s", "--split-string",
	"--default-signal", "--ignore-signal", "--block-signal")
var envInlineValuePrefixes = []string{"-u", "-c", "-s", "--unset=", "--chdir=", "--split-string=",
	"--default-signal=", "--ignore-signal=", "--block-signal="}
var envFlagOptions = newSet("-i", "--ignore-environment", "-0", "--null")

func hasEnvInlineValuePrefix(lower string) bool {
	for _, p := range envInlineValuePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func unwrapEnvInvocation(argv []string) []string {
	return scanWrapperInvocation(argv, map[string]bool{"--": true, "-": true},
		func(token, lower string) wrapperScanDirective {
			if IsEnvAssignment(token) {
				return scanContinue
			}
			if !strings.HasPrefix(token, "-") || token == "-" {
				return scanStop
			}
			flag := strings.SplitN(lower, "=", 2)[0]
			if envFlagOptions[flag] {
				return scanContinue
			}
			if envOptionsWithValue[flag] {
				if strings.Contains(lower, "=") {
					return scanContinue
				}
				return scanConsumeNext
			}
			if hasEnvInlineValuePrefix(lower) {
				return scanContinue
			}
			return scanInvalid
		}, nil)
}

func envInvocationUsesModifiers(argv []string) bool {
	idx := 1
	expectsOptionValue := false
	for idx < len(argv) {
		token := strings.TrimSpace(safeIndex(argv, idx))
		if token == "" {
			idx++
			continue
		}
		if expectsOptionValue {
			return true
		}
		if token == "--" || token == "-" {
			break
		}
		if IsEnvAssignment(token) {
			return true
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			break
		}
		lower := strings.ToLower(token)
		flag := strings.SplitN(lower, "=", 2)[0]
		if envFlagOptions[flag] {
			return true
		}
		if envOptionsWithValue[flag] {
			if strings.Contains(lower, "=") {
				return true
			}
			expectsOptionValue = true
			idx++
			continue
		}
		if hasEnvInlineValuePrefix(lower) {
			return true
		}
		return true
	}
	return false
}

// newSet creates a simple string set from variadic args.
func newSet(args ...string) map[string]bool {
	m := make(map[string]bool, len(args))
	for _, a := range args {
		m[a] = true
	}
	return m
}

// unwrapDashOptionInvocation scans dash-prefixed flags and stops at positional args.
func unwrapDashOptionInvocation(
	argv []string,
	onFlag func(flag, lower string) wrapperScanDirective,
	adjustCommandIndex func(int, []string) *int,
) []string {
	return scanWrapperInvocation(argv, map[string]bool{"--": true},
		func(token, lower string) wrapperScanDirective {
			if !strings.HasPrefix(token, "-") || token == "-" {
				return scanStop
			}
			flag := strings.SplitN(lower, "=", 2)[0]
			return onFlag(flag, lower)
		}, adjustCommandIndex)
}

// Platform-gated wrappers.
func isDarwin() bool { return runtime.GOOS == "darwin" }
