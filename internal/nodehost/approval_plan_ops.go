package nodehost

import "strings"

// --- Wrapper unwrapping for mutable operand detection ---

const maxMutableOperandUnwrapDepth = 12 // safety bound for combined unwrapping chain

func unwrapArgvForMutableOperand(argv []string) (unwrapped []string, baseIndex int) {
	current := argv
	base := 0
	for range maxMutableOperandUnwrapDepth {
		dispatch := UnwrapKnownDispatchWrapperInvocation(current)
		if dispatch.Kind == "unwrapped" {
			base += len(current) - len(dispatch.Argv)
			current = dispatch.Argv
			continue
		}
		mux := UnwrapKnownShellMultiplexerInvocation(current)
		if mux.Kind == "unwrapped" {
			base += len(current) - len(mux.Argv)
			current = mux.Argv
			continue
		}
		pm := unwrapPackageManagerExec(current)
		if pm != nil {
			base += len(current) - len(pm)
			current = pm
			continue
		}
		return current, base
	}
	return current, base
}

// --- Package manager unwrapping ---

func normalizePackageManagerExecToken(token string) string {
	n := NormalizeExecutableToken(token)
	// Strip .js/.cjs/.mjs suffixes for shim detection.
	for _, suffix := range []string{".js", ".cjs", ".mjs"} {
		if strings.HasSuffix(strings.ToLower(n), suffix) {
			return n[:len(n)-len(suffix)]
		}
	}
	return n
}

func unwrapPackageManagerExec(argv []string) []string {
	exe := normalizePackageManagerExecToken(safeIndex(argv, 0))
	switch exe {
	case "npm":
		return unwrapNpmExec(argv)
	case "npx", "bunx":
		return unwrapDirectPackageExec(argv)
	case "pnpm":
		return unwrapPnpmExec(argv)
	default:
		return nil
	}
}

func unwrapPnpmExec(argv []string) []string {
	idx := 1
	for idx < len(argv) {
		token := strings.TrimSpace(safeIndex(argv, idx))
		if token == "" {
			idx++
			continue
		}
		if token == "--" {
			idx++
			continue
		}
		if !strings.HasPrefix(token, "-") {
			if token == "exec" {
				if idx+1 >= len(argv) {
					return nil
				}
				tail := argv[idx+1:]
				if len(tail) > 0 && tail[0] == "--" {
					if len(tail) > 1 {
						return tail[1:]
					}
					return nil
				}
				return tail
			}
			if token == "node" {
				tail := argv[idx+1:]
				if len(tail) > 0 && tail[0] == "--" {
					tail = tail[1:]
				}
				return append([]string{"node"}, tail...)
			}
			return nil
		}
		flag := strings.SplitN(strings.ToLower(token), "=", 2)[0]
		if pnpmOptionsWithValue[flag] {
			if strings.Contains(token, "=") {
				idx++
			} else {
				idx += 2
			}
			continue
		}
		if pnpmFlagOptions[flag] {
			idx++
			continue
		}
		return nil
	}
	return nil
}

func unwrapDirectPackageExec(argv []string) []string {
	idx := 1
	for idx < len(argv) {
		token := strings.TrimSpace(safeIndex(argv, idx))
		if token == "" {
			idx++
			continue
		}
		if !strings.HasPrefix(token, "-") {
			return argv[idx:]
		}
		flag := strings.SplitN(strings.ToLower(token), "=", 2)[0]
		if flag == "-c" || flag == "--call" {
			return nil
		}
		if npmExecOptionsWithValue[flag] {
			if strings.Contains(token, "=") {
				idx++
			} else {
				idx += 2
			}
			continue
		}
		if npmExecFlagOptions[flag] {
			idx++
			continue
		}
		return nil
	}
	return nil
}

func unwrapNpmExec(argv []string) []string {
	idx := 1
	for idx < len(argv) {
		token := strings.TrimSpace(safeIndex(argv, idx))
		if token == "" {
			idx++
			continue
		}
		if !strings.HasPrefix(token, "-") {
			if token != "exec" {
				return nil
			}
			idx++
			break
		}
		if (token == "-C" || token == "--prefix" || token == "--userconfig") && !strings.Contains(token, "=") {
			idx += 2
			continue
		}
		idx++
	}
	if idx >= len(argv) {
		return nil
	}
	tail := argv[idx:]
	if len(tail) > 0 && tail[0] == "--" {
		if len(tail) > 1 {
			return tail[1:]
		}
		return nil
	}
	return unwrapDirectPackageExec(append([]string{"npx"}, tail...))
}

// --- Mutable file operand detection ---

func resolveMutableFileOperandIndex(argv []string, cwd string) *int {
	unwrapped, baseIdx := unwrapArgvForMutableOperand(argv)
	exe := NormalizeExecutableToken(safeIndex(unwrapped, 0))
	if exe == "" {
		return nil
	}

	// POSIX shell script operand.
	if PosixShellWrappers[exe] {
		idx := resolvePosixShellScriptOperandIndex(unwrapped)
		if idx == nil {
			return nil
		}
		v := baseIdx + *idx
		return &v
	}

	// Standard argv[1] interpreters.
	for _, p := range mutableArgv1InterpreterPatterns {
		if p.MatchString(exe) {
			operand := strings.TrimSpace(safeIndex(unwrapped, 1))
			if operand != "" && operand != "-" && !strings.HasPrefix(operand, "-") {
				v := baseIdx + 1
				return &v
			}
		}
	}

	// Bun.
	if exe == "bun" {
		if idx := resolveBunScriptOperandIndex(unwrapped, cwd); idx != nil {
			v := baseIdx + *idx
			return &v
		}
	}

	// Deno.
	if exe == "deno" {
		if idx := resolveDenoRunScriptOperandIndex(unwrapped, cwd); idx != nil {
			v := baseIdx + *idx
			return &v
		}
	}

	// Unsafe flag checks.
	if exe == "ruby" && hasRubyUnsafeFlag(unwrapped) {
		return nil
	}
	if exe == "perl" && hasPerlUnsafeFlag(unwrapped) {
		return nil
	}

	if !isMutableScriptRunner(exe) {
		return nil
	}

	var optionsWithFileValue map[string]bool
	if exe == "node" || exe == "nodejs" {
		optionsWithFileValue = nodeOptionsWithFileValue
	}
	idx := resolveGenericInterpreterScriptOperandIndex(unwrapped, cwd, optionsWithFileValue)
	if idx == nil {
		return nil
	}
	v := baseIdx + *idx
	return &v
}
