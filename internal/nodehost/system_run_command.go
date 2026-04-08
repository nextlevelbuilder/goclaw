package nodehost

import (
	"fmt"
	"strings"
)

// FormatExecCommand formats an argv slice into a shell-escaped command string.
func FormatExecCommand(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" {
			parts[i] = `""`
			continue
		}
		if strings.ContainsAny(arg, " \t\"") {
			parts[i] = fmt.Sprintf(`"%s"`, strings.ReplaceAll(arg, `"`, `\"`))
		} else {
			parts[i] = arg
		}
	}
	return strings.Join(parts, " ")
}

// ResolvedSystemRunCommand is the result of resolving a command request.
type ResolvedSystemRunCommand struct {
	Ok           bool
	Argv         []string
	CommandText  string
	ShellPayload *string
	PreviewText  *string
	Message      string // error message when !Ok
}

// posixOrPowershellInlineWrapperNames are shells whose inline commands get preview text.
var posixOrPowershellInlineWrapperNames = newSet(
	"ash", "bash", "dash", "fish", "ksh", "powershell", "pwsh", "sh", "zsh",
)

// buildSystemRunCommandDisplay resolves shell payload and preview text for an argv.
func buildSystemRunCommandDisplay(argv []string) (shellPayload *string, commandText string, previewText *string) {
	wrapper := ExtractShellWrapperCommand(argv, nil)
	shellPayload = wrapper.Command

	hasTrailingPositional := hasTrailingPositionalAfterInline(argv)
	envManip := wrapper.IsWrapper && HasEnvManipulationBeforeShellWrapper(argv)
	commandText = FormatExecCommand(argv)

	if shellPayload != nil && !envManip && !hasTrailingPositional {
		trimmed := strings.TrimSpace(*shellPayload)
		previewText = &trimmed
	}
	return
}

func hasTrailingPositionalAfterInline(argv []string) bool {
	wrapperArgv := unwrapShellWrapperArgv(argv)
	token0 := strings.TrimSpace(safeIndex(wrapperArgv, 0))
	if token0 == "" {
		return false
	}
	wrapper := NormalizeExecutableToken(token0)
	if !posixOrPowershellInlineWrapperNames[wrapper] {
		return false
	}

	var match InlineCommandMatch
	if wrapper == "powershell" || wrapper == "pwsh" {
		psFlags := newSet("-c", "-command", "--command", "-f", "-file", "-encodedcommand", "-enc", "-e")
		match = ResolveInlineCommandMatch(wrapperArgv, psFlags, false)
	} else {
		match = ResolveInlineCommandMatch(wrapperArgv, PosixInlineCommandFlags, true)
	}
	if match.ValueTokenIndex == nil {
		return false
	}
	for _, entry := range wrapperArgv[*match.ValueTokenIndex+1:] {
		if strings.TrimSpace(entry) != "" {
			return true
		}
	}
	return false
}

func unwrapShellWrapperArgv(argv []string) []string {
	current := unwrapDispatchWrappersForResolution(argv)
	mux := UnwrapKnownShellMultiplexerInvocation(current)
	if mux.Kind == "unwrapped" {
		return mux.Argv
	}
	return current
}

func unwrapDispatchWrappersForResolution(argv []string) []string {
	current := argv
	for range MaxDispatchWrapperDepth {
		r := UnwrapKnownDispatchWrapperInvocation(current)
		if r.Kind != "unwrapped" || len(r.Argv) == 0 {
			break
		}
		current = r.Argv
	}
	return current
}

// ResolveSystemRunCommandRequest validates and resolves a command request.
func ResolveSystemRunCommandRequest(command []string, rawCommand *string) ResolvedSystemRunCommand {
	raw := normalizeRawCommandText(rawCommand)

	if len(command) == 0 {
		if raw != nil {
			return ResolvedSystemRunCommand{Ok: false, Message: "rawCommand requires params.command"}
		}
		return ResolvedSystemRunCommand{Ok: true, CommandText: ""}
	}

	argv := make([]string, len(command))
	copy(argv, command)

	shellPayload, commandText, previewText := buildSystemRunCommandDisplay(argv)

	// Validate rawCommand consistency.
	if raw != nil {
		matchesArgv := *raw == commandText
		matchesPreview := previewText != nil && *raw == *previewText
		if !matchesArgv && !matchesPreview {
			return ResolvedSystemRunCommand{
				Ok:      false,
				Message: "INVALID_REQUEST: rawCommand does not match command",
			}
		}
	}

	return ResolvedSystemRunCommand{
		Ok:           true,
		Argv:         argv,
		CommandText:  commandText,
		ShellPayload: shellPayload,
		PreviewText:  previewText,
	}
}

func normalizeRawCommandText(raw *string) *string {
	if raw == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
