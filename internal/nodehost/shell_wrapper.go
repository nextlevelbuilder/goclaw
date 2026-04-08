package nodehost

import "strings"

// PosixShellWrappers is the set of POSIX shell executable names.
var PosixShellWrappers = newSet("ash", "bash", "dash", "fish", "ksh", "sh", "zsh")

var windowsCmdWrappers = newSet("cmd", "cmd.exe")
var powershellWrappers = newSet("powershell", "pwsh", "powershell.exe", "pwsh.exe")
var shellMultiplexerWrappers = newSet("busybox", "toybox")
var allShellWrappers = newSet("ash", "bash", "dash", "fish", "ksh", "sh", "zsh", "cmd", "powershell", "pwsh")

// ShellMultiplexerUnwrapResult is the result of attempting to unwrap a shell multiplexer.
type ShellMultiplexerUnwrapResult struct {
	Kind    string   // "not-wrapper", "blocked", "unwrapped"
	Wrapper string
	Argv    []string
}

// UnwrapKnownShellMultiplexerInvocation unwraps busybox/toybox → shell invocations.
func UnwrapKnownShellMultiplexerInvocation(argv []string) ShellMultiplexerUnwrapResult {
	token0 := strings.TrimSpace(safeIndex(argv, 0))
	if token0 == "" {
		return ShellMultiplexerUnwrapResult{Kind: "not-wrapper"}
	}
	wrapper := NormalizeExecutableToken(token0)
	if !shellMultiplexerWrappers[wrapper] {
		return ShellMultiplexerUnwrapResult{Kind: "not-wrapper"}
	}
	appletIdx := 1
	if strings.TrimSpace(safeIndex(argv, appletIdx)) == "--" {
		appletIdx++
	}
	applet := strings.TrimSpace(safeIndex(argv, appletIdx))
	if applet == "" || !IsShellWrapperExecutable(applet) {
		return ShellMultiplexerUnwrapResult{Kind: "blocked", Wrapper: wrapper}
	}
	unwrapped := argv[appletIdx:]
	if len(unwrapped) == 0 {
		return ShellMultiplexerUnwrapResult{Kind: "blocked", Wrapper: wrapper}
	}
	return ShellMultiplexerUnwrapResult{Kind: "unwrapped", Wrapper: wrapper, Argv: unwrapped}
}

// IsShellWrapperExecutable checks if a token names a known shell wrapper.
func IsShellWrapperExecutable(token string) bool {
	return allShellWrappers[NormalizeExecutableToken(token)]
}

// ShellWrapperCommand holds the result of extracting a shell wrapper payload.
type ShellWrapperCommand struct {
	IsWrapper bool
	Command   *string // the shell payload text
}

// ExtractShellWrapperCommand extracts the inline command from a shell wrapper invocation.
func ExtractShellWrapperCommand(argv []string, rawCommand *string) ShellWrapperCommand {
	return extractShellWrapperCommandInternal(argv, rawCommand, 0)
}

func extractShellWrapperCommandInternal(argv []string, rawCommand *string, depth int) ShellWrapperCommand {
	if depth > MaxDispatchWrapperDepth {
		return ShellWrapperCommand{}
	}
	token0 := strings.TrimSpace(safeIndex(argv, 0))
	if token0 == "" {
		return ShellWrapperCommand{}
	}

	// Try dispatch wrapper unwrap.
	dispatch := UnwrapKnownDispatchWrapperInvocation(argv)
	if dispatch.Kind == "blocked" {
		return ShellWrapperCommand{}
	}
	if dispatch.Kind == "unwrapped" {
		return extractShellWrapperCommandInternal(dispatch.Argv, rawCommand, depth+1)
	}

	// Try shell multiplexer unwrap.
	mux := UnwrapKnownShellMultiplexerInvocation(argv)
	if mux.Kind == "blocked" {
		return ShellWrapperCommand{}
	}
	if mux.Kind == "unwrapped" {
		return extractShellWrapperCommandInternal(mux.Argv, rawCommand, depth+1)
	}

	// Check if this is a shell wrapper itself.
	exe := NormalizeExecutableToken(token0)
	payload := extractShellPayload(argv, exe)
	if payload == nil {
		return ShellWrapperCommand{}
	}
	cmd := rawCommand
	if cmd == nil {
		cmd = payload
	}
	return ShellWrapperCommand{IsWrapper: true, Command: cmd}
}

// extractShellPayload extracts the inline command from a shell invocation.
func extractShellPayload(argv []string, exe string) *string {
	switch {
	case PosixShellWrappers[exe]:
		return extractPosixShellInlineCommand(argv)
	case windowsCmdWrappers[exe]:
		return extractCmdInlineCommand(argv)
	case powershellWrappers[exe]:
		return extractPowershellInlineCommand(argv)
	default:
		return nil
	}
}

func extractPosixShellInlineCommand(argv []string) *string {
	m := ResolveInlineCommandMatch(argv, PosixInlineCommandFlags, true)
	return m.Command
}

func extractCmdInlineCommand(argv []string) *string {
	for i, item := range argv {
		lower := strings.ToLower(strings.TrimSpace(item))
		if lower == "/c" || lower == "/k" {
			tail := argv[i+1:]
			if len(tail) == 0 {
				return nil
			}
			cmd := strings.TrimSpace(strings.Join(tail, " "))
			if cmd == "" {
				return nil
			}
			return &cmd
		}
	}
	return nil
}

func extractPowershellInlineCommand(argv []string) *string {
	psFlags := newSet("-c", "-command", "--command", "-f", "-file", "-encodedcommand", "-enc", "-e")
	m := ResolveInlineCommandMatch(argv, psFlags, false)
	return m.Command
}

// HasEnvManipulationBeforeShellWrapper checks if env modifies the environment
// before a shell wrapper in the argv chain.
func HasEnvManipulationBeforeShellWrapper(argv []string) bool {
	return hasEnvManipBeforeShellInternal(argv, 0, false)
}

func hasEnvManipBeforeShellInternal(argv []string, depth int, envSeen bool) bool {
	if depth > MaxDispatchWrapperDepth {
		return false
	}
	token0 := strings.TrimSpace(safeIndex(argv, 0))
	if token0 == "" {
		return false
	}
	dispatch := UnwrapKnownDispatchWrapperInvocation(argv)
	if dispatch.Kind == "blocked" {
		return false
	}
	if dispatch.Kind == "unwrapped" {
		nextEnvSeen := envSeen || HasDispatchEnvManipulation(argv)
		return hasEnvManipBeforeShellInternal(dispatch.Argv, depth+1, nextEnvSeen)
	}
	mux := UnwrapKnownShellMultiplexerInvocation(argv)
	if mux.Kind == "blocked" {
		return false
	}
	if mux.Kind == "unwrapped" {
		return hasEnvManipBeforeShellInternal(mux.Argv, depth+1, envSeen)
	}
	exe := NormalizeExecutableToken(token0)
	payload := extractShellPayload(argv, exe)
	if payload == nil {
		return false
	}
	return envSeen
}
