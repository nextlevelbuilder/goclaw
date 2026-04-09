package providers

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// defaultCursorCLIWorkDir returns the base workspace directory for Cursor CLI sessions.
func defaultCursorCLIWorkDir() string {
	return filepath.Join(config.ResolvedDataDirFromEnv(), "cursor-workspaces")
}

// ensureWorkDir creates and returns a stable work directory for the given session key.
func (p *CursorCLIProvider) ensureWorkDir(sessionKey string) string {
	safe := sanitizePathSegment(sessionKey)
	dir := filepath.Join(p.baseWorkDir, safe)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("cursor-cli: failed to create workdir", "dir", dir, "error", err)
		return os.TempDir()
	}
	return dir
}

// writeAgentsMD writes the system prompt to AGENTS.md in the work directory.
// Cursor agent reads this file automatically on every run.
// Skips write if content is unchanged to avoid unnecessary disk I/O.
func (p *CursorCLIProvider) writeAgentsMD(workDir, systemPrompt string) {
	path := filepath.Join(workDir, "AGENTS.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("cursor-cli: failed to write AGENTS.md", "path", path, "error", err)
	}
}

// buildArgs constructs CLI arguments for a Cursor agent invocation.
func (p *CursorCLIProvider) buildArgs(model, workDir string, hasMCP bool, chatID, outputFormat string) []string {
	mode := strings.ToLower(strings.TrimSpace(p.permMode))
	if mode == "" {
		mode = "force"
	}
	args := []string{
		"--print",
		"--output-format", outputFormat,
		"--model", model,
	}
	// default / strict: omit --force (stricter tool policy; may be unsuitable for unattended --print)
	if mode != "default" && mode != "strict" {
		args = append(args, "--force") // bypass confirmation prompts (--yolo is alias)
	}
	args = append(args, "--trust") // skip workspace prompts in headless mode
	if mode == "sandbox" {
		args = append(args, "--sandbox", "enabled")
	}
	args = append(args, "--workspace", workDir)
	if hasMCP {
		args = append(args, "--approve-mcps")
	}
	if chatID != "" {
		args = append(args, "--resume", chatID)
	}
	return args
}

type cursorCLISettingsJSON struct {
	PermMode string `json:"perm_mode"`
}

// PermModeFromCursorCLISettings returns perm_mode from llm_providers.settings JSON (empty if unset).
func PermModeFromCursorCLISettings(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var s cursorCLISettingsJSON
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s.PermMode)
}

// readCursorSessionID returns the persisted chat ID from workdir/.cursor_session_id.
// Returns "" if the file does not exist or cannot be read.
func readCursorSessionID(workDir string) string {
	data, err := os.ReadFile(filepath.Join(workDir, ".cursor_session_id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeCursorSessionID persists the server-assigned chat ID to workdir/.cursor_session_id.
func writeCursorSessionID(workDir, chatID string) {
	if chatID == "" {
		return
	}
	path := filepath.Join(workDir, ".cursor_session_id")
	if err := os.WriteFile(path, []byte(chatID), 0600); err != nil {
		slog.Warn("cursor-cli: failed to write session ID", "path", path, "error", err)
	}
}

// filterCursorEnv removes inherited CURSOR* env vars so the `agent` subprocess uses on-disk auth from `agent login`,
// not a leaked key from the gateway process environment.
func filterCursorEnv(environ []string) []string {
	var filtered []string
	for _, e := range environ {
		key := e
		if before, _, ok := strings.Cut(e, "="); ok {
			key = before
		}
		if strings.HasPrefix(key, "CURSOR") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// ResetCursorCLISession deletes session state for a given session key.
// Called on /reset to ensure the agent starts fresh.
func ResetCursorCLISession(baseWorkDir, sessionKey string) {
	if baseWorkDir == "" {
		baseWorkDir = defaultCursorCLIWorkDir()
	}
	safe := sanitizePathSegment(sessionKey)
	workDir := filepath.Join(baseWorkDir, safe)

	for _, rel := range []string{".cursor_session_id", "AGENTS.md", ".cursor/mcp.json"} {
		path := filepath.Join(workDir, rel)
		if err := os.Remove(path); err == nil {
			slog.Info("cursor-cli: deleted on /reset", "path", path)
		}
	}
}
