package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	shellwords "github.com/mattn/go-shellwords"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// maxWrapperDepth is the hard cap on shell-wrapper unwrapping. Commands nested
// deeper than this are denied unconditionally as adversarial — real commands
// never wrap beyond depth 3.
const maxWrapperDepth = 3

// wrapperBinaries identifies shell/exec wrappers whose first arg after -c is
// the real command to gate. Key is the normalized base name.
var wrapperBinaries = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true,
	"env": true, "nohup": true, "stdbuf": true, "timeout": true,
}

// normalizeBinaryName returns the lowercased file base of a binary reference.
// Examples: "/usr/bin/gh" → "gh", "./GH" → "gh", "  Gh  " → "gh".
// Applied at both the gate lookup and lookupCredentialedBinary so the two
// layers agree on identity.
func normalizeBinaryName(s string) string {
	return filepath.Base(strings.TrimSpace(strings.ToLower(s)))
}

// detectWrapper recognises shell-wrapper invocations and returns the inner
// command string. Supported shapes:
//
//	sh -c "<inner>"          (also bash / zsh / dash / /bin/sh / /usr/bin/env sh ...)
//	env [K=V ...] <cmd> ...  (no -c; the real binary is <cmd>)
//	nohup <cmd> ...
//	stdbuf -oL <cmd> ...
//	timeout 10 <cmd> ...
//
// Returns wrapper=normalized wrapper name, innerCmd=remaining command string.
// ok=false when cmd is not a recognised wrapper or parsing fails.
func detectWrapper(cmd string) (wrapper string, innerCmd string, ok bool) {
	parser := shellwords.NewParser()
	parser.ParseBacktick = false
	parser.ParseEnv = false
	words, err := parser.Parse(cmd)
	if err != nil || len(words) == 0 {
		return "", "", false
	}
	head := normalizeBinaryName(words[0])
	if !wrapperBinaries[head] {
		return "", "", false
	}

	switch head {
	case "sh", "bash", "zsh", "dash":
		// sh -c "<inner>" → inner is word[2].
		for i := 1; i < len(words); i++ {
			if words[i] == "-c" && i+1 < len(words) {
				return head, words[i+1], true
			}
		}
		return "", "", false
	case "env":
		// env [K=V ...] <cmd> [args...] OR env -S "<cmd args>"
		for i := 1; i < len(words); i++ {
			w := words[i]
			if strings.Contains(w, "=") && !strings.HasPrefix(w, "-") {
				continue // env var assignment, skip
			}
			if w == "-i" || w == "-u" || w == "-" {
				continue
			}
			if strings.HasPrefix(w, "-") {
				// Unknown env flag — bail out of wrapper detection.
				return "", "", false
			}
			// First non-assignment, non-flag token is the real command.
			return "env", strings.Join(append([]string{w}, words[i+1:]...), " "), true
		}
		return "", "", false
	case "nohup":
		if len(words) < 2 {
			return "", "", false
		}
		return "nohup", strings.Join(words[1:], " "), true
	case "stdbuf":
		// stdbuf [-oL -eL -iL ...] <cmd> ...
		for i := 1; i < len(words); i++ {
			if strings.HasPrefix(words[i], "-") {
				continue
			}
			return "stdbuf", strings.Join(words[i:], " "), true
		}
		return "", "", false
	case "timeout":
		// timeout [--foreground] [-k DUR] <DURATION> <cmd> ...
		for i := 1; i < len(words); i++ {
			w := words[i]
			if strings.HasPrefix(w, "-") {
				continue
			}
			// First non-flag token is the DURATION — skip it.
			if i+1 < len(words) {
				return "timeout", strings.Join(words[i+1:], " "), true
			}
			return "", "", false
		}
		return "", "", false
	}
	return "", "", false
}

// gateCandidate is one step of wrapper unwrapping; it captures the binary
// name to gate and (optionally) the wrapper token that introduced it.
type gateCandidate struct {
	binary  string // normalized (lowercase, file base)
	wrapper string // "" for outermost direct invocation; else wrapper name
}

// collectGateCandidates returns the ordered list of binaries extracted from
// a command via recursive wrapper unwrapping (outermost → innermost).
// tooDeep=true when unwrapping exceeds maxWrapperDepth — callers must deny.
func collectGateCandidates(cmd string) (candidates []gateCandidate, tooDeep bool) {
	current := cmd
	wrapper := ""
	for depth := 0; ; depth++ {
		bin, _, err := parseCommandBinary(current)
		if err != nil || bin == "" {
			return candidates, false
		}
		norm := normalizeBinaryName(bin)
		candidates = append(candidates, gateCandidate{binary: norm, wrapper: wrapper})
		w, inner, ok := detectWrapper(current)
		if !ok || strings.TrimSpace(inner) == "" {
			return candidates, false
		}
		if depth+1 > maxWrapperDepth {
			return candidates, true
		}
		wrapper = w
		current = inner
	}
}

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

// detectUnquotedShellOperators scans a command string for shell metacharacters
// that appear OUTSIDE of single or double quotes. This prevents false positives
// when argument values contain characters like | (e.g. --jq '.[0] | .name').
// Returns the list of detected operators, or nil if the command is clean.
func detectUnquotedShellOperators(command string) []string {
	unquoted := extractUnquotedSegments(command)
	if unquoted == "" {
		return nil
	}
	return detectShellOperators(unquoted)
}

// extractUnquotedSegments returns a string containing only the characters
// from command that are outside of single-quoted and double-quoted segments.
// Backslash escaping is handled both inside double quotes (\") and outside
// quotes (\' \" \\) to match go-shellwords parsing behavior — without this,
// \" outside quotes would incorrectly enter double-quote mode and hide
// subsequent shell operators from detection.
func extractUnquotedSegments(command string) string {
	var buf strings.Builder
	buf.Grow(len(command))

	inSingle := false
	inDouble := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			}
		case inDouble:
			if ch == '\\' && i+1 < len(command) {
				i++ // skip escaped character inside double quotes
			} else if ch == '"' {
				inDouble = false
			}
		default:
			switch ch {
			case '\\':
				// Backslash outside quotes escapes the next character, preventing
				// it from being treated as a quote delimiter. Both the backslash
				// and the escaped character are emitted as unquoted content so
				// that operator detection still sees them (e.g. \; remains visible).
				buf.WriteByte(ch)
				if i+1 < len(command) {
					i++
					buf.WriteByte(command[i])
				}
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			default:
				buf.WriteByte(ch)
			}
		}
	}
	return buf.String()
}

// detectShellOperators scans a raw command string for shell metacharacters.
// Returns the list of detected operators, or nil if the command is clean.
// NOTE: This function does not respect quoting — use detectUnquotedShellOperators
// for credentialed exec where argument values may contain metacharacters.
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
// verifies any stored path matches either the command binary or a known runtime
// package alias (for example openrouter-cli -> orc).
func resolveAndMatchBinary(binaryName string, configPath *string) (string, error) {
	if configPath != nil && strings.TrimSpace(*configPath) != "" {
		expectedPath := strings.TrimSpace(*configPath)
		if !filepath.IsAbs(expectedPath) {
			return "", fmt.Errorf("configured binary path must be absolute: %q", expectedPath)
		}
		if !skills.IsExecutableFile(expectedPath) {
			return "", fmt.Errorf("configured binary path %q is not executable", expectedPath)
		}
		if normalizeBinaryName(expectedPath) == normalizeBinaryName(binaryName) {
			return expectedPath, nil
		}
		if runtimePath, ok := skills.FindRuntimeExecutable(binaryName); ok && runtimePath == expectedPath {
			return expectedPath, nil
		}
		return "", fmt.Errorf("binary path mismatch: command uses %q but config expects %q", binaryName, expectedPath)
	}

	absPath, err := exec.LookPath(binaryName)
	if err != nil {
		if runtimePath, ok := skills.FindRuntimeExecutable(binaryName); ok {
			return runtimePath, nil
		}
		return "", fmt.Errorf("binary %q not found in PATH: %w", binaryName, err)
	}
	return absPath, nil
}

// matchesBinaryDeny checks if the joined args string matches any per-binary deny pattern.
// Used for deny_args where patterns span multiple args (e.g. `auth\s+login`, `repo\s+delete`).
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

// matchesBinaryVerbose checks each arg token against verbose/debug flag patterns.
// Patterns are anchored at the START of each arg (but not the end), which allows:
//   - `-v` to match `-v`, `-vv`, `-vvv` (verbosity escalation), `-v=1`, `-vq` (combined flags)
//   - `--verbose` to match `--verbose`, `--verbose=true`
//   - `-v` to NOT match `--version` (char 1 is `-`, not `v`)
//   - `--verbose` to NOT match `--version` (diverges at char 5)
//
// This is intentional: verbose flags leak sensitive output (tokens in HTTP headers,
// API response bodies, OAuth flows). Start-anchored per-arg matching catches the
// real verbose family without false-positive on safe flags like `--version`.
// Returns the matched pattern string, or empty if allowed.
func matchesBinaryVerbose(args []string, denyPatternsJSON json.RawMessage) string {
	if len(denyPatternsJSON) == 0 {
		return ""
	}
	var patterns []string
	if err := json.Unmarshal(denyPatternsJSON, &patterns); err != nil || len(patterns) == 0 {
		return ""
	}
	for _, p := range patterns {
		re, err := regexp.Compile("^(?:" + p + ")")
		if err != nil {
			slog.Warn("secure_cli.invalid_deny_pattern", "pattern", p, "error", err)
			continue
		}
		if slices.ContainsFunc(args, re.MatchString) {
			return p
		}
	}
	return ""
}

// executeCredentialed runs a CLI command in Direct Exec Mode (no shell).
// Credentials are injected as env vars into the child process only.
// rawCommand is the original command string before shell-word parsing (preserves quoting).
func (t *ExecTool) executeCredentialed(ctx context.Context, cred *store.SecureCLIBinary,
	binary string, args []string, cwd string, sandboxKey string, rawCommand string) *Result {

	// Attach a per-request scrub bag so adapter-derived secrets stay isolated
	// from other tenants/goroutines (see scrub.go WithScrubBag).
	ctx = WithScrubBag(ctx)

	// Step 0: Reject NUL bytes (defense-in-depth — also checked in Execute()).
	if strings.ContainsRune(rawCommand, '\x00') {
		return ErrorResult("command contains invalid NUL byte")
	}

	// Step 1: Check for shell operators in the ORIGINAL command (preserves quoting).
	// We check the raw command string (before shell-word parsing) so that characters
	// inside quoted argument values (e.g. | in --jq '.[0] | ...') are not falsely flagged.
	// Only top-level (unquoted) shell operators indicate actual command chaining attempts.
	if ops := detectUnquotedShellOperators(rawCommand); len(ops) > 0 {
		return credentialedShellOperatorError(rawCommand, ops)
	}

	// Step 2: Per-binary deny check (deny_args)
	denyArgs, allowAudits := applyCommandKeywordAllowlist(binary, args, t.commandKeywordAllowlistSnapshot())
	if p := matchesBinaryDeny(denyArgs, cred.DenyArgs); p != "" {
		return credentialedDenyError(binary, args, p)
	}
	// Per-binary verbose deny check (deny_verbose) — per-arg start-anchored match
	// so `-v` blocks `-v`/`-vv`/`-v=1` but not `--version`.
	if p := matchesBinaryVerbose(args, cred.DenyVerbose); p != "" {
		return credentialedDenyError(binary, args, p)
	}
	for _, audit := range allowAudits {
		slog.Info("security.command_keyword_allowlist",
			"binary", audit.Command,
			"subcommand", audit.Subcommand,
			"arg", audit.Arg,
			"keyword", audit.Keyword,
			"rule_id", audit.RuleID,
			"reason", audit.Reason,
			"agent_id", store.AgentIDFromContext(ctx),
			"user_id", store.UserIDFromContext(ctx),
			"credential_user_id", store.CredentialUserIDFromContext(ctx),
			"tenant_id", store.TenantIDFromContext(ctx),
		)
	}

	// Step 3: Decrypt env vars from store (already decrypted by store layer).
	// Per-user env overrides take priority over binary/grant env.
	envMap, err := mergeCredentialedEnv(cred)
	if err != nil {
		return ErrorResult(fmt.Sprintf("credentialed exec: invalid env JSON for %q: %v", binary, err))
	}
	slog.Debug("secure_cli.env_merged",
		"binary", binary,
		"agent_id", store.AgentIDFromContext(ctx),
		"tenant_id", store.TenantIDFromContext(ctx),
		"credential_user_id_present", store.CredentialUserIDFromContext(ctx) != "",
		"env_keys", sortedKeys(envMap),
	)
	if missing := missingRequiredCredentialEnv(binary, envMap); len(missing) > 0 {
		slog.Warn("secure_cli.missing_required_env",
			"binary", binary,
			"agent_id", store.AgentIDFromContext(ctx),
			"tenant_id", store.TenantIDFromContext(ctx),
			"credential_user_id_present", store.CredentialUserIDFromContext(ctx) != "",
			"missing_env_keys", missing,
			"env_keys", sortedKeys(envMap),
		)
		return credentialedMissingEnvError(binary, missing)
	}

	// Step 4: Register credential values for output scrubbing
	for _, v := range envMap {
		AddScrubValuesCtx(ctx, v)
	}

	// Step 5: Resolve binary to absolute path and verify against config
	absPath, err := resolveAndMatchBinary(binary, cred.BinaryPath)
	if err != nil {
		r := credentialedPathError(binary, err)
		if t.sandboxMgr != nil && sandboxKey != "" {
			r.ForLLM += hintBinaryNotFound
		}
		return r
	}

	// Step 6: Determine timeout
	timeout := time.Duration(cred.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Step 6b: Resolve adapter from DB row (source of truth, NOT CLIPresets map).
	// Passthrough is the default and a no-op — preserves bit-for-bit behavior
	// for every legacy preset.
	adapterName := ""
	if cred.AdapterName != nil {
		adapterName = *cred.AdapterName
	}
	adapter := AdapterFor(adapterName)

	// Sandbox is incompatible with non-passthrough adapters in v1 — they need
	// to materialize ephemeral files (SSH key, PAT helper) on the host fs that
	// the sandboxed process can't read. Reject early before any ephemerals
	// would be created.
	inSandbox := t.sandboxMgr != nil && sandboxKey != ""
	if inSandbox && adapter.Name() != "passthrough" {
		return ErrorResult(fmt.Sprintf("credentialed exec: %q adapter not supported in sandbox mode yet", adapter.Name()))
	}

	if adapter.ShouldInject(args) {
		userCred := userCredFromBinary(ctx, cred)
		if r := validateAdapterCredentialReady(adapter.Name(), binary, userCred); r != nil {
			return r
		}
		// Plant the resolved exec cwd so adapters (e.g. git) can run any
		// pre-flight sub-exec from the right repo, not goclaw's daemon CWD.
		prepareCtx := WithExecCwd(ctx, cwd)
		inj, err := adapter.Prepare(prepareCtx, cred, userCred, args)
		if err != nil {
			return ErrorResult(ScrubCredentialsCtx(ctx, fmt.Sprintf("credentialed exec: %s adapter prepare failed: %v", adapter.Name(), err)))
		}
		if inj != nil {
			if inj.Cleanup != nil {
				defer func() {
					if cerr := inj.Cleanup(); cerr != nil {
						// Scrub cerr — os.Remove errors embed the full tmpfile
						// path, which is in inj.ScrubValues precisely because
						// it's adapter-sensitive (e.g. SSH key keypath).
						slog.Warn("security.adapter_cleanup_failed",
							"adapter", adapter.Name(),
							"binary", binary,
							"error", ScrubCredentialsCtx(ctx, cerr.Error()),
						)
					}
				}()
			}
			if len(inj.ArgvPrefix) > 0 {
				args = append(append([]string{}, inj.ArgvPrefix...), args...)
			}
			maps.Copy(envMap, inj.Env)
			if len(inj.ScrubValues) > 0 {
				AddScrubValuesCtx(ctx, inj.ScrubValues...)
			}
			emitSystemEnvInjectionAudit(adapter.Name(), binary,
				store.CredentialUserIDFromContext(ctx), cred.CredentialSource, inj, effectiveHostScope(cred))
		}
	}

	// Step 7: Execute — sandbox or host
	if inSandbox {
		return t.executeCredentialedSandbox(ctx, absPath, args, cwd, sandboxKey, envMap, timeout)
	}
	return t.executeCredentialedHost(ctx, absPath, args, cwd, envMap, timeout)
}

// userCredFromBinary synthesizes adapter credential input from LookupByBinary's
// effective credential fields. The adapter contract still uses
// SecureCLIUserCredential, but the material can come from user, context, or
// agent scoped rows.
func userCredFromBinary(ctx context.Context, bin *store.SecureCLIBinary) *store.SecureCLIUserCredential {
	if bin == nil {
		return nil
	}
	if len(bin.CredentialEnv) > 0 || bin.CredentialType != nil || bin.CredentialHostScope != nil {
		return &store.SecureCLIUserCredential{
			BinaryID:       bin.ID,
			UserID:         credentialSubjectForAdapter(ctx, bin),
			EncryptedEnv:   bin.CredentialEnv,
			CredentialType: bin.CredentialType,
			HostScope:      bin.CredentialHostScope,
		}
	}
	if len(bin.UserEnv) == 0 && bin.UserCredentialType == nil && bin.UserHostScope == nil {
		return nil
	}
	return &store.SecureCLIUserCredential{
		BinaryID:       bin.ID,
		UserID:         store.CredentialUserIDFromContext(ctx),
		EncryptedEnv:   bin.UserEnv,
		CredentialType: bin.UserCredentialType,
		HostScope:      bin.UserHostScope,
	}
}

func credentialSubjectForAdapter(ctx context.Context, bin *store.SecureCLIBinary) string {
	if bin != nil && bin.CredentialSubjectID != "" {
		return bin.CredentialSubjectID
	}
	return store.CredentialUserIDFromContext(ctx)
}

func validateAdapterCredentialReady(adapterName, binary string, cred *store.SecureCLIUserCredential) *Result {
	if adapterName != "git" {
		return nil
	}
	if cred == nil || cred.CredentialType == nil || strings.TrimSpace(*cred.CredentialType) == "" {
		return credentialedGitCredentialResolutionError(binary, "no typed git credential selected")
	}
	switch strings.TrimSpace(*cred.CredentialType) {
	case "pat", "ssh_key":
		if cred.HostScope == nil || strings.TrimSpace(*cred.HostScope) == "" {
			return credentialedGitCredentialResolutionError(binary, "typed git credential is missing host_scope")
		}
		return nil
	case "env":
		return credentialedGitCredentialResolutionError(binary, "legacy env credential cannot authenticate adapter-managed git remotes")
	default:
		return nil
	}
}

func effectiveHostScope(bin *store.SecureCLIBinary) *string {
	if bin == nil {
		return nil
	}
	if bin.CredentialHostScope != nil {
		return bin.CredentialHostScope
	}
	return bin.UserHostScope
}

func mergeCredentialedEnv(cred *store.SecureCLIBinary) (map[string]string, error) {
	envMap := make(map[string]string)
	if cred == nil {
		return envMap, nil
	}
	if len(cred.EncryptedEnv) > 0 {
		baseEnv, err := store.FlattenSecureCLIEnv(cred.EncryptedEnv)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, baseEnv)
	}
	if len(cred.CredentialEnv) > 0 && isEnvCredentialType(cred.CredentialType) {
		scopedEnv, err := store.FlattenSecureCLIEnv(cred.CredentialEnv)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, scopedEnv)
		return envMap, nil
	}
	if len(cred.UserEnv) > 0 {
		if !isEnvCredentialType(cred.UserCredentialType) {
			return envMap, nil
		}
		userEnvMap, err := store.FlattenSecureCLIEnv(cred.UserEnv)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, userEnvMap)
	}
	return envMap, nil
}

func isEnvCredentialType(typ *string) bool {
	return typ == nil || *typ == "" || *typ == "env"
}

func missingRequiredCredentialEnv(binary string, envMap map[string]string) []string {
	required := requiredCredentialEnvVars(binary)
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(envMap[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

// validateExecCwd checks that cmd.Dir exists and is a directory before
// cmd.Start(). Empty cwd is allowed (Go interprets as "inherit parent's dir").
//
// Why: on Linux, Go's syscall.forkAndExecInChild reports any child-side error
// (chdir, execve, missing PT_INTERP, etc.) as `&PathError{Op: "fork/exec",
// Path: argv0, Err: errno}`. The label `fork/exec PATH:` always names the
// binary even when chdir was the actual failure. A stale stored workspace
// (e.g. /app/workspace/clax persisted from a Docker-era deployment but used
// on a bare-metal host) then surfaces as `fork/exec /usr/bin/gh: no such file
// or directory` — sending operators down the wrong investigation path.
//
// Returns nil when cwd is empty, exists and is a directory; otherwise an
// error naming the actual problem with the working directory.
func validateExecCwd(cwd string) error {
	if cwd == "" {
		return nil
	}
	info, err := os.Stat(cwd)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("working directory does not exist: %q", cwd)
		}
		return fmt.Errorf("working directory inaccessible: %q: %w", cwd, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("working directory is not a directory: %q", cwd)
	}
	return nil
}

// executeCredentialedHost runs a credentialed command directly on the host.
// Uses exec.Command (no shell) with credentials as env vars.
// ctx cancellation triggers SIGTERM → 3s grace → SIGKILL via process-group helpers.
func (t *ExecTool) executeCredentialedHost(ctx context.Context, absPath string, args []string,
	cwd string, envMap map[string]string, timeout time.Duration) *Result {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Pre-flight cmd.Dir validation. On Linux, Go's clone+chdir+execve failure
	// path collapses every child-side error into "fork/exec PATH: <errno-string>"
	// — so a missing cmd.Dir surfaces as if absPath itself were missing. Catch
	// this case explicitly so the error names the real culprit.
	if err := validateExecCwd(cwd); err != nil {
		return ErrorResult(fmt.Sprintf("credentialed exec: %v (binary %s does exist)", err, absPath))
	}

	// Plain exec.Command (not CommandContext) so we own the kill sequence.
	cmd := exec.Command(absPath, args...)
	cmd.Dir = cwd

	// Process group so abort reaches the whole tree (mirrors executeOnHost).
	setProcessGroup(cmd)

	// Build env: inherit minimal PATH + HOME, add credentials
	cmd.Env = buildCredentialedEnv(envMap)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return ErrorResult(fmt.Sprintf("credentialed exec: failed to start %s: %v", absPath, err))
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return formatCredentialedResult(absPath, args, stdout.String(), stderr.String(), err, ctx, timeout)

	case <-ctx.Done():
		_ = killProcessGroup(cmd, syscallSIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = killProcessGroup(cmd, syscallSIGKILL)
			<-done
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ErrorResult(fmt.Sprintf("[CREDENTIALED EXEC] Command timed out after %s.\nBinary: %s", timeout, absPath))
		}
		return ErrorResult(fmt.Sprintf("[CREDENTIALED EXEC] Command aborted.\nBinary: %s", absPath))
	}
}

// executeCredentialedSandbox runs a credentialed command inside a Docker sandbox.
// Uses sandbox.WithEnv to inject credentials via docker exec -e (no shell).
func (t *ExecTool) executeCredentialedSandbox(ctx context.Context, absPath string, args []string,
	cwd string, sandboxKey string, envMap map[string]string, timeout time.Duration) *Result {

	mountWorkspace, err := effectiveSandboxWorkspace(ctx, t.workspace)
	if err != nil {
		return ErrorResult(err.Error())
	}
	containerCwd, cwdErr := sandboxCwdForHostPath(cwd, mountWorkspace, sandbox.DefaultContainerWorkdir)
	if cwdErr != nil {
		return ErrorResult(fmt.Sprintf("credentialed sandbox path mapping: %v", cwdErr))
	}

	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, mountWorkspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		slog.Warn("security.credentialed_exec_sandbox_unavailable",
			"binary", absPath, "error", err)
		return ErrorResult("credentialed exec requires sandbox but sandbox is unavailable: " + err.Error())
	}

	// Direct exec inside sandbox: [absPath, args...] with env injection
	command := append([]string{absPath}, args...)
	result, err := sb.Exec(ctx, command, containerCwd, sandbox.WithEnv(envMap))
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
		scrubbed := ScrubCredentialsCtx(ctx, output)
		return credentialedExecFailError(absPath, args, result.ExitCode, scrubbed+MaybeSandboxHint(result.ExitCode, scrubbed))
	}
	if output == "" {
		output = "(command completed with no output)"
	}
	output = ScrubCredentialsCtx(ctx, output)
	output = capExecOutput(output, execMaxOutputChars)
	return SilentResult(output)
}

// buildCredentialedEnv creates a minimal environment with injected credentials.
// Inherits PATH and HOME from parent process, adds credential env vars.
// On Windows, also passes through SYSTEMROOT / TEMP / APPDATA / etc. — these
// are not secrets and many native CLIs (gh, az, aws, npm) require them to run.
func buildCredentialedEnv(envMap map[string]string) []string {
	var env []string
	if runtime.GOOS == "windows" {
		pathDefault := "C:\\Windows\\system32;C:\\Windows;C:\\Windows\\System32\\Wbem"
		homeDefault := os.Getenv("USERPROFILE")
		if homeDefault == "" {
			homeDefault = "C:\\Users\\Default"
		}
		env = []string{
			"PATH=" + getenvDefault("PATH", pathDefault),
			"HOME=" + getenvDefault("HOME", homeDefault),
			"LANG=" + getenvDefault("LANG", "en_US.UTF-8"),
			"USERNAME=" + getenvDefault("USERNAME", "goclaw"),
		}
		// Pass through Windows runtime vars that native tools expect.
		// Missing SYSTEMROOT breaks networking/registry in most Win32 programs.
		for _, k := range []string{
			"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "COMSPEC", "PATHEXT",
			"TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
			"PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMDATA",
			"HOMEDRIVE", "HOMEPATH", "COMPUTERNAME",
		} {
			if v := os.Getenv(k); v != "" {
				env = append(env, k+"="+v)
			}
		}
	} else {
		env = []string{
			"PATH=" + getenvDefault("PATH", "/usr/local/bin:/usr/bin:/bin"),
			"HOME=" + getenvDefault("HOME", "/tmp"),
			"LANG=" + getenvDefault("LANG", "en_US.UTF-8"),
			"USER=" + getenvDefault("USER", "goclaw"),
		}
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
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ErrorResult(fmt.Sprintf("[CREDENTIALED EXEC] Command timed out after %s.\nBinary: %s", timeout, binary))
		}
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return credentialedExecFailError(binary, args, exitCode, ScrubCredentialsCtx(ctx, output))
	}

	if output == "" {
		output = "(command completed with no output)"
	}
	output = ScrubCredentialsCtx(ctx, output)
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
	// Normalize lookup key so path/case variants (/usr/bin/gh, ./gh, GH) all
	// resolve to the same registry row. Same helper is used by the gate
	// branch in Execute because identity must agree at both layers.
	normBinary := normalizeBinaryName(binary)
	// Get agent ID from context for scoped lookup
	agentID := store.AgentIDFromContext(ctx)
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}
	// Pass userID for per-user credential resolution (LEFT JOIN, zero extra queries).
	// Uses CredentialUserIDFromContext to pick up merged tenant user identity
	// (falls back to UserIDFromContext when not set).
	userID := store.CredentialUserIDFromContext(ctx)
	cred, err := t.secureCLIStore.LookupByBinary(ctx, normBinary, agentIDPtr, userID)
	if err != nil {
		slog.Warn("secure_cli.lookup: query failed", "binary", binary, "agent_id", agentID, "error", err)
		return nil, "", nil
	}
	if cred == nil {
		slog.Debug("secure_cli.lookup: no credential found", "binary", binary, "agent_id", agentID)
		return nil, "", nil
	}
	slog.Debug("secure_cli.lookup: found credential", "binary", binary, "cred_id", cred.ID, "env_size", len(cred.EncryptedEnv))
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

func credentialedMissingEnvError(binary string, keys []string) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Credential config missing required credential env.\n"+
			"Binary: %s\nMissing env keys: %s\n"+
			"Configure the missing key as a SecureCLI user credential for the same user context, then grant this binary to the target agent.",
			binary, strings.Join(keys, ", ")),
		ForUser: fmt.Sprintf("CLI credential for %q is missing required env: %s.", binary, strings.Join(keys, ", ")),
		IsError: true,
	}
}

func credentialedGitCredentialResolutionError(binary, reason string) *Result {
	return &Result{
		ForLLM: fmt.Sprintf("[CREDENTIALED EXEC] Git credential resolution failed.\n"+
			"Binary: %s\nReason: %s\n"+
			"Configure a SecureCLI git credential with Credential Type = Personal Access Token or SSH Private Key and Host Scope matching the remote host, then grant this binary to the target agent.",
			binary, reason),
		ForUser: "Git credential resolution failed. Configure a host-scoped PAT or SSH key for this agent.",
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
