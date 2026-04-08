package nodehost

import (
	"regexp"
	"strings"
)

// dispatchWrapperSpec defines how to unwrap a specific dispatch wrapper.
type dispatchWrapperSpec struct {
	name             string
	unwrap           func(argv []string) []string // nil means always blocked
	isTransparent    func(argv []string) bool     // nil means opaque (semantic usage)
}

var niceOptionsWithValue = newSet("-n", "--adjustment", "--priority")
var caffeinateOptionsWithValue = newSet("-t", "-w")
var stdbufOptionsWithValue = newSet("-i", "--input", "-o", "--output", "-e", "--error")
var timeFlagOptions = newSet("-a", "--append", "-h", "--help", "-l", "-p", "-q", "--quiet", "-v", "--verbose", "-V", "--version")
var timeOptionsWithValue = newSet("-f", "--format", "-o", "--output")
var bsdScriptFlagOptions = newSet("-a", "-d", "-k", "-p", "-q", "-r")
var bsdScriptOptionsWithValue = newSet("-F", "-t")
var sandboxExecOptionsWithValue = newSet("-f", "-p", "-d")
var timeoutFlagOptions = newSet("--foreground", "--preserve-status", "-v", "--verbose")
var timeoutOptionsWithValue = newSet("-k", "--kill-after", "-s", "--signal")
var xcrunFlagOptions = newSet("-k", "--kill-cache", "-l", "--log", "-n", "--no-cache", "-r", "--run", "-v", "--verbose")

var knownArchRe = regexp.MustCompile(`^-(?:arm64|arm64e|i386|x86_64|x86_64h)$`)
var niceNumericRe = regexp.MustCompile(`^-\d+$`)

func unwrapNice(argv []string) []string {
	return unwrapDashOptionInvocation(argv, func(flag, lower string) wrapperScanDirective {
		if niceNumericRe.MatchString(lower) {
			return scanContinue
		}
		if niceOptionsWithValue[flag] {
			if strings.Contains(lower, "=") || lower != flag {
				return scanContinue
			}
			return scanConsumeNext
		}
		if strings.HasPrefix(lower, "-n") && len(lower) > 2 {
			return scanContinue
		}
		return scanInvalid
	}, nil)
}

func unwrapCaffeinate(argv []string) []string {
	return unwrapDashOptionInvocation(argv, func(flag, lower string) wrapperScanDirective {
		switch flag {
		case "-d", "-i", "-m", "-s", "-u":
			return scanContinue
		}
		if caffeinateOptionsWithValue[flag] {
			if lower != flag || strings.Contains(lower, "=") {
				return scanContinue
			}
			return scanConsumeNext
		}
		return scanInvalid
	}, nil)
}

func unwrapNohup(argv []string) []string {
	return scanWrapperInvocation(argv, map[string]bool{"--": true},
		func(token, lower string) wrapperScanDirective {
			if !strings.HasPrefix(token, "-") || token == "-" {
				return scanStop
			}
			if lower == "--help" || lower == "--version" {
				return scanContinue
			}
			return scanInvalid
		}, nil)
}

func unwrapSandboxExec(argv []string) []string {
	return unwrapDashOptionInvocation(argv, func(flag, lower string) wrapperScanDirective {
		if sandboxExecOptionsWithValue[flag] {
			if lower != flag || strings.Contains(lower, "=") {
				return scanContinue
			}
			return scanConsumeNext
		}
		return scanInvalid
	}, nil)
}

func unwrapStdbuf(argv []string) []string {
	return unwrapDashOptionInvocation(argv, func(flag, lower string) wrapperScanDirective {
		if !stdbufOptionsWithValue[flag] {
			return scanInvalid
		}
		if strings.Contains(lower, "=") {
			return scanContinue
		}
		return scanConsumeNext
	}, nil)
}

func unwrapTime(argv []string) []string {
	return unwrapDashOptionInvocation(argv, func(flag, lower string) wrapperScanDirective {
		if timeFlagOptions[flag] {
			return scanContinue
		}
		if timeOptionsWithValue[flag] {
			if strings.Contains(lower, "=") {
				return scanContinue
			}
			return scanConsumeNext
		}
		return scanInvalid
	}, nil)
}

func unwrapScript(argv []string) []string {
	if !isDarwin() {
		return nil
	}
	return scanWrapperInvocation(argv, map[string]bool{"--": true},
		func(token, lower string) wrapperScanDirective {
			if !strings.HasPrefix(lower, "-") || lower == "-" {
				return scanStop
			}
			flag := strings.SplitN(token, "=", 2)[0]
			if bsdScriptOptionsWithValue[flag] {
				if strings.Contains(token, "=") {
					return scanContinue
				}
				return scanConsumeNext
			}
			if bsdScriptFlagOptions[flag] {
				return scanContinue
			}
			return scanInvalid
		}, func(cmdIdx int, a []string) *int {
			// Skip transcript file positional arg.
			sawTranscript := false
			for i := cmdIdx; i < len(a); i++ {
				t := strings.TrimSpace(safeIndex(a, i))
				if t == "" {
					continue
				}
				if !sawTranscript {
					sawTranscript = true
					continue
				}
				return intPtr(i)
			}
			return nil
		})
}

func unwrapTimeout(argv []string) []string {
	return unwrapDashOptionInvocation(argv, func(flag, lower string) wrapperScanDirective {
		if timeoutFlagOptions[flag] {
			return scanContinue
		}
		if timeoutOptionsWithValue[flag] {
			if strings.Contains(lower, "=") {
				return scanContinue
			}
			return scanConsumeNext
		}
		return scanInvalid
	}, func(cmdIdx int, a []string) *int {
		// Skip the duration positional arg.
		next := cmdIdx + 1
		if next < len(a) {
			return intPtr(next)
		}
		return nil
	})
}

func unwrapArch(argv []string) []string {
	if !isDarwin() {
		return nil
	}
	expectsArchName := false
	return scanWrapperInvocation(argv, nil,
		func(token, lower string) wrapperScanDirective {
			if expectsArchName {
				expectsArchName = false
				if knownArchRe.MatchString("-" + lower) {
					return scanContinue
				}
				return scanInvalid
			}
			if !strings.HasPrefix(token, "-") || token == "-" {
				return scanStop
			}
			if lower == "-32" || lower == "-64" {
				return scanContinue
			}
			if lower == "-arch" {
				expectsArchName = true
				return scanContinue
			}
			if lower == "-c" || lower == "-d" || lower == "-e" || lower == "-h" {
				return scanInvalid
			}
			if knownArchRe.MatchString(lower) {
				return scanContinue
			}
			return scanInvalid
		}, nil)
}

func unwrapXcrun(argv []string) []string {
	if !isDarwin() {
		return nil
	}
	return scanWrapperInvocation(argv, nil,
		func(token, lower string) wrapperScanDirective {
			if !strings.HasPrefix(token, "-") || token == "-" {
				return scanStop
			}
			if xcrunFlagOptions[lower] {
				return scanContinue
			}
			return scanInvalid
		}, nil)
}

// dispatchWrapperSpecs is the registry of known dispatch wrappers.
var dispatchWrapperSpecs = []dispatchWrapperSpec{
	{name: "arch", unwrap: unwrapArch, isTransparent: func(_ []string) bool { return isDarwin() }},
	{name: "caffeinate", unwrap: unwrapCaffeinate, isTransparent: func(_ []string) bool { return true }},
	{name: "chrt"},
	{name: "doas"},
	{name: "env", unwrap: unwrapEnvInvocation, isTransparent: func(a []string) bool { return !envInvocationUsesModifiers(a) }},
	{name: "ionice"},
	{name: "nice", unwrap: unwrapNice, isTransparent: func(_ []string) bool { return true }},
	{name: "nohup", unwrap: unwrapNohup, isTransparent: func(_ []string) bool { return true }},
	{name: "sandbox-exec", unwrap: unwrapSandboxExec, isTransparent: func(_ []string) bool { return true }},
	{name: "script", unwrap: unwrapScript, isTransparent: func(_ []string) bool { return true }},
	{name: "setsid"},
	{name: "stdbuf", unwrap: unwrapStdbuf, isTransparent: func(_ []string) bool { return true }},
	{name: "sudo"},
	{name: "taskset"},
	{name: "time", unwrap: unwrapTime, isTransparent: func(_ []string) bool { return true }},
	{name: "timeout", unwrap: unwrapTimeout, isTransparent: func(_ []string) bool { return true }},
	{name: "xcrun", unwrap: unwrapXcrun, isTransparent: func(_ []string) bool { return isDarwin() }},
}

var dispatchSpecByName = func() map[string]*dispatchWrapperSpec {
	m := make(map[string]*dispatchWrapperSpec, len(dispatchWrapperSpecs))
	for i := range dispatchWrapperSpecs {
		m[dispatchWrapperSpecs[i].name] = &dispatchWrapperSpecs[i]
	}
	return m
}()

// UnwrapKnownDispatchWrapperInvocation attempts to peel one dispatch wrapper layer.
func UnwrapKnownDispatchWrapperInvocation(argv []string) DispatchWrapperUnwrapResult {
	token0 := strings.TrimSpace(safeIndex(argv, 0))
	if token0 == "" {
		return DispatchWrapperUnwrapResult{Kind: "not-wrapper"}
	}
	wrapper := NormalizeExecutableToken(token0)
	spec := dispatchSpecByName[wrapper]
	if spec == nil {
		return DispatchWrapperUnwrapResult{Kind: "not-wrapper"}
	}
	if spec.unwrap == nil {
		return DispatchWrapperUnwrapResult{Kind: "blocked", Wrapper: wrapper}
	}
	unwrapped := spec.unwrap(argv)
	if unwrapped == nil {
		return DispatchWrapperUnwrapResult{Kind: "blocked", Wrapper: wrapper}
	}
	return DispatchWrapperUnwrapResult{Kind: "unwrapped", Wrapper: wrapper, Argv: unwrapped}
}

// HasDispatchEnvManipulation checks if an env wrapper modifies the environment.
func HasDispatchEnvManipulation(argv []string) bool {
	r := UnwrapKnownDispatchWrapperInvocation(argv)
	return r.Kind == "unwrapped" && r.Wrapper == "env" && envInvocationUsesModifiers(argv)
}
