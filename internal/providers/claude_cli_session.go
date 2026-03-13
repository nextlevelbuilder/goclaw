package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// validCLIModels lists accepted model aliases for the Claude CLI.
var validCLIModels = map[string]bool{
	"sonnet": true, "opus": true, "haiku": true,
}

// validateCLIModel checks if a model alias is supported by the Claude CLI.
func validateCLIModel(model string) error {
	if !validCLIModels[model] {
		return fmt.Errorf("claude-cli: unsupported model %q (valid: sonnet, opus, haiku)", model)
	}
	return nil
}

// buildArgs constructs CLI arguments.
// mcpConfigPath is the resolved per-session MCP config file (may differ per call).
func (p *ClaudeCLIProvider) buildArgs(model, workDir, mcpConfigPath string, cliSessionID uuid.UUID, outputFormat string, hasImages, disableTools bool) []string {
	args := []string{
		"-p",
		"--output-format", outputFormat,
		"--model", model,
		"--permission-mode", p.permMode,
		"--verbose",
	}

	if mcpConfigPath != "" {
		args = append(args, "--mcp-config", mcpConfigPath)
	}

	// Session persistence: check if CLI session file exists on disk.
	// If exists → --resume (continue conversation). If not → --session-id (create new).
	// Session files live at ~/.claude/projects/<sanitized-workdir>/<uuid>.jsonl
	sid := cliSessionID.String()
	if sessionFileExists(workDir, cliSessionID) {
		args = append(args, "--resume", sid)
	} else {
		args = append(args, "--session-id", sid)
	}

	if hasImages {
		args = append(args, "--input-format", "stream-json")
	}

	if disableTools {
		// Summoner: disable all tools entirely via disallowedTools
		args = append(args, "--disallowedTools", "Bash,Edit,Read,Write,Glob,Grep,WebFetch,WebSearch,TodoRead,TodoWrite,NotebookRead,NotebookEdit")
	} else if mcpConfigPath != "" {
		// Chat with MCP bridge: disable CLI built-in tools, only allow MCP bridge tools.
		// This ensures all tool execution goes through GoClaw's controlled MCP bridge.
		args = append(args, "--disallowedTools", "Bash,Edit,Read,Write,Glob,Grep,WebFetch,WebSearch,TodoRead,TodoWrite,NotebookRead,NotebookEdit")
	}

	if p.hooksSettingsPath != "" {
		args = append(args, "--settings", p.hooksSettingsPath)
	}

	return args
}

// resolveMCPConfigPath writes a per-session MCP config with agent context and returns its path.
func (p *ClaudeCLIProvider) resolveMCPConfigPath(ctx context.Context, sessionKey string, bc BridgeContext) string {
	if p.mcpConfigData == nil {
		return ""
	}
	path := p.mcpConfigData.WriteMCPConfig(ctx, sessionKey, bc)
	if path != "" {
		p.mcpConfigDirs.Store(filepath.Dir(path), struct{}{})
	}
	return path
}

// ensureWorkDir creates and returns a stable work directory for the given session key.
func (p *ClaudeCLIProvider) ensureWorkDir(sessionKey string) string {
	// Sanitize session key for filesystem safety (path traversal, null bytes, length)
	safe := sanitizePathSegment(sessionKey)
	dir := filepath.Join(p.baseWorkDir, safe)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("claude-cli: failed to create workdir", "dir", dir, "error", err)
		return os.TempDir()
	}
	return dir
}

// writeClaudeMD writes the system prompt to CLAUDE.md in the work directory.
// CLI reads this file automatically on every run (including --resume).
// Skips write if content is unchanged to avoid unnecessary disk I/O.
func (p *ClaudeCLIProvider) writeClaudeMD(workDir, systemPrompt string) {
	path := filepath.Join(workDir, "CLAUDE.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("claude-cli: failed to write CLAUDE.md", "path", path, "error", err)
	}
}

// extractFromMessages extracts system prompt, last user message, and images from the messages array.
func extractFromMessages(msgs []Message) (systemPrompt, userMsg string, images []ImageContent) {
	for _, m := range msgs {
		if m.Role == "system" {
			systemPrompt = m.Content
		}
	}
	// Find last user message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userMsg = msgs[i].Content
			images = msgs[i].Images
			break
		}
	}
	return
}

// extractStringOpt gets a string value from Options map by key.
func extractStringOpt(opts map[string]any, key string) string {
	if opts == nil {
		return ""
	}
	if v, ok := opts[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractBoolOpt gets a bool value from Options map by key.
func extractBoolOpt(opts map[string]any, key string) bool {
	if opts == nil {
		return false
	}
	if v, ok := opts[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// bridgeContextFromOpts builds a BridgeContext from the Options map.
func bridgeContextFromOpts(opts map[string]any) BridgeContext {
	return BridgeContext{
		AgentID:  extractStringOpt(opts, OptAgentID),
		UserID:   extractStringOpt(opts, OptUserID),
		Channel:  extractStringOpt(opts, OptChannel),
		ChatID:   extractStringOpt(opts, OptChatID),
		PeerKind: extractStringOpt(opts, OptPeerKind),
	}
}

// defaultCLIWorkDir returns ~/.goclaw/cli-workspaces, falling back to temp dir.
func defaultCLIWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "goclaw-cli-workspaces")
	}
	return filepath.Join(home, ".goclaw", "cli-workspaces")
}

// deriveSessionUUID creates a deterministic UUID v5 from a session key string.
func deriveSessionUUID(sessionKey string) uuid.UUID {
	if sessionKey == "" {
		return uuid.New() // fallback: random
	}
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(sessionKey))
}

// sessionFileExists checks if a Claude CLI session file exists for the given work directory.
// Claude CLI resolves symlinks (e.g. /var/folders → /private/var/folders on macOS)
// before encoding the path, so we must do the same.
func sessionFileExists(workDir string, sessionID uuid.UUID) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	// Resolve symlinks to match CLI's path encoding (macOS: /var → /private/var)
	resolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		resolved = workDir
	}
	// Claude CLI stores sessions at: ~/.claude/projects/<encoded-path>/<session-id>.jsonl
	// CLI replaces path separators, "_", ".", and ":" with "-" in the path encoding.
	// On Windows: C:\Users\foo → C--Users-foo (backslash + colon both become "-")
	// On macOS/Linux: /home/foo → -home-foo (forward slash becomes "-")
	encoded := strings.NewReplacer(string(filepath.Separator), "-", "_", "-", ".", "-", ":", "-").Replace(resolved)
	sessionFile := filepath.Join(home, ".claude", "projects", encoded, sessionID.String()+".jsonl")
	_, err = os.Stat(sessionFile)
	return err == nil
}

// buildStreamJSONInput creates stream-json stdin for vision (images + text).
func buildStreamJSONInput(text string, images []ImageContent) *bytes.Reader {
	var contentBlocks []map[string]any

	for _, img := range images {
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": img.MimeType,
				"data":       img.Data,
			},
		})
	}

	if text != "" {
		contentBlocks = append(contentBlocks, map[string]any{
			"type": "text",
			"text": text,
		})
	}

	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": contentBlocks,
		},
	}

	data, _ := json.Marshal(msg)
	return bytes.NewReader(data)
}

// ResetCLISession deletes the Claude CLI session file and CLAUDE.md for a given session key.
// Called on /reset to ensure the CLI starts fresh instead of --resume-ing poisoned history.
// Safe to call even if CLI provider is not in use (no-op if files don't exist).
func ResetCLISession(baseWorkDir, sessionKey string) {
	if baseWorkDir == "" {
		baseWorkDir = defaultCLIWorkDir()
	}
	safe := sanitizePathSegment(sessionKey)
	workDir := filepath.Join(baseWorkDir, safe)
	sessionID := deriveSessionUUID(sessionKey)

	// Delete CLI session .jsonl file from ~/.claude/projects/
	home, err := os.UserHomeDir()
	if err == nil {
		resolved, err := filepath.EvalSymlinks(workDir)
		if err != nil {
			resolved = workDir
		}
		encoded := strings.NewReplacer(string(filepath.Separator), "-", "_", "-", ".", "-", ":", "-").Replace(resolved)
		sessionFile := filepath.Join(home, ".claude", "projects", encoded, sessionID.String()+".jsonl")
		if err := os.Remove(sessionFile); err == nil {
			slog.Info("claude-cli: deleted session file on /reset", "path", sessionFile)
		}
	}

	// Delete CLAUDE.md from workdir so it regenerates fresh
	claudeMD := filepath.Join(workDir, "CLAUDE.md")
	if err := os.Remove(claudeMD); err == nil {
		slog.Info("claude-cli: deleted CLAUDE.md on /reset", "path", claudeMD)
	}
}

// filterCLIEnv removes CLAUDE* env vars to prevent nested session conflicts.
func filterCLIEnv(environ []string) []string {
	var filtered []string
	for _, e := range environ {
		key := e
		if before, _, ok := strings.Cut(e, "="); ok {
			key = before
		}
		// Filter out variables that could cause nested CLI conflicts
		if strings.HasPrefix(key, "CLAUDE") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}
