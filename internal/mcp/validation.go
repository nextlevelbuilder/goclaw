package mcp

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// Allowed commands for stdio transport (basename only).
// This is a restrictive allowlist — only well-known runtimes are permitted.
var allowedCommands = map[string]bool{
	"node": true, "npx": true, "npm": true,
	"python": true, "python3": true, "python2": true,
	"ruby": true, "go": true, "cargo": true,
	"java": true, "dotnet": true, "php": true,
	"uvx": true, "uv": true, "pipx": true,
	"deno": true, "bun": true,
}

// Shell metacharacters that indicate injection attempt.
var shellMetaChars = regexp.MustCompile(`[;|&$` + "`" + `(){}[\]<>]`)

// Dangerous arg flags that enable code execution.
var dangerousArgPatterns = []string{
	"--eval", "-e", "-c", // Code execution flags
	"--require", "-r", // Module injection
	"--import",       // ES module injection
	"exec(", "eval(", // Inline code
	"__import__",    // Python import injection
	"child_process", // Node.js process spawning
	"subprocess",    // Python subprocess
}

// Fail-closed env var allowlist — only these are permitted for env: resolution.
var allowedEnvVars = map[string]bool{
	"HOME": true, "USER": true, "PATH": true,
	"SHELL": true, "LANG": true, "LC_ALL": true,
	"TZ": true, "TERM": true,
	"NODE_ENV": true, "ENVIRONMENT": true,
	"LOG_LEVEL": true, "DEBUG": true,
}

// ValidateCommand checks stdio command for injection vulnerabilities.
// Returns nil if the command is safe, or an error describing the issue.
func ValidateCommand(cmd string) error {
	if cmd == "" {
		return nil // Empty is valid (not stdio)
	}

	// Trim whitespace and check for empty
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return fmt.Errorf("command is empty or whitespace only")
	}

	// Check for shell metacharacters
	if shellMetaChars.MatchString(cmd) {
		return fmt.Errorf("command contains shell metacharacters")
	}

	// Check for path traversal
	if strings.Contains(cmd, "..") {
		return fmt.Errorf("command contains path traversal")
	}

	// Check for newline injection
	if strings.ContainsAny(cmd, "\n\r") {
		return fmt.Errorf("command contains newline characters")
	}

	basename := commandBasename(cmd)

	// Only bare runtime names are accepted. Path-bearing commands can point at
	// workspace-controlled wrappers named after an allowlisted runtime.
	if strings.ContainsAny(cmd, `/\`) {
		return fmt.Errorf("command must be a bare allowlisted runtime name, not a path")
	}

	// Bare command must be in allowlist
	if !allowedCommands[basename] {
		return fmt.Errorf("command %q not in allowlist (allowed: %s)", basename, allowedCommandNames())
	}
	return nil
}

// ValidateArgs checks command arguments for dangerous patterns.
func ValidateArgs(args []string) error {
	for i, arg := range args {
		argLower := strings.ToLower(arg)
		for _, pattern := range dangerousArgPatterns {
			if strings.Contains(argLower, pattern) {
				return fmt.Errorf("arg[%d] contains dangerous pattern %q", i, pattern)
			}
		}
		// Check for shell metacharacters in args
		if shellMetaChars.MatchString(arg) {
			return fmt.Errorf("arg[%d] contains shell metacharacters", i)
		}
	}
	return nil
}

// ValidateArgsForCommand applies command-specific stdio restrictions after the
// generic argument scan. Several allowed runtimes also include package runners
// or remote loaders; those execution modes are blocked for untrusted MCP config.
func ValidateArgsForCommand(command string, args []string) error {
	if err := ValidateArgs(args); err != nil {
		return err
	}

	switch commandBasename(command) {
	case "node":
		return validateNodeArgs(args)
	case "python", "python2", "python3":
		return validatePythonArgs(args)
	case "deno":
		return validateDenoArgs(args)
	case "bun":
		return validateBunArgs(args)
	case "npx", "npm", "uvx", "uv", "pipx":
		return validatePackageRunnerArgs(commandBasename(command), args)
	case "go":
		return validateGoArgs(args)
	case "cargo":
		return validateCargoArgs(args)
	case "dotnet":
		return validateDotnetArgs(args)
	default:
		return nil
	}
}

func commandBasename(command string) string {
	base := strings.TrimSpace(command)
	if idx := strings.LastIndexAny(base, `/\`); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	return base
}

func allowedCommandNames() string {
	names := make([]string, 0, len(allowedCommands))
	for name := range allowedCommands {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func validateNodeArgs(args []string) error {
	for i, arg := range args {
		if isFlag(arg, "-p", "--print", "--loader", "--experimental-loader") {
			return fmt.Errorf("arg[%d] uses blocked node execution flag %q", i, arg)
		}
	}
	return nil
}

func validatePythonArgs(args []string) error {
	for i, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "-m" || strings.HasPrefix(lower, "-m") && lower != "--" {
			return fmt.Errorf("arg[%d] uses blocked python module execution flag %q", i, arg)
		}
	}
	return nil
}

func validateDenoArgs(args []string) error {
	if err := validateRuntimeRemoteRefs(args); err != nil {
		return err
	}
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "add", "install":
			return fmt.Errorf("arg[%d] uses blocked deno package subcommand %q", i, args[i])
		}
	}
	return nil
}

func validateBunArgs(args []string) error {
	if err := validateRuntimeRemoteRefs(args); err != nil {
		return err
	}
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "x", "create", "add", "install", "update", "upgrade":
			return fmt.Errorf("arg[%d] uses blocked bun package subcommand %q", i, args[i])
		}
	}
	return nil
}

func validateRuntimeRemoteRefs(args []string) error {
	for i, arg := range args {
		if isRemoteCodeReference(arg) {
			return fmt.Errorf("arg[%d] uses blocked remote code reference %q", i, arg)
		}
	}
	return nil
}

func validatePackageRunnerArgs(command string, args []string) error {
	switch command {
	case "npx":
		return validatePackageTargetArgs(command, args, []string{"--package", "-p", "--call"})
	case "uvx":
		return validatePackageTargetArgs(command, args, []string{"--from", "--with", "--with-editable", "--with-requirements"})
	case "npm":
		return validateNPMArgs(args)
	case "pipx":
		return validatePipxArgs(args)
	case "uv":
		return validateUVArgs(args)
	default:
		return nil
	}
}

func validatePackageTargetArgs(command string, args []string, blockedFlags []string) error {
	for i, arg := range args {
		if isFlag(arg, blockedFlags...) {
			return fmt.Errorf("arg[%d] uses blocked %s package flag %q", i, command, arg)
		}
		if isOptionArg(arg) {
			continue
		}
		if !isLikelyLocalPath(arg) {
			return fmt.Errorf("arg[%d] uses blocked %s package target %q", i, command, arg)
		}
	}
	return nil
}

func validateNPMArgs(args []string) error {
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "exec", "x", "run", "run-script", "create", "install", "i", "add", "update", "upgrade":
			return fmt.Errorf("arg[%d] uses blocked npm subcommand %q", i, args[i])
		}
	}
	return nil
}

func validatePipxArgs(args []string) error {
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "run", "install", "inject", "upgrade", "upgrade-all", "runpip":
			return fmt.Errorf("arg[%d] uses blocked pipx subcommand %q", i, args[i])
		}
	}
	return nil
}

func validateUVArgs(args []string) error {
	sawTool := false
	sawPip := false
	for i, arg := range args {
		if isFlag(arg, "--with", "--with-editable", "--with-requirements", "--from") {
			return fmt.Errorf("arg[%d] uses blocked uv package flag %q", i, arg)
		}
		lower := strings.ToLower(strings.TrimSpace(arg))
		if lower == "" || isOptionArg(lower) {
			continue
		}
		switch {
		case lower == "tool":
			sawTool = true
			continue
		case lower == "pip":
			sawPip = true
			continue
		case sawTool && (lower == "run" || lower == "install" || lower == "upgrade"):
			return fmt.Errorf("arg[%d] uses blocked uv tool subcommand %q", i, args[i])
		case sawPip && (lower == "install" || lower == "sync"):
			return fmt.Errorf("arg[%d] uses blocked uv pip subcommand %q", i, args[i])
		case lower == "add" || lower == "sync":
			return fmt.Errorf("arg[%d] uses blocked uv package subcommand %q", i, args[i])
		}
	}
	return nil
}

func validateGoArgs(args []string) error {
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "run", "install", "get":
			return fmt.Errorf("arg[%d] uses blocked go package subcommand %q", i, args[i])
		}
	}
	return nil
}

func validateCargoArgs(args []string) error {
	for i, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "run", "install", "add", "update":
			return fmt.Errorf("arg[%d] uses blocked cargo package subcommand %q", i, args[i])
		}
	}
	return nil
}

func validateDotnetArgs(args []string) error {
	sawTool := false
	sawAdd := false
	for i, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if lower == "" || isOptionArg(lower) {
			continue
		}
		switch {
		case lower == "run", lower == "restore":
			return fmt.Errorf("arg[%d] uses blocked dotnet subcommand %q", i, args[i])
		case lower == "tool":
			sawTool = true
			continue
		case sawTool && (lower == "install" || lower == "update" || lower == "run"):
			return fmt.Errorf("arg[%d] uses blocked dotnet tool subcommand %q", i, args[i])
		case lower == "add":
			sawAdd = true
			continue
		case sawAdd && lower == "package":
			return fmt.Errorf("arg[%d] uses blocked dotnet package subcommand %q", i, args[i])
		}
	}
	return nil
}

func isFlag(arg string, names ...string) bool {
	lower := strings.ToLower(strings.TrimSpace(arg))
	for _, name := range names {
		if lower == name || strings.HasPrefix(lower, name+"=") {
			return true
		}
	}
	return false
}

func isOptionArg(arg string) bool {
	return strings.HasPrefix(strings.TrimSpace(arg), "-")
}

func isLikelyLocalPath(arg string) bool {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, `.\`) ||
		strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, `\`) {
		return true
	}
	return len(arg) >= 3 && arg[1] == ':' && (arg[2] == '\\' || arg[2] == '/')
}

func isRemoteCodeReference(arg string) bool {
	arg = strings.TrimSpace(arg)
	if isLikelyLocalPath(arg) {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(arg))
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		return false
	}
	if len(u.Scheme) == 1 && len(arg) >= 2 && arg[1] == ':' {
		return false
	}
	switch u.Scheme {
	case "http", "https", "npm", "jsr", "git", "git+https", "git+ssh", "ssh":
		return true
	default:
		return u.Host != ""
	}
}

// mcpAllowedHostsStore holds the operator-configured allowlist of MCP server
// hostnames that are exempt from the private-IP SSRF block during MCP config
// validation (see SetAllowedHosts).
var mcpAllowedHostsStore atomic.Pointer[map[string]bool]

// SetAllowedHosts configures the MCP server hostname allowlist consulted by
// ValidateURL / ValidateServerConfig. Hostnames are matched
// case-insensitively against the pre-resolution URL host. Call once at gateway
// startup; a nil/empty slice disables the allowlist (the default, no behavior
// change). Only owner/admin gateway config should feed this.
func SetAllowedHosts(hosts []string) {
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			m[h] = true
		}
	}
	mcpAllowedHostsStore.Store(&m)
}

// allowedHosts returns the current MCP hostname allowlist, or nil if unset.
func allowedHosts() map[string]bool {
	if p := mcpAllowedHostsStore.Load(); p != nil {
		return *p
	}
	return nil
}

// ValidateURL checks URL for SSRF vulnerabilities using the existing security package.
// This provides DNS rebinding protection via IP pinning.
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}

	// Reuse existing SSRF validation; an operator may allowlist trusted internal
	// MCP hosts (private network self-host) via SetAllowedHosts.
	_, _, err := security.ValidateAllowingHosts(rawURL, allowedHosts())
	if err != nil {
		return fmt.Errorf("URL validation failed: %w", err)
	}
	return nil
}

// ValidateAndResolveEnvVar checks and resolves env: prefix values.
// Uses FAIL-CLOSED approach: only allowlisted vars are permitted.
// Returns the resolved value or an error if the var is not allowed.
func ValidateAndResolveEnvVar(value string) (string, error) {
	after, ok := strings.CutPrefix(value, "env:")
	if !ok {
		return value, nil // Not an env var reference
	}

	varName := strings.ToUpper(after)

	// FAIL-CLOSED: only allowlisted vars permitted
	if !allowedEnvVars[varName] {
		return "", fmt.Errorf("env var %q not in allowlist (allowed: HOME, USER, PATH, SHELL, LANG, LC_ALL, TZ, TERM, NODE_ENV, ENVIRONMENT, LOG_LEVEL, DEBUG)", after)
	}

	return os.Getenv(after), nil
}

// ValidateServerConfig performs all validations for an MCP server configuration.
// This is a convenience function that validates command+args (for stdio) or URL (for HTTP transports).
func ValidateServerConfig(transport, command string, args []string, url string) error {
	if transport == "stdio" {
		if err := ValidateCommand(command); err != nil {
			return fmt.Errorf("invalid command: %w", err)
		}
		if err := ValidateArgsForCommand(command, args); err != nil {
			return fmt.Errorf("invalid args: %w", err)
		}
	}

	if transport == "sse" || transport == "streamable-http" {
		if err := ValidateURL(url); err != nil {
			return fmt.Errorf("invalid URL: %w", err)
		}
	}

	return nil
}

// ValidateHeaders validates header values for env: references.
// Returns an error if any header uses a non-allowlisted env var.
func ValidateHeaders(headers map[string]string) error {
	for k, v := range headers {
		if _, err := ValidateAndResolveEnvVar(v); err != nil {
			return fmt.Errorf("header %q: %w", k, err)
		}
	}
	return nil
}
