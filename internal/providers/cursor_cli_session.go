package providers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// validCursorModes lists accepted working modes for the Cursor CLI.
var validCursorModes = map[string]bool{
	"agent": true,
	"plan":  true,
	"ask":   true,
}

func (p *CursorCLIProvider) buildArgs(model, workDir, chatID string, streaming bool) []string {
	args := []string{
		"-p",
		"--force",
		"--trust",
		"--workspace", workDir,
	}
	if streaming {
		args = append(args, "--output-format", "stream-json")
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	mode := p.defaultMode
	if !validCursorModes[mode] {
		mode = "agent"
	}
	if mode != "agent" {
		args = append(args, "--mode", mode)
	}
	if chatID != "" {
		args = append(args, "--resume", chatID)
	}
	return args
}

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

func (p *CursorCLIProvider) getOrCreateChatID(ctx context.Context, sessionKey string) (string, error) {
	if sessionKey == "" {
		return "", nil
	}

	registry := getCursorChatIDRegistry(p.baseWorkDir)
	if cached, ok := registry.Load(sessionKey); ok {
		return cached.(string), nil
	}

	workDir := p.ensureWorkDir(sessionKey)
	chatIDFile := filepath.Join(workDir, ".cursor", "chatid")
	if data, err := os.ReadFile(chatIDFile); err == nil {
		chatID := strings.TrimSpace(string(data))
		if chatID != "" {
			registry.Store(sessionKey, chatID)
			return chatID, nil
		}
	}

	chatID, err := p.createChat(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(chatIDFile), 0755); err == nil {
		if err := os.WriteFile(chatIDFile, []byte(chatID), 0600); err != nil {
			slog.Warn("cursor-cli: failed to write chat ID file", "path", chatIDFile, "error", err)
		}
	}
	registry.Store(sessionKey, chatID)
	slog.Info("cursor-cli: created new chat", "session_key", sessionKey, "chat_id", chatID)
	return chatID, nil
}

func (p *CursorCLIProvider) createChat(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, p.cliPath, "create-chat")
	cmd.Env = filterCursorEnv(os.Environ())

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cursor-cli create-chat: %w", err)
	}

	chatID := strings.TrimSpace(string(output))
	if chatID == "" {
		return "", fmt.Errorf("cursor-cli create-chat: empty response")
	}
	return chatID, nil
}

// ResetCursorCLISession deletes the Cursor CLI session state for a given session key.
// Called on /reset so the next message starts a fresh chat.
func ResetCursorCLISession(baseWorkDir, sessionKey string) {
	if baseWorkDir == "" {
		baseWorkDir = defaultCursorWorkDir()
	}

	registry := getCursorChatIDRegistry(baseWorkDir)
	if _, ok := registry.Load(sessionKey); ok {
		registry.Delete(sessionKey)
	}

	safe := sanitizePathSegment(sessionKey)
	workDir := filepath.Join(baseWorkDir, safe)
	chatIDFile := filepath.Join(workDir, ".cursor", "chatid")
	if err := os.Remove(chatIDFile); err == nil {
		slog.Info("cursor-cli: cleared chat ID on /reset", "session_key", sessionKey)
	}
}
