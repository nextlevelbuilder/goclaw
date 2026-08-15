package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// BuildCLIHooksConfig generates a Claude CLI settings file with PreToolUse hooks
// that enforce GoClaw's security policies (shell deny patterns, path restrictions).
// Returns settings file path and a cleanup function.
func BuildCLIHooksConfig(workspace string, restrictToWorkspace bool, denyPatternSets ...[]*regexp.Regexp) (string, func(), error) {
	tmpDir := filepath.Join(os.TempDir(), "goclaw-cli-hooks")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", nil, fmt.Errorf("create hooks dir: %w", err)
	}

	id := uuid.New().String()[:8]

	// Write the hook script
	hookScript := generateHookScript(workspace, restrictToWorkspace, denyPatternSets...)
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
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "Bash",
					"hooks": []map[string]any{
						{"type": "command", "command": hookPath},
					},
				},
				{
					"matcher": "Write",
					"hooks": []map[string]any{
						{"type": "command", "command": hookPath},
					},
				},
				{
					"matcher": "Edit",
					"hooks": []map[string]any{
						{"type": "command", "command": hookPath},
					},
				},
				{
					"matcher": "Read",
					"hooks": []map[string]any{
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
func generateHookScript(workspace string, restrictToWorkspace bool, denyPatternSets ...[]*regexp.Regexp) string {
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

	for _, p := range hookDenyPatternStrings(denyPatternSets...) {
		// Escape single quotes for bash
		escaped := strings.ReplaceAll(p, `'`, `'\''`)
		fmt.Fprintf(&sb, "    '%s'\n", escaped)
	}

	sb.WriteString(`  )

  for pattern in "${patterns[@]}"; do
    if echo "$cmd" | grep -qE "$pattern" 2>/dev/null; then
      deny "security: shell command blocked by deny pattern"
    fi
  done
}

`)

	// Path restriction check
	if restrictToWorkspace && workspace != "" {
		// Escape workspace path for safe bash embedding (single quotes + quote escaping)
		safeWorkspace := strings.ReplaceAll(workspace, `'`, `'\''`)
		fmt.Fprintf(&sb, `# === Workspace path restriction ===
WORKSPACE='%s'

check_path_restriction() {
  local file_path="$1"
  # Resolve all paths (including relative) to absolute for proper checking
  local resolved
  resolved=$(realpath -m "$file_path" 2>/dev/null || echo "$file_path")
  if [[ "$resolved" != "$WORKSPACE"* ]]; then
    deny "security: path outside workspace boundary"
  fi
}

`, safeWorkspace)
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

func hookDenyPatternStrings(denyPatternSets ...[]*regexp.Regexp) []string {
	if len(denyPatternSets) == 0 {
		return ShellDenyPatterns
	}
	patterns := make([]string, 0, len(denyPatternSets[0]))
	for _, p := range denyPatternSets[0] {
		if p == nil {
			continue
		}
		patterns = append(patterns, p.String())
	}
	return patterns
}
