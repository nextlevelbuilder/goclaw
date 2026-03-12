package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// Dangerous command patterns to deny by default.
// Defense-in-depth: these patterns complement Docker hardening (cap-drop ALL,
// read-only rootfs, no-new-privileges, pids-limit, memory limit).
// Sources: OWASP Agentic AI Top 10, Claude Code CVE-2025-66032, MITRE ATT&CK,
// PayloadsAllTheThings, Trail of Bits prompt-injection-to-RCE research.
var defaultDenyPatterns = []*regexp.Regexp{
	// ── Destructive file operations ──
	regexp.MustCompile(`\brm\s+-[rf]{1,2}\b`),
	regexp.MustCompile(`\brm\s+.*--recursive`),
	regexp.MustCompile(`\brm\s+.*--force`),
	regexp.MustCompile(`\bdel\s+/[fq]\b`),
	regexp.MustCompile(`\brmdir\s+/s\b`),
	regexp.MustCompile(`\b(mkfs|diskpart)\b|\bformat\s`),
	regexp.MustCompile(`\bdd\s+if=`),
	regexp.MustCompile(`>\s*/dev/sd[a-z]\b`),
	regexp.MustCompile(`\b(shutdown|reboot|poweroff)\b`),
	regexp.MustCompile(`:\(\)\s*\{.*\};\s*:`), // fork bomb

	// ── Data exfiltration ──
	regexp.MustCompile(`\bcurl\b.*\|\s*(ba)?sh\b`),                                              // curl | sh
	regexp.MustCompile(`\bcurl\b.*(-d\b|-F\b|--data|--upload|--form|-T\b|-X\s*P(UT|OST|ATCH))`), // curl POST/PUT
	regexp.MustCompile(`\bwget\b.*-O\s*-\s*\|\s*(ba)?sh\b`),                                     // wget | sh
	regexp.MustCompile(`\bwget\b.*--post-(data|file)`),                                          // wget POST
	regexp.MustCompile(`\b(nslookup|dig|host)\b`),                                               // DNS exfiltration
	regexp.MustCompile(`/dev/tcp/`),                                                             // bash tcp redirect

	// ── Reverse shells ──
	regexp.MustCompile(`\b(nc|ncat|netcat)\b.*-[el]\b`),
	regexp.MustCompile(`\bsocat\b`),
	regexp.MustCompile(`\bopenssl\b.*s_client`),
	regexp.MustCompile(`\btelnet\b.*\d+`),
	regexp.MustCompile(`\bpython[23]?\b.*\bimport\s+(socket|http\.client|urllib|requests)\b`),
	regexp.MustCompile(`\bperl\b.*-e\s*.*\b[Ss]ocket\b`),
	regexp.MustCompile(`\bruby\b.*-e\s*.*\b(TCPSocket|Socket)\b`),
	regexp.MustCompile(`\bnode\b.*-e\s*.*\b(net\.connect|child_process)\b`),
	regexp.MustCompile(`\bawk\b.*/inet/`), // awk built-in networking
	regexp.MustCompile(`\bmkfifo\b`),      // named pipes for shell redirection

	// ── Dangerous eval / code injection ──
	regexp.MustCompile(`\beval\s*\$`),
	regexp.MustCompile(`\bbase64\s+-d\b.*\|\s*(ba)?sh\b`),

	// ── Privilege escalation ──
	regexp.MustCompile(`\bsudo\b`),
	regexp.MustCompile(`\bsu\s+-`),
	regexp.MustCompile(`\bnsenter\b`),
	regexp.MustCompile(`\bunshare\b`),
	regexp.MustCompile(`\b(mount|umount)\b`),
	regexp.MustCompile(`\b(capsh|setcap|getcap)\b`),

	// ── Dangerous path operations ──
	regexp.MustCompile(`\bchmod\s+[0-7]{3,4}\s+/`),
	regexp.MustCompile(`\bchown\b.*\s+/`),
	regexp.MustCompile(`\bchmod\b.*\+x.*/tmp/`), // make tmpfs executable
	regexp.MustCompile(`\bchmod\b.*\+x.*/var/tmp/`),
	regexp.MustCompile(`\bchmod\b.*\+x.*/dev/shm/`),

	// ── Environment variable injection ──
	regexp.MustCompile(`\bLD_PRELOAD\s*=`),
	regexp.MustCompile(`\bDYLD_INSERT_LIBRARIES\s*=`),
	regexp.MustCompile(`\bLD_LIBRARY_PATH\s*=`),
	regexp.MustCompile(`/etc/ld\.so\.preload`),
	regexp.MustCompile(`\bGIT_EXTERNAL_DIFF\s*=`), // git diff arbitrary code exec
	regexp.MustCompile(`\bGIT_DIFF_OPTS\s*=`),     // git diff behavior injection
	regexp.MustCompile(`\bBASH_ENV\s*=`),          // shell init injection
	regexp.MustCompile(`\bENV\s*=.*\bsh\b`),       // sh init injection

	// ── Container escape ──
	regexp.MustCompile(`/var/run/docker\.sock|docker\.(sock|socket)`),
	regexp.MustCompile(`/proc/sys/(kernel|fs|net)/`),      // proc writes
	regexp.MustCompile(`/sys/(kernel|fs|class|devices)/`), // sysfs manipulation

	// ── Crypto mining ──
	regexp.MustCompile(`\b(xmrig|cpuminer|minerd|cgminer|bfgminer|ethminer|nbminer|t-rex|phoenixminer|lolminer|gminer|claymore)\b`),
	regexp.MustCompile(`stratum\+tcp://|stratum\+ssl://`),

	// ── Filter bypass (Claude Code CVE-2025-66032) ──
	regexp.MustCompile(`\bsed\b.*['"]/e\b`),                               // sed /e command execution
	regexp.MustCompile(`\bsort\b.*--compress-program`),                    // sort arbitrary exec
	regexp.MustCompile(`\bgit\b.*(--upload-pack|--receive-pack|--exec)=`), // git exec flags
	regexp.MustCompile(`\b(rg|grep)\b.*--pre=`),                           // preprocessor execution
	regexp.MustCompile(`\bman\b.*--html=`),                                // man command injection
	regexp.MustCompile(`\bhistory\b.*-[saw]\b`),                           // history file injection
	regexp.MustCompile(`\$\{[^}]*@[PpEeAaKk]\}`),                          // ${var@P} parameter expansion

	// ── Network abuse / reconnaissance ──
	regexp.MustCompile(`\b(nmap|masscan|zmap|rustscan)\b`),
	regexp.MustCompile(`\b(ssh|scp|sftp)\b.*@`),                               // outbound SSH
	regexp.MustCompile(`\b(chisel|frp|ngrok|cloudflared|bore|localtunnel)\b`), // tunneling tools

	// ── Persistence ──
	regexp.MustCompile(`\bcrontab\b`),
	regexp.MustCompile(`>\s*~/?\.(bashrc|bash_profile|profile|zshrc)`), // shell RC injection
	regexp.MustCompile(`\btee\b.*\.(bashrc|bash_profile|profile|zshrc)`),

	// ── Process manipulation ──
	regexp.MustCompile(`\bkill\s+-9\s`),
	regexp.MustCompile(`\b(killall|pkill)\b`),

	// ── Environment variable dumping ──
	// Bare env/printenv/set/export dumps all vars including secrets (API keys, DSN, encryption keys).
	// 'env VAR=val cmd' (env with assignment before command) is still allowed.
	regexp.MustCompile(`^\s*env\s*$`),                                 // bare 'env'
	regexp.MustCompile(`^\s*env\s*\|`),                                // 'env | ...' (piped)
	regexp.MustCompile(`^\s*env\s*>\s`),                               // 'env > file'
	regexp.MustCompile(`\bprintenv\b`),                                // any printenv usage
	regexp.MustCompile(`^\s*(set|export\s+-p|declare\s+-x)\s*($|\|)`), // shell var dumps
	regexp.MustCompile(`\bcompgen\s+-e\b`),                            // bash env completion dump
	regexp.MustCompile(`/proc/[^/]+/environ`),                         // /proc/PID/environ (leaks all env vars)
	regexp.MustCompile(`/proc/self/environ`),                          // /proc/self/environ
	regexp.MustCompile(`(?i)\bstrings\b.*/proc/`),                     // strings on /proc files (binary env dump)
}

// ExecTool executes shell commands, optionally inside a sandbox container.
type ExecTool struct {
	workingDir     string
	timeout        time.Duration
	denyPatterns   []*regexp.Regexp
	denyExemptions []string // substrings that exempt a command from deny (e.g. ".goclaw/skills-store/")
	restrict       bool
	sandboxMgr     sandbox.Manager      // nil = no sandbox, execute on host
	approvalMgr    *ExecApprovalManager // nil = no approval needed
	agentID        string               // for approval request context
}

// NewExecTool creates an exec tool that runs commands directly on the host.
func NewExecTool(workingDir string, restrict bool) *ExecTool {
	return &ExecTool{
		workingDir:   workingDir,
		timeout:      60 * time.Second,
		denyPatterns: defaultDenyPatterns,
		restrict:     restrict,
	}
}

// NewSandboxedExecTool creates an exec tool that routes commands through a sandbox container.
func NewSandboxedExecTool(workingDir string, restrict bool, mgr sandbox.Manager) *ExecTool {
	return &ExecTool{
		workingDir:   workingDir,
		timeout:      300 * time.Second, // sandbox allows longer timeout
		denyPatterns: defaultDenyPatterns,
		restrict:     restrict,
		sandboxMgr:   mgr,
	}
}

// SetSandboxKey is a no-op; sandbox key is now read from ctx (thread-safe).
func (t *ExecTool) SetSandboxKey(key string) {}

// DenyPaths adds dynamic deny patterns that block commands referencing the given paths.
// Used to prevent exec from reading/copying files from sensitive directories (e.g. data dir, .goclaw).
func (t *ExecTool) DenyPaths(paths ...string) {
	for _, p := range paths {
		escaped := regexp.QuoteMeta(p)
		t.denyPatterns = append(t.denyPatterns, regexp.MustCompile(escaped))
	}
}

// AllowPathExemptions adds substrings that exempt a command from deny pattern matches.
// E.g. AllowPathExemptions(".goclaw/skills-store/") lets commands referencing
// skills-store pass even though ".goclaw/" is denied.
func (t *ExecTool) AllowPathExemptions(substrings ...string) {
	t.denyExemptions = append(t.denyExemptions, substrings...)
}

// SetApprovalManager sets the exec approval manager for this tool.
func (t *ExecTool) SetApprovalManager(mgr *ExecApprovalManager, agentID string) {
	t.approvalMgr = mgr
	t.agentID = agentID
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
				"description": "Optional working directory for the command",
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

	// Check for dangerous commands (applies to both host and sandbox)
	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(command) {
			// Check if any exemption applies (e.g. skills-store within .goclaw)
			exempt := false
			for _, ex := range t.denyExemptions {
				if strings.Contains(command, ex) {
					exempt = true
					break
				}
			}
			if !exempt {
				return ErrorResult(fmt.Sprintf("command denied by safety policy: matches pattern %s", pattern.String()))
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

	// Use per-user workspace from context if available, fallback to struct field
	cwd := ToolWorkspaceFromCtx(ctx)
	if cwd == "" {
		cwd = t.workingDir
	}
	if wd, _ := args["working_dir"].(string); wd != "" {
		if effectiveRestrict(ctx, t.restrict) {
			resolved, err := resolvePath(wd, t.workingDir, true)
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

// executeOnHost runs a command directly on the host (original behavior).
func (t *ExecTool) executeOnHost(ctx context.Context, command, cwd string) *Result {
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

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
		if ctx.Err() == context.DeadlineExceeded {
			return ErrorResult(fmt.Sprintf("command timed out after %s", t.timeout))
		}
		if result == "" {
			result = err.Error()
		}
		return ErrorResult(result)
	}

	if result == "" {
		result = "(command completed with no output)"
	}

	return SilentResult(result)
}

// executeInSandbox routes a command through a Docker sandbox container.
func (t *ExecTool) executeInSandbox(ctx context.Context, command, cwd, sandboxKey string) *Result {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workingDir, SandboxConfigFromCtx(ctx))
	if err != nil {
		if errors.Is(err, sandbox.ErrSandboxDisabled) {
			return t.executeOnHost(ctx, command, cwd)
		}
		// Docker unavailable (binary missing, daemon down) → fallback to host
		slog.Warn("sandbox unavailable, falling back to host exec",
			"error", err,
			"command", truncateCmd(command, 80),
		)
		return t.executeOnHost(ctx, command, cwd)
	}

	// Map host workdir to container workdir
	containerCwd := "/workspace"
	if cwd != t.workingDir {
		rel, relErr := filepath.Rel(t.workingDir, cwd)
		if relErr == nil {
			containerCwd = filepath.Join("/workspace", rel)
		}
	}

	result, err := sb.Exec(ctx, []string{"sh", "-c", command}, containerCwd)
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
		return ErrorResult(output)
	}
	if output == "" {
		output = "(command completed with no output)"
	}

	return SilentResult(output)
}
