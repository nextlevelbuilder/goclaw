package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Options key for passing session key from agent loop to CLI provider.
const OptSessionKey = "session_key"

// OptDisableTools disables all built-in CLI tools when set to true.
// Useful for pure text generation (e.g. summoning) where tool use is unwanted.
const OptDisableTools = "disable_tools"

// ClaudeCLIProvider implements Provider by shelling out to the `claude` CLI binary.
// It acts as a thin proxy: CLI manages session history, tool execution, and context.
// GoClaw only forwards the latest user message and streams back the response.
type ClaudeCLIProvider struct {
	cliPath            string // path to claude binary (default: "claude")
	defaultModel       string // default: "sonnet"
	baseWorkDir        string // base dir for agent workspaces
	mcpConfigPath      string // pre-built MCP config file path (empty = no MCP)
	permMode           string // permission mode (default: "bypassPermissions")
	hooksSettingsPath  string // generated settings.json with security hooks (empty = no hooks)
	hooksCleanup       func() // cleanup function for hooks temp files
	mcpCleanup         func() // cleanup function for MCP config temp file
	mu                 sync.Mutex                // protects sessionMu map + workdir creation
	sessionMu          map[string]*sync.Mutex    // per-session mutex to prevent concurrent CLI calls
}

// ClaudeCLIOption configures the provider.
type ClaudeCLIOption func(*ClaudeCLIProvider)

// WithClaudeCLIModel sets the default model alias.
func WithClaudeCLIModel(model string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

// WithClaudeCLIWorkDir sets the base work directory.
func WithClaudeCLIWorkDir(dir string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

// WithClaudeCLIMCPConfig sets the MCP config file path.
func WithClaudeCLIMCPConfig(path string, cleanup ...func()) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		p.mcpConfigPath = path
		if len(cleanup) > 0 && cleanup[0] != nil {
			p.mcpCleanup = cleanup[0]
		}
	}
}

// WithClaudeCLIPermMode sets the permission mode.
func WithClaudeCLIPermMode(mode string) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		if mode != "" {
			p.permMode = mode
		}
	}
}

// WithClaudeCLISecurityHooks enables GoClaw security hooks for CLI tool calls.
// Generates a settings file with PreToolUse hooks that enforce shell deny patterns
// and workspace path restrictions.
func WithClaudeCLISecurityHooks(workspace string, restrictToWorkspace bool) ClaudeCLIOption {
	return func(p *ClaudeCLIProvider) {
		settingsPath, cleanup, err := BuildCLIHooksConfig(workspace, restrictToWorkspace)
		if err != nil {
			slog.Warn("claude-cli: failed to build security hooks", "error", err)
			return
		}
		p.hooksSettingsPath = settingsPath
		p.hooksCleanup = cleanup
	}
}

// NewClaudeCLIProvider creates a provider that invokes the claude CLI.
func NewClaudeCLIProvider(cliPath string, opts ...ClaudeCLIOption) *ClaudeCLIProvider {
	if cliPath == "" {
		cliPath = "claude"
	}
	p := &ClaudeCLIProvider{
		cliPath:      cliPath,
		defaultModel: "sonnet",
		baseWorkDir:  defaultCLIWorkDir(),
		permMode:     "bypassPermissions",
		sessionMu:    make(map[string]*sync.Mutex),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *ClaudeCLIProvider) Name() string         { return "claude-cli" }
func (p *ClaudeCLIProvider) DefaultModel() string  { return p.defaultModel }

// Close cleans up temp files (MCP config, hooks settings).
func (p *ClaudeCLIProvider) Close() {
	if p.mcpCleanup != nil {
		p.mcpCleanup()
	}
	if p.hooksCleanup != nil {
		p.hooksCleanup()
	}
}

// lockSession acquires a per-session mutex to prevent concurrent CLI calls on the same session.
func (p *ClaudeCLIProvider) lockSession(sessionKey string) func() {
	p.mu.Lock()
	m, ok := p.sessionMu[sessionKey]
	if !ok {
		m = &sync.Mutex{}
		p.sessionMu[sessionKey] = m
	}
	p.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Chat runs the CLI synchronously and returns the final response.
func (p *ClaudeCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	sessionKey := extractSessionKey(req.Options)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	unlock := p.lockSession(sessionKey)
	defer unlock()

	workDir := p.ensureWorkDir(sessionKey)
	if systemPrompt != "" {
		p.writeClaudeMD(workDir, systemPrompt)
	}

	cliSessionID := deriveSessionUUID(sessionKey)
	disableTools := extractDisableTools(req.Options)
	args := p.buildArgs(model, workDir, cliSessionID, "json", len(images) > 0, disableTools)

	var stdin *bytes.Reader
	if len(images) > 0 {
		stdin = buildStreamJSONInput(userMsg, images)
	} else {
		args = append(args, "--", userMsg)
	}

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterCLIEnv(os.Environ())
	if stdin != nil {
		cmd.Stdin = stdin
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	slog.Debug("claude-cli exec", "cmd", fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " ")), "workdir", workDir)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude-cli: %w (stderr: %s)", err, stderr.String())
	}

	return parseJSONResponse(output)
}

// ChatStream runs the CLI with stream-json output, calling onChunk for each text delta.
func (p *ClaudeCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	sessionKey := extractSessionKey(req.Options)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	slog.Debug("claude-cli: acquiring session lock", "session_key", sessionKey)
	unlock := p.lockSession(sessionKey)
	slog.Debug("claude-cli: session lock acquired", "session_key", sessionKey)
	defer func() {
		unlock()
		slog.Debug("claude-cli: session lock released", "session_key", sessionKey)
	}()

	workDir := p.ensureWorkDir(sessionKey)
	if systemPrompt != "" {
		p.writeClaudeMD(workDir, systemPrompt)
	}

	cliSessionID := deriveSessionUUID(sessionKey)
	disableToolsStream := extractDisableTools(req.Options)
	args := p.buildArgs(model, workDir, cliSessionID, "stream-json", len(images) > 0, disableToolsStream)

	var stdin *bytes.Reader
	if len(images) > 0 {
		stdin = buildStreamJSONInput(userMsg, images)
	} else {
		args = append(args, "--", userMsg)
	}

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterCLIEnv(os.Environ())
	if stdin != nil {
		cmd.Stdin = stdin
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude-cli stdout pipe: %w", err)
	}

	fullCmd := fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " "))
	slog.Debug("claude-cli stream exec", "cmd", fullCmd, "workdir", workDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude-cli start: %w", err)
	}

	// Debug log file: only enabled when GOCLAW_DEBUG=1
	var debugFile *os.File
	if os.Getenv("GOCLAW_DEBUG") == "1" {
		debugLogPath := filepath.Join(workDir, "cli-debug.log")
		debugFile, _ = os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if debugFile != nil {
			fmt.Fprintf(debugFile, "=== CMD: %s\n=== WORKDIR: %s\n=== TIME: %s\n\n", fullCmd, workDir, time.Now().Format(time.RFC3339))
			defer debugFile.Close()
		}
	}

	// Parse stream-json line-by-line
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024) // 256KB initial, 1MB max

	var finalResp ChatResponse
	var contentBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Write raw line to debug log
		if debugFile != nil {
			fmt.Fprintf(debugFile, "%s\n", line)
		}

		var ev cliStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("claude-cli: skip malformed stream line", "error", err)
			continue
		}

		switch ev.Type {
		case "assistant":
			if ev.Message == nil {
				continue
			}
			text, thinking := extractStreamContent(ev.Message)
			if text != "" {
				contentBuf.WriteString(text)
				onChunk(StreamChunk{Content: text})
			}
			if thinking != "" {
				onChunk(StreamChunk{Thinking: thinking})
			}

		case "result":
			if ev.Result != "" {
				finalResp.Content = ev.Result
			} else {
				finalResp.Content = contentBuf.String()
			}
			finalResp.FinishReason = "stop"
			if ev.Subtype == "error" {
				finalResp.FinishReason = "error"
			}
			if ev.Usage != nil {
				finalResp.Usage = &Usage{
					PromptTokens:     ev.Usage.InputTokens,
					CompletionTokens: ev.Usage.OutputTokens,
					TotalTokens:      ev.Usage.InputTokens + ev.Usage.OutputTokens,
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if debugFile != nil {
			fmt.Fprintf(debugFile, "\n=== STDERR:\n%s\n=== EXIT ERROR: %v\n", stderrBuf.String(), err)
		}
		// If we got partial content, return it with the error
		if finalResp.Content != "" {
			return &finalResp, nil
		}
		return nil, fmt.Errorf("claude-cli: %w (stderr: %s)", err, stderrBuf.String())
	}
	if debugFile != nil && stderrBuf.Len() > 0 {
		fmt.Fprintf(debugFile, "\n=== STDERR:\n%s\n", stderrBuf.String())
	}

	// Fallback if no "result" event was received
	if finalResp.Content == "" {
		finalResp.Content = contentBuf.String()
		finalResp.FinishReason = "stop"
	}

	onChunk(StreamChunk{Done: true})
	return &finalResp, nil
}

// --- internal helpers ---

// buildArgs constructs CLI arguments.
func (p *ClaudeCLIProvider) buildArgs(model, workDir string, cliSessionID uuid.UUID, outputFormat string, hasImages, disableTools bool) []string {
	args := []string{
		"-p",
		"--output-format", outputFormat,
		"--model", model,
		"--permission-mode", p.permMode,
		"--verbose",
	}

	if p.mcpConfigPath != "" {
		args = append(args, "--mcp-config", p.mcpConfigPath)
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
		// Summoner: disable all tools entirely
		args = append(args, "--tools", "")
	} else if p.mcpConfigPath != "" {
		// Chat with MCP bridge: disable CLI built-in tools, only allow MCP bridge tools.
		// This ensures all tool execution goes through GoClaw's controlled MCP bridge.
		args = append(args, "--disallowedTools", "Bash,Edit,Read,Write,Glob,Grep,WebFetch,WebSearch,TodoRead,TodoWrite,NotebookRead,NotebookEdit")
	}

	if p.hooksSettingsPath != "" {
		args = append(args, "--settings", p.hooksSettingsPath)
	}

	return args
}

// ensureWorkDir creates and returns a stable work directory for the given session key.
func (p *ClaudeCLIProvider) ensureWorkDir(sessionKey string) string {
	if sessionKey == "" {
		sessionKey = "default"
	}
	// Sanitize session key for filesystem
	safe := strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(sessionKey)
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
func (p *ClaudeCLIProvider) writeClaudeMD(workDir, systemPrompt string) {
	path := filepath.Join(workDir, "CLAUDE.md")
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

// extractDisableTools checks if disable_tools is set to true in Options.
func extractDisableTools(opts map[string]interface{}) bool {
	if opts == nil {
		return false
	}
	if v, ok := opts[OptDisableTools]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// extractSessionKey gets session_key from Options map.
func extractSessionKey(opts map[string]interface{}) string {
	if opts == nil {
		return ""
	}
	if v, ok := opts[OptSessionKey]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
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
	var contentBlocks []map[string]interface{}

	for _, img := range images {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": img.MimeType,
				"data":       img.Data,
			},
		})
	}

	if text != "" {
		contentBlocks = append(contentBlocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}

	msg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": contentBlocks,
		},
	}

	data, _ := json.Marshal(msg)
	return bytes.NewReader(data)
}

// parseJSONResponse parses the CLI JSON output into a ChatResponse.
func parseJSONResponse(data []byte) (*ChatResponse, error) {
	// Try parsing as JSON array first (CLI may output all events as a single array).
	if resp := parseJSONArray(data); resp != nil {
		return resp, nil
	}

	// Fallback: CLI may output one JSON object per line.
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if resp := parseSingleJSONResult(line); resp != nil {
			return resp, nil
		}
	}

	// Last resort: treat entire output as text response
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("claude-cli: empty response")
	}
	return &ChatResponse{
		Content:      trimmed,
		FinishReason: "stop",
	}, nil
}

// parseJSONArray tries to parse data as a JSON array of CLI events, extracting
// the "result" event's text content and "assistant" event's text blocks.
func parseJSONArray(data []byte) *ChatResponse {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}

	var events []json.RawMessage
	if err := json.Unmarshal(trimmed, &events); err != nil {
		return nil
	}

	var resultText string
	var assistantText strings.Builder
	var usage *Usage
	finishReason := "stop"

	for _, raw := range events {
		var ev struct {
			Type    string          `json:"type"`
			Subtype string          `json:"subtype,omitempty"`
			Result  string          `json:"result,omitempty"`
			Message json.RawMessage `json:"message,omitempty"`
			Usage   *cliUsage       `json:"usage,omitempty"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "result":
			resultText = ev.Result
			if ev.Subtype == "error" {
				finishReason = "error"
			}
			if ev.Usage != nil {
				usage = &Usage{
					PromptTokens:     ev.Usage.InputTokens,
					CompletionTokens: ev.Usage.OutputTokens,
					TotalTokens:      ev.Usage.InputTokens + ev.Usage.OutputTokens,
				}
			}

		case "assistant":
			// Extract text from content blocks
			if ev.Message != nil {
				var msg cliStreamMsg
				if err := json.Unmarshal(ev.Message, &msg); err == nil {
					for _, block := range msg.Content {
						if block.Type == "text" {
							assistantText.WriteString(block.Text)
						}
					}
				}
			}
		}
	}

	// Prefer "result" text, fall back to concatenated assistant text blocks
	content := resultText
	if content == "" {
		content = assistantText.String()
	}
	if content == "" {
		return nil
	}

	resp := &ChatResponse{
		Content:      content,
		FinishReason: finishReason,
		Usage:        usage,
	}
	return resp
}

// parseSingleJSONResult tries to parse a single JSON line as a "result" event.
func parseSingleJSONResult(line []byte) *ChatResponse {
	var resp cliJSONResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil
	}
	if resp.Type != "result" {
		return nil
	}
	cr := &ChatResponse{
		Content:      resp.Result,
		FinishReason: "stop",
	}
	if resp.Subtype == "error" {
		cr.FinishReason = "error"
	}
	if resp.Usage != nil {
		cr.Usage = &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}
	return cr
}

// extractStreamContent extracts text and thinking from a stream message.
func extractStreamContent(msg *cliStreamMsg) (text, thinking string) {
	var textBuf, thinkBuf strings.Builder
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			textBuf.WriteString(block.Text)
		case "thinking":
			thinkBuf.WriteString(block.Thinking)
		}
	}
	return textBuf.String(), thinkBuf.String()
}

// filterCLIEnv removes CLAUDE* env vars to prevent nested session conflicts.
func filterCLIEnv(environ []string) []string {
	var filtered []string
	for _, e := range environ {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		// Filter out variables that could cause nested CLI conflicts
		if strings.HasPrefix(key, "CLAUDE") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

