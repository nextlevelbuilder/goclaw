package mcp

import (
	"fmt"
	"os"
	"regexp"
	"strings"

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
	"--eval", "-e", "-c",      // Code execution flags
	"--require", "-r",         // Module injection
	"--import",                // ES module injection
	"exec(", "eval(",          // Inline code
	"__import__",              // Python import injection
	"child_process",           // Node.js process spawning
	"subprocess",              // Python subprocess
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

	// Extract basename for allowlist check
	basename := cmd
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		basename = cmd[idx+1:]
	}

	// Allow absolute paths to known commands
	if strings.HasPrefix(cmd, "/") {
		if !allowedCommands[basename] {
			return fmt.Errorf("command %q not in allowlist", basename)
		}
		return nil
	}

	// Bare command must be in allowlist
	if !allowedCommands[cmd] {
		return fmt.Errorf("command %q not in allowlist (allowed: node, npx, python, python3, ruby, go, java, uvx, uv, pipx, deno, bun)", cmd)
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

// ValidateURL checks URL for SSRF vulnerabilities using the existing security package.
// This provides DNS rebinding protection via IP pinning.
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}

	// Reuse existing SSRF validation with DNS rebinding protection
	_, _, err := security.Validate(rawURL)
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
		if err := ValidateArgs(args); err != nil {
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
