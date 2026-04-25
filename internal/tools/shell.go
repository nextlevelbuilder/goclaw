package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"golang.org/x/text/unicode/norm"
)

// Dangerous command patterns organized into configurable deny groups.
// Defense-in-depth: patterns complement Docker hardening (cap-drop ALL,
// no-new-privileges, pids-limit, memory limit).
// Sources: OWASP Agentic AI Top 10, Claude Code CVE-2025-66032, MITRE ATT&CK,
// PayloadsAllTheThings, Trail of Bits prompt-injection-to-RCE research.
// Groups and patterns defined in shell_deny_groups.go.

// DefaultDenyPatterns returns all patterns from groups where Default=true.
// Backward-compatible wrapper for code that doesn't use per-agent overrides.
func DefaultDenyPatterns() []*regexp.Regexp {
	return ResolveDenyPatterns(nil)
}

// ExecTool executes shell commands, optionally inside a sandbox container.
type ExecTool struct {
	workspace        string
	timeout          time.Duration
	pathDenyPatterns []*regexp.Regexp // always-on path-based denials (DenyPaths)
	pathDenyRoots    []string         // raw deny roots for nested workspace exemptions
	denyExemptions   []string         // substrings that exempt a command from deny
	restrict         bool
	sandboxMgr       sandbox.Manager      // nil = no sandbox, execute on host
	approvalMgr      *ExecApprovalManager // nil = no approval needed
	agentID          string               // for approval request context
	secureCLIStore   store.SecureCLIStore // nil = no credentialed exec
	// globalDenyGroups holds global shell deny-group toggles from config.tools.
	// Per-agent overrides from context (store.WithShellDenyGroups) win per-key.
	// Updated at startup and via TopicConfigChanged pub/sub for runtime reload.
	globalDenyGroups map[string]bool
}

// SetGlobalShellDenyGroups replaces the global shell deny-group toggles. The
// caller's map is defensively copied so later mutations cannot leak into the
// tool's internal state. Passing nil or an empty map clears the global config
// (per-agent context overrides, if any, still apply on their own).
func (t *ExecTool) SetGlobalShellDenyGroups(groups map[string]bool) {
	if len(groups) == 0 {
		t.globalDenyGroups = nil
		return
	}
	cp := make(map[string]bool, len(groups))
	for k, v := range groups {
		cp[k] = v
	}
	t.globalDenyGroups = cp
}

// effectiveDenyGroups merges the per-agent context override with the global
// config. Precedence: per-agent context (per-key) > global. When one side is
// empty, the other is returned directly (no allocation).
func (t *ExecTool) effectiveDenyGroups(ctx context.Context) map[string]bool {
	agent := store.ShellDenyGroupsFromContext(ctx)
	if len(t.globalDenyGroups) == 0 {
		return agent
	}
	if len(agent) == 0 {
		return t.globalDenyGroups
	}
	merged := make(map[string]bool, len(t.globalDenyGroups)+len(agent))
	for k, v := range t.globalDenyGroups {
		merged[k] = v
	}
	for k, v := range agent {
		merged[k] = v // agent wins per-key
	}
	return merged
}

// EffectiveDenyGroupsForTest exposes effectiveDenyGroups for cross-package tests
// (e.g. cmd pub/sub regression). Not for production callers.
func (t *ExecTool) EffectiveDenyGroupsForTest(ctx context.Context) map[string]bool {
	return t.effectiveDenyGroups(ctx)
}

// NewExecTool creates an exec tool that runs commands directly on the host.
func NewExecTool(workspace string, restrict bool) *ExecTool {
	return &ExecTool{
		workspace: workspace,
		timeout:   60 * time.Second,
		restrict:  restrict,
	}
}

// NewSandboxedExecTool creates an exec tool that routes commands through a sandbox container.
func NewSandboxedExecTool(workspace string, restrict bool, mgr sandbox.Manager) *ExecTool {
	return &ExecTool{
		workspace:  workspace,
		timeout:    300 * time.Second, // sandbox allows longer timeout
		restrict:   restrict,
		sandboxMgr: mgr,
	}
}

// SetSandboxKey is a no-op; sandbox key is now read from ctx (thread-safe).
func (t *ExecTool) SetSandboxKey(key string) {}

// DenyPaths adds always-on deny patterns that block commands referencing the given paths.
// These are NOT configurable via deny groups — they always apply regardless of group config.
func (t *ExecTool) DenyPaths(paths ...string) {
	for _, p := range paths {
		escaped := regexp.QuoteMeta(p)
		t.pathDenyPatterns = append(t.pathDenyPatterns, regexp.MustCompile(escaped))
		t.pathDenyRoots = append(t.pathDenyRoots, p)
	}
}

// AllowPathExemptions adds path prefixes that exempt a command from deny pattern matches.
// Each shell argument is checked individually — commands like "cat .goclaw/skills-store/tool.py"
// are exempt because the argument ".goclaw/skills-store/tool.py" starts with the prefix.
func (t *ExecTool) AllowPathExemptions(prefixes ...string) {
	t.denyExemptions = append(t.denyExemptions, prefixes...)
}

// normalizeCommand applies NFKC Unicode normalization and strips zero-width
// characters before deny pattern matching, preventing Unicode-based bypasses.
func normalizeCommand(s string) string {
	// NFKC normalization: folds compatibility characters (e.g. fullwidth letters)
	s = norm.NFKC.String(s)
	// Strip zero-width characters that are invisible but can fragment tokens
	s = strings.NewReplacer(
		"\u200b", "", // zero-width space
		"\u200c", "", // zero-width non-joiner
		"\u200d", "", // zero-width joiner
		"\u2060", "", // word joiner
		"\ufeff", "", // BOM / zero-width no-break space
	).Replace(s)
	return s
}

// SetApprovalManager sets the exec approval manager for this tool.
func (t *ExecTool) SetApprovalManager(mgr *ExecApprovalManager, agentID string) {
	t.approvalMgr = mgr
	t.agentID = agentID
}

// SetSecureCLIStore sets the credential store for credentialed exec.
func (t *ExecTool) SetSecureCLIStore(s store.SecureCLIStore) {
	t.secureCLIStore = s
}

// HasSecureCLIStore reports whether a credential store is wired.
// Intended for wiring-check tests that verify subagent ExecTools also enforce
// the secure-CLI gate (Red Team F3).
func (t *ExecTool) HasSecureCLIStore() bool {
	return t.secureCLIStore != nil
}

func (t *ExecTool) Name() string        { return "exec" }
func (t *ExecTool) Description() string { return "Execute a shell command and return its output" }
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]any{
				"type":        "string",
				"description": "Working directory for the command (default: workspace root)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]any) *Result {
	command, _ := args["command"].(string)
	if command == "" {
		return ErrorResult("command is required")
	}

	// Reject NUL bytes — they cause silent shell truncation enabling injection.
	if strings.ContainsRune(command, '\x00') {
		return ErrorResult("command contains invalid NUL byte")
	}

	// Normalize command before all deny checks: NFKC + zero-width strip prevents
	// Unicode-based pattern bypass while preserving functional command content.
	normalizedCommand := normalizeCommand(command)

	// Resolve deny patterns: merge per-agent context overrides with global
	// config (per-key agent precedence), fallback to all registry defaults.
	denyOverrides := t.effectiveDenyGroups(ctx)
	groupPatterns := ResolveDenyPatterns(denyOverrides)

	// Also resolve package_install patterns separately for approval routing.
	var pkgInstallPatterns []*regexp.Regexp
	if pkgGroup, ok := DenyGroupRegistry["package_install"]; ok && IsGroupDenied(denyOverrides, "package_install") {
		pkgInstallPatterns = pkgGroup.Patterns
	}

	// Combine group-based patterns + always-on path denials.
	allPatterns := make([]*regexp.Regexp, 0, len(groupPatterns)+len(t.pathDenyPatterns))
	allPatterns = append(allPatterns, groupPatterns...)
	allPatterns = append(allPatterns, t.pathDenyPatterns...)
	exemptions := append([]string{}, t.denyExemptions...)
	exemptions = append(exemptions, t.dynamicPathExemptions(ctx)...)

	// Check for dangerous commands (applies to both host and sandbox).
	wordFields := parseExecCommandWords(normalizedCommand)
	pathBaseDir := ToolWorkspaceFromCtx(ctx)
	if pathBaseDir == "" {
		pathBaseDir = t.workspace
	}
	for _, pattern := range allPatterns {
		if pattern.MatchString(normalizedCommand) {
			// Check if exemption applies. Only exempt if EVERY field that
			// individually matches the deny pattern is covered by an exemption.
			// This prevents pipe/comment bypass: "cat /app/data/skills-store/x | cat /app/data/secret"
			// — the second field matches deny but has no exemption → denied.
			// Strips surrounding quotes (LLMs often quote paths) and rejects
			// path traversal ("..") to prevent exemption escape.
			exempt := false
			trimmed := strings.TrimSpace(normalizedCommand)
			fields := wordFields
			if len(fields) == 0 {
				fields = strings.Fields(trimmed)
			}
			matchingFields := 0
			exemptFields := 0
			for _, field := range fields {
				clean := strings.TrimSpace(field)
				if !pattern.MatchString(clean) {
					continue // field doesn't trigger this deny pattern
				}
				matchingFields++
				if matchesAnyPathExemption(clean, exemptions, pathBaseDir) {
					exemptFields++
				}
			}
			// Exempt only if at least one field matched AND all matched fields are exempt.
			if matchingFields > 0 && exemptFields == matchingFields {
				exempt = true
			}
			if exempt {
				continue
			}

			// Package install commands: route through approval flow instead of hard deny.
			// This lets agents "request permission" from admin to install packages.
			if t.approvalMgr != nil && matchesAny(normalizedCommand, pkgInstallPatterns) {
				slog.Info("exec: package install requires approval", "command", truncateCmd(command, 100), "agent", t.agentID)
				decision, err := t.approvalMgr.RequestApproval(command, t.agentID, 2*time.Minute)
				if err != nil {
					return ErrorResult(fmt.Sprintf("package install approval: %v", err))
				}
				if decision == ApprovalDeny {
					return ErrorResult("package installation denied by admin")
				}
				// Approved — skip deny, continue to execution.
				continue
			}

			return ErrorResult(fmt.Sprintf("command denied by safety policy: matches pattern %s", pattern.String()))
		}
	}

	// Memory path hint: shell commands can't access DB-backed memory files.
	if hint := MaybeMemoryExecHint(normalizedCommand); hint != "" {
		return SilentResult(hint)
	}

	// Credentialed exec: if command matches a configured binary, use Direct Exec Mode.
	// This bypasses approval (admin trust) and shell (security).
	if cred, binary, cmdArgs := t.lookupCredentialedBinary(ctx, command); cred != nil {
		cwd := ToolWorkspaceFromCtx(ctx)
		if cwd == "" {
			cwd = t.workspace
		}
		if wd, _ := args["working_dir"].(string); wd != "" {
			if effectiveRestrict(ctx, t.restrict) {
				if resolved, err := resolvePath(wd, t.workspace, true); err == nil {
					cwd = resolved
				}
			} else {
				cwd = wd
			}
		}
		sandboxKey := ToolSandboxKeyFromCtx(ctx)
		return t.executeCredentialed(ctx, cred, binary, cmdArgs, cwd, sandboxKey, command)
	}

	// Secure CLI gate: registered-but-not-granted binaries MUST NOT fall through
	// to host exec with parent env. Works on the already-normalized command
	// (Red Team F6) and unwraps shell wrappers up to depth 3 (Red Team F1).
	// Fails CLOSED on DB error (Red Team F7).
	if t.secureCLIStore != nil {
		candidates, tooDeep := collectGateCandidates(normalizedCommand)
		if tooDeep {
			slog.Warn("security.credentialed_binary_wrapper_too_deep",
				"command", truncateCmd(normalizedCommand, 80),
				"agent_id", store.AgentIDFromContext(ctx))
			return ErrorResult("Command nesting too deep (>3 shell wrappers). This looks adversarial; if legitimate, flatten the command.")
		}
		for _, c := range candidates {
			if c.binary == "" {
				continue
			}
			gctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			registered, rerr := t.secureCLIStore.IsRegisteredBinary(gctx, c.binary)
			cancel()
			if rerr != nil {
				slog.Warn("security.credentialed_binary_gate_error",
					"binary", c.binary, "error", rerr,
					"agent_id", store.AgentIDFromContext(ctx))
				return ErrorResult("Secure CLI gate temporarily unavailable. Retry in a moment.")
			}
			if registered {
				slog.Warn("security.credentialed_binary_denied",
					"binary", c.binary,
					"wrapper", c.wrapper,
					"agent_id", store.AgentIDFromContext(ctx),
					"tenant_id", store.TenantIDFromContext(ctx),
					"command_prefix", truncateCmd(normalizedCommand, 80))
				return ErrorResult(fmt.Sprintf(
					"Binary %q requires a secure CLI grant. Ask admin to grant access to this agent.",
					c.binary))
			}
		}
	}

	// Exec approval check (matching TS exec-approval.ts pipeline)
	if t.approvalMgr != nil {
		switch t.approvalMgr.CheckCommand(command) {
		case "deny":
			return ErrorResult("command denied by exec approval policy")
		case "ask":
			decision, err := t.approvalMgr.RequestApproval(command, t.agentID, 2*time.Minute)
			if err != nil {
				return ErrorResult(fmt.Sprintf("exec approval: %v", err))
			}
			if decision == ApprovalDeny {
				return ErrorResult("command denied by user")
			}
		}
	}

	// Use per-user workspace from context if available, fallback to struct field.
	// The context workspace is tenant-scoped; t.workspace is the global (master) workspace.
	cwd := ToolWorkspaceFromCtx(ctx)
	if cwd == "" {
		cwd = t.workspace
	}
	if wd, _ := args["working_dir"].(string); wd != "" {
		if effectiveRestrict(ctx, t.restrict) {
			// Validate working_dir against the tenant-scoped workspace (not the
			// global workspace) so non-master tenants can't escape their scope.
			// Also allow team workspace as a valid target (same as filesystem tools).
			wsBase := ToolWorkspaceFromCtx(ctx)
			if wsBase == "" {
				wsBase = t.workspace
			}
			// Shell is an arbitrary executor — a cross-chat cwd would let the
			// command mutate files in another chat's workspace. Enforce the
			// stricter write-allowed prefixes (team root excluded) to block
			// cross-chat cwd even for "read-only" commands like cat, since we
			// cannot prove the shell command will not write.
			allowed := allowedWriteWithTeamWorkspace(ctx, nil)
			resolved, err := resolvePathWithAllowed(wd, wsBase, true, allowed)
			if err != nil {
				return ErrorResult(err.Error())
			}
			cwd = resolved
		} else {
			cwd = wd
		}
	}

	// Sandbox routing (sandboxKey from ctx — thread-safe)
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, command, cwd, sandboxKey)
	}

	// Host execution
	return t.executeOnHost(ctx, command, cwd)
}

// matchesAny checks if a command matches any pattern in the list.
func matchesAny(command string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(command) {
			return true
		}
	}
	return false
}

// executeOnHost runs a command directly on the host (original behavior).
// ctx cancellation (e.g. agent abort) triggers SIGTERM → 3s grace → SIGKILL on the
// entire process group so forked children are also cleaned up (no orphans).
func (t *ExecTool) executeOnHost(ctx context.Context, command, cwd string) *Result {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Use plain exec.Command (not CommandContext) so we control the kill sequence.
	// CommandContext would SIGKILL only the direct child, leaving forked grandchildren alive.
	// Route through the platform shell: cmd.exe on Windows, sh on POSIX.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = cwd

	// Scrub credential env vars so fall-through exec cannot exfiltrate
	// host secrets (Red Team F4). Uses static deny list + dynamic keys
	// discovered from any registered secure-cli binary for this tenant.
	var dynKeys []string
	if t.secureCLIStore != nil {
		dynKeys = t.credentialEnvKeys(ctx)
	} else {
		dynKeys = staticCredentialEnvKeys
	}
	cmd.Env = scrubCredentialEnv(os.Environ(), dynKeys)

	// Place the child in its own process group so killProcessGroup(-pgid, sig)
	// reaches the shell and all of its forked children.
	setProcessGroup(cmd)

	// Limit output to 1MB to prevent OOM from runaway commands.
	stdout := &limitedBuffer{max: 1 << 20}
	stderr := &limitedBuffer{max: 1 << 20}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return ErrorResult(fmt.Sprintf("failed to start command: %v", err))
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// Normal completion (success or non-zero exit).
		return buildHostResult(err, stdout, stderr, ctx, t.timeout)

	case <-ctx.Done():
		// Context cancelled or timed out — kill the process group gracefully then forcefully.
		_ = killProcessGroup(cmd, syscallSIGTERM)
		select {
		case <-done:
			// Exited cleanly after SIGTERM.
		case <-time.After(3 * time.Second):
			// Still alive after grace period — force kill.
			_ = killProcessGroup(cmd, syscallSIGKILL)
			<-done
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ErrorResult(fmt.Sprintf("command timed out after %s", t.timeout))
		}
		return ErrorResult("command aborted")
	}
}

// buildHostResult formats the result of a completed host command execution.
func buildHostResult(err error, stdout, stderr *limitedBuffer, ctx context.Context, timeout time.Duration) *Result {
	var result string
	if stdout.Len() > 0 {
		result = stdout.String()
	}
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += "STDERR:\n" + stderr.String()
	}

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ErrorResult(fmt.Sprintf("command timed out after %s", timeout))
		}
		if result == "" {
			result = err.Error()
		}
		return ErrorResult(result)
	}

	if result == "" {
		result = "(command completed with no output)"
	}
	return SilentResult(capExecOutput(result, execMaxOutputChars))
}

// executeInSandbox routes a command through a Docker sandbox container.
func (t *ExecTool) executeInSandbox(ctx context.Context, command, cwd, sandboxKey string) *Result {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		if errors.Is(err, sandbox.ErrSandboxDisabled) {
			return t.executeOnHost(ctx, command, cwd)
		}
		// Docker unavailable (binary missing, daemon down) → fail closed.
		// Do NOT silently fallback to host — that defeats the purpose of sandboxing.
		slog.Warn("security.sandbox_unavailable",
			"error", err,
			"command", truncateCmd(command, 80),
		)
		return ErrorResult(fmt.Sprintf("sandbox unavailable: %v (will not fall back to unsandboxed host execution)", err))
	}

	// Map host workdir to container workdir via SandboxCwd helper.
	containerCwd, cwdErr := SandboxCwd(ctx, t.workspace, sandbox.DefaultContainerWorkdir)
	if cwdErr != nil {
		return ErrorResult(fmt.Sprintf("sandbox path mapping: %v", cwdErr))
	}

	result, err := sb.Exec(ctx, []string{"sh", "-c", command}, containerCwd) //nolint: no ExecOption for normal exec
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox exec: %v", err))
	}

	// Format output same as host execution
	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += "STDERR:\n" + result.Stderr
	}
	if result.ExitCode != 0 {
		if output == "" {
			output = fmt.Sprintf("command exited with code %d", result.ExitCode)
		}
		output += MaybeSandboxHint(result.ExitCode, output)
		return ErrorResult(output)
	}
	if output == "" {
		output = "(command completed with no output)"
	}

	return SilentResult(capExecOutput(output, execMaxOutputChars))
}

// limitedBuffer caps output to prevent OOM from runaway commands.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	if lb.truncated {
		return len(p), nil
	}
	remaining := lb.max - lb.buf.Len()
	if remaining <= 0 {
		lb.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		lb.buf.Write(p[:remaining])
		lb.truncated = true
		return len(p), nil
	}
	return lb.buf.Write(p)
}

func (lb *limitedBuffer) String() string {
	s := lb.buf.String()
	if lb.truncated {
		s += "\n[output truncated at 1MB]"
	}
	return s
}

func (lb *limitedBuffer) Len() int { return lb.buf.Len() }
