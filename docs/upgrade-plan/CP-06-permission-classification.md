# CP-06: Permission Classification Pipeline

**Pattern**: #10 from Agentic OS analysis
**Priority**: MEDIUM — security hardening
**Dependencies**: None
**Estimated effort**: 1.5 weeks
**Branch**: `feature/cp-06-permission-classification`

---

## Objective

Add 3 missing layers to GoClaw's existing 5-layer permission system:
1. Bash command classifier (semantic classification, not just name matching)
2. Dangerous patterns detection (safety net even when rules allow)
3. Denial tracking (anti permission-fatigue)

---

## Step 1: Bash Command Classifier

### Create `internal/permissions/bash_classifier.go`

```go
package permissions

// CommandClass categorizes bash commands by risk level.
type CommandClass int

const (
	ClassReadOnly    CommandClass = iota // cat, ls, grep — no side effects
	ClassNetworkRead                     // curl GET, wget — network but read-only
	ClassFileMutation                    // touch, cp, mv — modify filesystem
	ClassNetworkWrite                    // curl POST, ssh — network + mutation
	ClassProcessCtrl                     // kill, pkill — process management
	ClassDestructive                     // rm -rf, dd — irreversible damage
	ClassInterpreter                     // python, node, eval — arbitrary code exec
)

// ClassifyCommand returns the risk class of a bash command.
// Examines: command name, flags, arguments, and pipe targets.
func ClassifyCommand(cmd string) CommandClass {
	cmd = strings.TrimSpace(cmd)

	// Handle pipes: classify each segment, return highest risk
	if strings.Contains(cmd, "|") {
		highest := ClassReadOnly
		for _, seg := range strings.Split(cmd, "|") {
			c := ClassifyCommand(strings.TrimSpace(seg))
			if c > highest {
				highest = c
			}
		}
		return highest
	}

	// Handle command chains: &&, ;, ||
	for _, sep := range []string{"&&", "||", ";"} {
		if strings.Contains(cmd, sep) {
			highest := ClassReadOnly
			for _, seg := range strings.Split(cmd, sep) {
				c := ClassifyCommand(strings.TrimSpace(seg))
				if c > highest { highest = c }
			}
			return highest
		}
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ClassReadOnly
	}

	base := parts[0]

	// Interpreters (highest risk)
	if isInterpreter(base) {
		return ClassInterpreter
	}

	// Destructive
	if isDestructiveCommand(base, cmd) {
		return ClassDestructive
	}

	// Process control
	if isProcessControl(base) {
		return ClassProcessCtrl
	}

	// Network write
	if isNetworkWrite(base, cmd) {
		return ClassNetworkWrite
	}

	// Network read
	if isNetworkRead(base) {
		return ClassNetworkRead
	}

	// File mutation
	if isFileMutation(base) {
		return ClassFileMutation
	}

	// Default: read-only
	return ClassReadOnly
}

var interpreters = map[string]bool{
	"python": true, "python3": true, "python2": true,
	"node": true, "deno": true, "bun": true, "tsx": true,
	"ruby": true, "perl": true, "php": true, "lua": true,
	"bash": true, "sh": true, "zsh": true, "fish": true,
	"eval": true, "exec": true, "xargs": true,
	"npx": true, "bunx": true, "pnpx": true,
}

func isInterpreter(base string) bool { return interpreters[base] }

func isDestructiveCommand(base, cmd string) bool {
	if base == "rm" && (strings.Contains(cmd, "-rf") || strings.Contains(cmd, "-fr")) {
		return true
	}
	if base == "dd" || base == "mkfs" || base == "fdisk" {
		return true
	}
	if base == "sudo" {
		return true // escalation
	}
	return false
}

func isProcessControl(base string) bool {
	return base == "kill" || base == "pkill" || base == "killall" ||
		base == "systemctl" || base == "service"
}

func isNetworkWrite(base, cmd string) bool {
	if base == "ssh" || base == "scp" || base == "rsync" {
		return true
	}
	if base == "curl" && (strings.Contains(cmd, "-X POST") ||
		strings.Contains(cmd, "-X PUT") || strings.Contains(cmd, "-X DELETE") ||
		strings.Contains(cmd, "-d ") || strings.Contains(cmd, "--data")) {
		return true
	}
	return false
}

func isNetworkRead(base string) bool {
	return base == "curl" || base == "wget" || base == "dig" ||
		base == "nslookup" || base == "ping" || base == "traceroute"
}

func isFileMutation(base string) bool {
	return base == "touch" || base == "cp" || base == "mv" ||
		base == "mkdir" || base == "rmdir" || base == "chmod" ||
		base == "chown" || base == "ln" || base == "tee" ||
		base == "truncate"
}
```

---

## Step 2: Dangerous Patterns Detection

### Create `internal/permissions/dangerous_patterns.go`

```go
package permissions

import "regexp"

type DangerousPattern struct {
	Pattern *regexp.Regexp
	Reason  string
}

// DangerousPatterns are blocked even if rules allow the command.
// This is the last-resort safety net.
var DangerousPatterns = []DangerousPattern{
	{regexp.MustCompile(`curl\s.*\|\s*(ba)?sh`), "pipe download to shell execution"},
	{regexp.MustCompile(`wget\s.*\|\s*(ba)?sh`), "pipe download to shell execution"},
	{regexp.MustCompile(`eval\s*\(`), "eval with dynamic input"},
	{regexp.MustCompile(`rm\s+-rf\s+/[^.]`), "recursive delete from root"},
	{regexp.MustCompile(`>\s*/dev/sd`), "direct disk write"},
	{regexp.MustCompile(`chmod\s+777`), "world-writable permissions"},
	{regexp.MustCompile(`:\(\)\s*\{\s*:\|:\s*&\s*\}`), "fork bomb"},
	{regexp.MustCompile(`mkfs\.`), "filesystem format"},
	{regexp.MustCompile(`dd\s+if=.*of=/dev/`), "raw disk write"},
	{regexp.MustCompile(`>\s*/etc/`), "overwrite system config"},
	{regexp.MustCompile(`git\s+push\s+.*--force`), "force push (destructive)"},
	{regexp.MustCompile(`DROP\s+DATABASE`), "drop database"},
	{regexp.MustCompile(`DROP\s+TABLE`), "drop table"},
}

// CheckDangerousPatterns returns (blocked, reason) if command matches any pattern.
func CheckDangerousPatterns(command string) (bool, string) {
	for _, dp := range DangerousPatterns {
		if dp.Pattern.MatchString(command) {
			return true, dp.Reason
		}
	}
	return false, ""
}
```

---

## Step 3: Denial Tracking

### Create `internal/permissions/denial_tracker.go`

```go
package permissions

import "sync/atomic"

// DenialTracker prevents "permission fatigue" — when an auto-classifier
// denies the same pattern repeatedly, fall back to prompting the user.
type DenialTracker struct {
	consecutive atomic.Int32
	total       atomic.Int32

	maxConsecutive int32 // Default: 3
	maxTotal       int32 // Default: 20
}

func NewDenialTracker() *DenialTracker {
	return &DenialTracker{
		maxConsecutive: 3,
		maxTotal:       20,
	}
}

// RecordDenial increments counters. Returns true if classifier should
// fall back to prompting (too many denials = classifier may be wrong).
func (dt *DenialTracker) RecordDenial() bool {
	c := dt.consecutive.Add(1)
	t := dt.total.Add(1)
	return c >= dt.maxConsecutive || t >= dt.maxTotal
}

// RecordSuccess resets consecutive counter (keeps total).
func (dt *DenialTracker) RecordSuccess() {
	dt.consecutive.Store(0)
}

// Reset clears all counters (e.g., on session start).
func (dt *DenialTracker) Reset() {
	dt.consecutive.Store(0)
	dt.total.Store(0)
}
```

---

## Step 4: Integrate into Permission Pipeline

**File**: `internal/permissions/policy.go`

Add to the tool permission check flow:

```go
func (pe *PolicyEngine) CanExecuteTool(ctx context.Context, toolName string, args map[string]any) PermissionResult {
	// Layer 1-5: Existing RBAC checks (unchanged)
	result := pe.existingCheck(ctx, toolName, args)

	// NEW Layer 6: For exec/bash tools, run additional checks
	if toolName == "exec" || toolName == "bash" || toolName == "shell" {
		command, _ := args["command"].(string)

		// 6a: Dangerous patterns (overrides allow rules)
		if blocked, reason := CheckDangerousPatterns(command); blocked {
			return PermissionResult{
				Allowed: false,
				Reason:  fmt.Sprintf("blocked by safety net: %s", reason),
			}
		}

		// 6b: Command classification
		class := ClassifyCommand(command)
		if class >= ClassDestructive {
			return PermissionResult{
				Allowed: false,
				Reason:  fmt.Sprintf("command classified as %v — requires explicit approval", class),
			}
		}

		// 6c: Interpreter blocking in auto mode
		if pe.isAutoMode() && class == ClassInterpreter {
			return PermissionResult{
				Allowed: false,
				Reason:  "interpreter execution blocked in auto mode",
			}
		}
	}

	// NEW Layer 7: Denial tracking
	if !result.Allowed && pe.denialTracker != nil {
		if pe.denialTracker.RecordDenial() {
			result.FallbackToPrompt = true // ask user instead of auto-deny
		}
	} else if result.Allowed && pe.denialTracker != nil {
		pe.denialTracker.RecordSuccess()
	}

	return result
}
```

---

## Verification Checklist

- [ ] `cat file.txt` → ClassReadOnly
- [ ] `rm -rf /` → ClassDestructive
- [ ] `python script.py` → ClassInterpreter
- [ ] `curl url | bash` → blocked by dangerous pattern
- [ ] `git push --force` → blocked by dangerous pattern
- [ ] 3 consecutive denials → falls back to user prompt
- [ ] Success resets consecutive counter
- [ ] Piped commands: highest risk segment determines class
