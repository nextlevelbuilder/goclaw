package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// ShellDenyPatterns are regex patterns for dangerous shell commands.
// Mirrors internal/tools/shell.go defaultDenyPatterns for CLI hook enforcement.
var ShellDenyPatterns = []string{
	// Destructive file operations
	`\brm\s+-[rf]{1,2}\b`,
	`\brm\s+.*--recursive`,
	`\brm\s+.*--force`,
	`\bdel\s+/[fq]\b`,
	`\brmdir\s+/s\b`,
	`\b(mkfs|diskpart)\b|\bformat\s`,
	`\bdd\s+if=`,
	`>\s*/dev/sd[a-z]\b`,
	`\b(shutdown|reboot|poweroff)\b`,
	`:\(\)\s*\{.*\};\s*:`,

	// Data exfiltration
	`\bcurl\b.*\|\s*(ba)?sh\b`,
	`\bcurl\b.*(-d\b|-F\b|--data|--upload|--form|-T\b|-X\s*P(UT|OST|ATCH))`,
	`\bwget\b.*-O\s*-\s*\|\s*(ba)?sh\b`,
	`\bwget\b.*--post-(data|file)`,
	`\b(nslookup|dig|host)\b`,
	`/dev/tcp/`,

	// Reverse shells
	`\b(nc|ncat|netcat)\b.*-[el]\b`,
	`\bsocat\b`,
	`\bopenssl\b.*s_client`,
	`\btelnet\b.*[0-9]+`,
	`\bpython[23]?\b.*\bimport\s+(socket|http\.client|urllib|requests)\b`,
	`\bperl\b.*-e\s*.*\b[Ss]ocket\b`,
	`\bruby\b.*-e\s*.*\b(TCPSocket|Socket)\b`,
	`\bnode\b.*-e\s*.*\b(net\.connect|child_process)\b`,
	`\bawk\b.*/inet/`,
	`\bmkfifo\b`,

	// Dangerous eval / code injection
	`\beval\s*\$`,
	`\bbase64\s+-d\b.*\|\s*(ba)?sh\b`,

	// Privilege escalation
	`\bsudo\b`,
	`\bsu\s+-`,
	`\bnsenter\b`,
	`\bunshare\b`,
	`\b(mount|umount)\b`,
	`\b(capsh|setcap|getcap)\b`,

	// Dangerous path operations
	`\bchmod\s+[0-7]{3,4}\s+/`,
	`\bchown\b.*\s+/`,
	`\bchmod\b.*\+x.*/tmp/`,
	`\bchmod\b.*\+x.*/var/tmp/`,
	`\bchmod\b.*\+x.*/dev/shm/`,

	// Environment variable injection
	`\bLD_PRELOAD\s*=`,
	`\bDYLD_INSERT_LIBRARIES\s*=`,
	`\bLD_LIBRARY_PATH\s*=`,
	`/etc/ld\.so\.preload`,
	`\bGIT_EXTERNAL_DIFF\s*=`,
	`\bGIT_DIFF_OPTS\s*=`,
	`\bBASH_ENV\s*=`,
	`\bENV\s*=.*\bsh\b`,

	// Container escape
	`/var/run/docker\.sock|docker\.(sock|socket)`,
	`/proc/sys/(kernel|fs|net)/`,
	`/sys/(kernel|fs|class|devices)/`,

	// Crypto mining
	`\b(xmrig|cpuminer|minerd|cgminer|bfgminer|ethminer|nbminer|t-rex|phoenixminer|lolminer|gminer|claymore)\b`,
	`stratum\+tcp://|stratum\+ssl://`,

	// Filter bypass (CVE-2025-66032)
	`\bsed\b.*['"]/e\b`,
	`\bsort\b.*--compress-program`,
	`\bgit\b.*(--upload-pack|--receive-pack|--exec)=`,
	`\b(rg|grep)\b.*--pre=`,
	`\bman\b.*--html=`,
	`\bhistory\b.*-[saw]\b`,
	`\$\{[^}]*@[PpEeAaKk]\}`,

	// Network abuse / reconnaissance
	`\b(nmap|masscan|zmap|rustscan)\b`,
	`\b(ssh|scp|sftp)\b.*@`,
	`\b(chisel|frp|ngrok|cloudflared|bore|localtunnel)\b`,

	// Persistence
	`\bcrontab\b`,
	`>\s*~/?\.(bashrc|bash_profile|profile|zshrc)`,
	`\btee\b.*\.(bashrc|bash_profile|profile|zshrc)`,

	// Process manipulation
	`\bkill\s+-9\s`,
	`\b(killall|pkill)\b`,

	// Environment variable dumping
	`^\s*env\s*$`,
	`^\s*env\s*\|`,
	`^\s*env\s*>\s`,
	`\bprintenv\b`,
	`^\s*(set|export\s+-p|declare\s+-x)\s*($|\|)`,
	`\bcompgen\s+-e\b`,
	`/proc/[^/]+/environ`,
	`/proc/self/environ`,
	`(?i)\bstrings\b.*/proc/`,
}

// BuildCLIHooksConfig generates a Claude CLI settings file with PreToolUse hooks
// that enforce GoClaw's security policies (shell deny patterns, path restrictions).
// Returns settings file path and a cleanup function.
func BuildCLIHooksConfig(workspace string, restrictToWorkspace bool) (string, func(), error) {
	tmpDir := filepath.Join(os.TempDir(), "goclaw-cli-hooks")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", nil, fmt.Errorf("create hooks dir: %w", err)
	}

	id := uuid.New().String()[:8]

	// Write the hook script
	hookScript := generateHookScript(workspace, restrictToWorkspace)
	hookPath := filepath.Join(tmpDir, fmt.Sprintf("hook-%s.sh", id))
	if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
		return "", nil, fmt.Errorf("write hook script: %w", err)
	}

	// Write settings JSON
	settings := generateSettingsJSON(hookPath)
	settingsPath := filepath.Join(tmpDir, fmt.Sprintf("settings-%s.json", id))
	if err := os.WriteFile(settingsPath, settings, 0600); err != nil {
		os.Remove(hookPath)
		return "", nil, fmt.Errorf("write settings: %w", err)
	}

	cleanup := func() {
		os.Remove(hookPath)
		os.Remove(settingsPath)
	}

	return settingsPath, cleanup, nil
}

// generateSettingsJSON creates Claude CLI settings with PreToolUse hooks.
func generateSettingsJSON(hookPath string) []byte {
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "Bash",
					"hooks": []map[string]interface{}{
						{"type": "command", "command": hookPath},
					},
				},
				{
					"matcher": "Write",
					"hooks": []map[string]interface{}{
						{"type": "command", "command": hookPath},
					},
				},
				{
					"matcher": "Edit",
					"hooks": []map[string]interface{}{
						{"type": "command", "command": hookPath},
					},
				},
				{
					"matcher": "Read",
					"hooks": []map[string]interface{}{
						{"type": "command", "command": hookPath},
					},
				},
			},
		},
	}

	data, _ := json.MarshalIndent(settings, "", "  ")
	return data
}

// generateHookScript creates a bash script that enforces GoClaw security policies.
func generateHookScript(workspace string, restrictToWorkspace bool) string {
	var sb strings.Builder

	sb.WriteString(`#!/bin/bash
set -euo pipefail

# GoClaw security hook for Claude CLI PreToolUse.
# Checks shell deny patterns and workspace path restrictions.

INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // empty')
TOOL_INPUT=$(echo "$INPUT" | jq -c '.tool_input // {}')

allow() {
  echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
  exit 0
}

deny() {
  local reason="$1"
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"$reason\"}}"
  exit 0
}

`)

	// Shell deny patterns check
	sb.WriteString(`# === Shell command deny patterns ===
check_shell_deny() {
  local cmd="$1"
  local patterns=(
`)

	for _, p := range ShellDenyPatterns {
		// Escape single quotes for bash
		escaped := strings.ReplaceAll(p, `'`, `'\''`)
		fmt.Fprintf(&sb, "    '%s'\n", escaped)
	}

	sb.WriteString(`  )

  for pattern in "${patterns[@]}"; do
    if echo "$cmd" | grep -qP "$pattern" 2>/dev/null || echo "$cmd" | grep -qE "$pattern" 2>/dev/null; then
      deny "security: shell command blocked by deny pattern"
    fi
  done
}

`)

	// Path restriction check
	if restrictToWorkspace && workspace != "" {
		fmt.Fprintf(&sb, `# === Workspace path restriction ===
WORKSPACE="%s"

check_path_restriction() {
  local file_path="$1"
  # Resolve to absolute path
  if [[ "$file_path" != /* ]]; then
    return 0  # relative paths are OK (resolved by CLI relative to workdir)
  fi
  # Check if path is within workspace
  local resolved
  resolved=$(realpath -m "$file_path" 2>/dev/null || echo "$file_path")
  if [[ "$resolved" != "$WORKSPACE"* ]]; then
    deny "security: path outside workspace boundary"
  fi
}

`, workspace)
	}

	// Main dispatch
	sb.WriteString(`# === Main ===
case "$TOOL_NAME" in
  Bash)
    CMD=$(echo "$TOOL_INPUT" | jq -r '.command // empty')
    if [ -n "$CMD" ]; then
      check_shell_deny "$CMD"
    fi
    ;;
  Write)
    FILE_PATH=$(echo "$TOOL_INPUT" | jq -r '.file_path // empty')
`)

	if restrictToWorkspace && workspace != "" {
		sb.WriteString(`    if [ -n "$FILE_PATH" ]; then
      check_path_restriction "$FILE_PATH"
    fi
`)
	}

	sb.WriteString(`    ;;
  Edit)
    FILE_PATH=$(echo "$TOOL_INPUT" | jq -r '.file_path // empty')
`)

	if restrictToWorkspace && workspace != "" {
		sb.WriteString(`    if [ -n "$FILE_PATH" ]; then
      check_path_restriction "$FILE_PATH"
    fi
`)
	}

	sb.WriteString(`    ;;
  Read)
    FILE_PATH=$(echo "$TOOL_INPUT" | jq -r '.file_path // empty')
`)

	if restrictToWorkspace && workspace != "" {
		sb.WriteString(`    if [ -n "$FILE_PATH" ]; then
      check_path_restriction "$FILE_PATH"
    fi
`)
	}

	sb.WriteString(`    ;;
esac

# Default: allow
allow
`)

	return sb.String()
}
