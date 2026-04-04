package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	shellwords "github.com/mattn/go-shellwords"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// shellOperatorPattern detects shell metacharacters that indicate command chaining.
// These are unsafe in credentialed mode because they allow reading injected env vars.
var shellOperatorPattern = regexp.MustCompile(`[;|&<>\n\r` + "`" + `]|\$\(|\$\{`)

// parseCommandBinary splits a command string into binary name and arguments.
// Uses shell-word parsing to correctly handle quoted arguments with spaces.
func parseCommandBinary(command string) (binary string, args []string, err error) {
	parser := shellwords.NewParser()
	parser.ParseBacktick = false
	parser.ParseEnv = false

	words, err := parser.Parse(command)
	if err != nil {
		return "", nil, fmt.Errorf("parse command: %w", err)
	}
	if len(words) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	return words[0], words[1:], nil
}

// detectShellOperators scans a raw command string for shell metacharacters.
// Returns the list of detected operators, or nil if the command is clean.
func detectShellOperators(command string) []string {
	matches := shellOperatorPattern.FindAllString(command, -1)
	if len(matches) == 0 {
		return nil
	}
	// Deduplicate
	seen := make(map[string]bool, len(matches))
	var unique []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			unique = append(unique, m)
		}
	}
	return unique
}

// resolveAndMatchBinary resolves a binary name to an absolute path and
// optionally verifies it matches the stored config path. This prevents
// binary spoofing (e.g. ./gh in workspace instead of /usr/bin/gh).
func resolveAndMatchBinary(binaryName string, configPath *string) (string, error) {
	absPath, err := exec.LookPath(binaryName)
	if err != nil {
		return "", fmt.Errorf("binary %q not found in PATH: %w", binaryName, err)
	}
	// If config specifies an absolute path, verify it matches
	if configPath != nil && *configPath != "" && absPath != *configPath {
		return "", fmt.Errorf("binary path mismatch: resolved %q but config expects %q", absPath, *configPath)
	}
	return absPath, nil
}

// matchesBinaryDeny checks if the joined args string matches any per-binary deny pattern.
// Returns the matched pattern string, or empty if allowed.
func matchesBinaryDeny(args []string, denyPatternsJSON json.RawMessage) string {
	if len(denyPatternsJSON) == 0 {
		return ""
	}
	var patterns []string
	if err := json.Unmarshal(denyPatternsJSON, &patterns); err != nil || len(patterns) == 0 {
		return ""
	}
	argsStr := strings.Join(args, " ")
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			slog.Warn("secure_cli.invalid_deny_pattern", "pattern", p, "error", err)
			continue
		}
		if re.MatchString(argsStr) {
			return p
		}
	}
	return ""
}

// executeCredentialed runs a CLI command in Direct Exec Mode (no shell).
// Credentials are injected as env vars into the child process only.
func (t *ExecTool) executeCredentialed(ctx context.Context, cred *store.SecureCLIBinary,
	binary string, args []string, cwd string, sandboxKey string) *Result {

	// Step 0: Reject NUL bytes (defense-in-depth — also checked in Execute()).
	rawCommand := binary + " " + strings.Join(args, " ")
	if strings.ContainsRune(rawCommand, '\x00') {
		return ErrorResult("command contains invalid NUL byte")
	}

	// Step 1: Check for shell operators (early detection for clear error)
	if ops := detectShellOperators(rawCommand); len(ops) > 0 {
		return credentialedShellOperatorError(rawCommand, ops)
	}

	// Step 2: Resolve binary to absolute path and verify against config
	absPath, err := resolveAndMatchBinary(binary, cred.BinaryPath)
	if err != nil {
		r := credentialedPathError(binary, err)
		if t.sandboxMgr != nil && sandboxKey != "" {
			r.ForLLM += hintBinaryNotFound
		}
		return r
	}

	// Step 3: Per-binary deny check (deny_args)
	if p := matchesBinaryDeny(args, cred.DenyArgs); p != "" {
		return credentialedDenyError(binary, args, p)
	}
	// Per-binary verbose deny check (deny_verbose)
	if p := matchesBinaryDeny(args, cred.DenyVerbose); p != "" {
		return credentialedDenyError(binary, args, p)
	}

	// Step 4: Decrypt env vars from store (already decrypted by store layer)
	envMap := make(map[string]string)
	if len(cred.EncryptedEnv) > 0 {
		if err := json.Unmarshal(cred.EncryptedEnv, &envMap); err != nil {
			return ErrorResult(fmt.Sprintf("credentialed exec: invalid env JSON for %q: %v", binary, err))
		}
	}

	// Step 4b: Merge per-user env overrides (user takes priority over base)
	if len(cred.UserEnv) > 0 {
		var userEnvMap map[string]string
		if err := json.Unmarshal(cred.UserEnv, &userEnvMap); err == nil {
			maps.Copy(envMap, userEnvMap)
		}
	}

	// Step 5: Register credential values for output scrubbing
	for _, v := range envMap {
		AddCredentialScrubValues(v)
	}

	// Step 6: Determine timeout
	timeout := time.Duration(cred.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Step 7: Execute — sandbox or host
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeCredentialedSandbox(ctx, absPath, args, cwd, sandboxKey, envMap, timeout)
	}
	return t.executeCredentialedHost(ctx, absPath, args, cwd, envMap, timeout)
}

// executeCredentialedHost runs a credentialed command directly on the host.
// Uses exec.Command (no shell) with credentials as env vars.
func (t *ExecTool) executeCredentialedHost(ctx context.Context, absPath string, args []string,
	cwd string, envMap map[string]string, timeout time.Duration) *Result {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, absPath, args...)
	cmd.Dir = cwd

	// Build env: inherit minimal PATH + HOME, add credentials
	cmd.Env = buildCredentialedEnv(envMap)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return formatCredentialedResult(absPath, args, stdout.String(), stderr.String(), err, ctx, timeout)
}

// executeCredentialedSandbox runs a credentialed command inside a Docker sandbox.
// Uses sandbox.WithEnv to inject credentials via docker exec -e (no shell).
func (t *ExecTool) executeCredentialedSandbox(ctx context.Context, absPath string, args []string,
	cwd string, sandboxKey string, envMap map[string]string, timeout time.Duration) *Result {

	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		slog.Warn("security.credentialed_exec_sandbox_unavailable",
			"binary", absPath, "error", err)
		return ErrorResult("credentialed exec requires sandbox but sandbox is unavailable: " + err.Error())
	}

	// Direct exec inside sandbox: [absPath, args...] with env injection
	command := append([]string{absPath}, args...)
	result, err := sb.Exec(ctx, command, cwd, sandbox.WithEnv(envMap))
	if err != nil {
		return ErrorResult(fmt.Sprintf("credentialed sandbox exec: %v", err))
	}

	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += "STDERR:\n" + result.Stderr
	}
	if result.ExitCode != 0 {
		scrubbed := ScrubCredentials(output)
		return credentialedExecFailError(absPath, args, result.ExitCode, scrubbed+MaybeSandboxHint(result.ExitCode, scrubbed))
	}
	if output == "" {
		output = "(command completed with no output)"
	}
	output = ScrubCredentials(output)
	output = capExecOutput(output, execMaxOutputChars)
	return SilentResult(output)
}

// buildCredentialedEnv creates a minimal environment with injected credentials.
// Inherits PATH and HOME from parent process, adds credential env vars.
func buildCredentialedEnv(envMap map[string]string) []string {
	env := []string{
		"PATH=" + getenvDefault("PATH", "/usr/local/bin:/usr/bin:/bin"),
		"HOME=" + getenvDefault("HOME", "/tmp"),
		"LANG=" + getenvDefault("LANG", "en_US.UTF-8"),
		"USER=" + getenvDefault("USER", "goclaw"),
	}
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	return env
}

// formatCredentialedResult formats the output of a credentialed exec call.
func formatCredentialedResult(binary string, args []string,
	stdout, stderr string, err error, ctx context.Context, timeout time.Duration) *Result {

	var output string
	if stdout != "" {
		output = stdout
	}
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += "STDERR:\n" + stderr
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ErrorResult(fmt.Sprintf("[CREDENTIALED EXEC] Command timed out after %s.\nBinary: %s", timeout, binary))
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return credentialedExecFailError(binary, args, exitCode, ScrubCredentials(output))
	}

	if output == "" {
		output = "(command completed with no output)"
	}
	output = ScrubCredentials(output)
	output = capExecOutput(output, execMaxOutputChars)
	return SilentResult(output)
}

// lookupCredentialedBinary checks if a command's binary has credential config.
// Returns the credential config and parsed args, or nil if not credentialed.
func (t *ExecTool) lookupCredentialedBinary(ctx context.Context, command string) (*store.SecureCLIBinary, string, []string) {
	if t.secureCLIStore == nil {
		slog.Warn("secure_cli.lookup: store is nil, skipping credentialed exec", "command", command)
		return nil, "", nil
	}
	binary, args, err := parseCommandBinary(command)
	if err != nil {
		return nil, "", nil
	}
	// Get agent ID from context for scoped lookup
	agentID := store.AgentIDFromContext(ctx)
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}
	// Pass userID for per-user credential resolution (LEFT JOIN, zero extra queries).
	userID := store.UserIDFromContext(ctx)
	cred, err := t.secureCLIStore.LookupByBinary(ctx, binary, agentIDPtr, userID)
	if err != nil {
		slog.Warn("secure_cli.lookup: query failed", "binary", binary, "agent_id", agentID, "error", err)
		return nil, "", nil
	}
	if cred == nil {
		slog.Warn("secure_cli.lookup: no credential found", "binary", binary, "agent_id", agentID)
		return nil, "", nil
	}
	slog.Info("secure_cli.lookup: found credential", "binary", binary, "cred_id", cred.ID, "env_size", len(cred.EncryptedEnv))
	return cred, binary, args
}

// getenvDefault returns the value of an env var, or a default if not set.
func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- Structured error helpers ---

func credentialedShellOperatorError(command string, ops []string) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Shell operators not supported.\n"+
			"Detected: %s\n"+
			"This CLI runs in Direct Exec Mode — no shell operators (;  &&  ||  |  >  <  $()  ``).\n"+
			"Run the command without operators. Use --json or --format=json for structured output.",
			strings.Join(ops, ", ")),
		ForUser: "Command contains shell operators not supported in credentialed mode.",
		IsError: true,
	}
}

func credentialedPathError(binary string, err error) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Binary resolution failed.\n"+
			"Binary: %s\nError: %v\n"+
			"The binary may not be installed or the path doesn't match the configured path.",
			binary, err),
		ForUser: fmt.Sprintf("CLI binary %q not found or path mismatch.", binary),
		IsError: true,
	}
}

func credentialedDenyError(binary string, args []string, pattern string) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Command blocked by security policy.\n"+
			"Binary: %s\nArgs: %s\nMatched deny pattern: %s\n"+
			"This operation requires admin approval and cannot be performed automatically.",
			binary, strings.Join(args, " "), pattern),
		ForUser: fmt.Sprintf("Operation '%s %s' is blocked by security policy.", binary, strings.Join(args, " ")),
		IsError: true,
	}
}

func credentialedExecFailError(binary string, args []string, exitCode int, output string) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Command failed (exit code %d).\n"+
			"Binary: %s\nArgs: %s\n"+
			"Note: This runs in Direct Exec Mode — shell operators are NOT supported.\n"+
			"If you used shell operators, remove them and try again.\n\n%s",
			exitCode, binary, strings.Join(args, " "), output),
		ForUser: fmt.Sprintf("Command failed with exit code %d.", exitCode),
		IsError: true,
	}
}
