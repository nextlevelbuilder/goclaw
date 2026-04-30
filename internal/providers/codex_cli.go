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
)

// CodexCLIProvider implements Provider by shelling out to `codex exec`.
type CodexCLIProvider struct {
	name         string
	cliPath      string
	defaultModel string
	baseWorkDir  string
	mu           sync.Mutex
	sessionMu    sync.Map
}

type CodexCLIOption func(*CodexCLIProvider)

func WithCodexCLIName(name string) CodexCLIOption {
	return func(p *CodexCLIProvider) {
		if name != "" {
			p.name = name
		}
	}
}

func WithCodexCLIModel(model string) CodexCLIOption {
	return func(p *CodexCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

func WithCodexCLIWorkDir(dir string) CodexCLIOption {
	return func(p *CodexCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

func NewCodexCLIProvider(cliPath string, opts ...CodexCLIOption) *CodexCLIProvider {
	if cliPath == "" {
		cliPath = "codex"
	}
	p := &CodexCLIProvider{
		name:         "codex-cli",
		cliPath:      cliPath,
		defaultModel: "gpt-5.4",
		baseWorkDir:  defaultCLIWorkDir(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *CodexCLIProvider) Name() string         { return p.name }
func (p *CodexCLIProvider) DefaultModel() string { return p.defaultModel }

func (p *CodexCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: 200_000,
		TokenizerID:      "cl100k_base",
	}
}

func (p *CodexCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	var final ChatResponse
	resp, err := p.run(ctx, req, func(chunk StreamChunk) {
		if chunk.Content != "" {
			final.Content += chunk.Content
		}
	})
	if err != nil {
		return nil, err
	}
	if resp.Content == "" {
		resp.Content = final.Content
	}
	return resp, nil
}

func (p *CodexCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.run(ctx, req, onChunk)
}

func (p *CodexCLIProvider) run(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	if len(images) > 0 {
		return nil, fmt.Errorf("codex-cli: image input is not supported")
	}
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	unlock := p.lockSession(sessionKey)
	defer unlock()

	workDir := p.ensureWorkDir(sessionKey)
	if systemPrompt != "" {
		p.writeAgentsMD(workDir, systemPrompt)
	}

	outputPath := filepath.Join(workDir, ".codex-last-message.txt")
	_ = os.Remove(outputPath)

	args := p.buildArgs(model, workDir, outputPath, extractStringOpt(req.Options, OptThinkingLevel))
	cmdName, cmdArgs := p.command(args)
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workDir
	cmd.Env = filterCLIEnv(os.Environ())
	cmd.Stdin = strings.NewReader(userMsg)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex-cli stdout pipe: %w", err)
	}

	slog.Debug("codex-cli exec", "cmd", fmt.Sprintf("%s %s", cmdName, strings.Join(cmdArgs, " ")), "workdir", workDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex-cli start: %w", err)
	}

	var contentBuf strings.Builder
	var streamErr string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, StdioScanBufInit), StdioScanBufMax)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		text, thinking, errMsg, failed := parseCodexCLIEvent(line)
		if text != "" {
			contentBuf.WriteString(text)
			onChunk(StreamChunk{Content: text})
		}
		if thinking != "" {
			onChunk(StreamChunk{Thinking: thinking})
		}
		if errMsg != "" {
			streamErr = errMsg
		}
		if failed && streamErr == "" {
			streamErr = string(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("codex-cli: stream read error: %w", err)
	}

	waitErr := cmd.Wait()
	finalText := strings.TrimSpace(readOptionalFile(outputPath))
	if finalText == "" {
		finalText = strings.TrimSpace(contentBuf.String())
	}
	if waitErr != nil {
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if streamErr != "" {
			stderrStr = strings.TrimSpace(stderrStr + "\n" + streamErr)
		}
		if finalText != "" {
			return &ChatResponse{Content: finalText, FinishReason: "stop"}, nil
		}
		return nil, fmt.Errorf("codex-cli: %w (stderr: %s)", waitErr, stderrStr)
	}
	if finalText == "" {
		return nil, fmt.Errorf("codex-cli: empty response")
	}
	onChunk(StreamChunk{Done: true})
	return &ChatResponse{Content: finalText, FinishReason: "stop"}, nil
}

func (p *CodexCLIProvider) buildArgs(model, workDir, outputPath, effort string) []string {
	args := []string{
		"exec",
		"--json",
		"--model", model,
		"--sandbox", "workspace-write",
		"--skip-git-repo-check",
		"--cd", workDir,
		"--output-last-message", outputPath,
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort != "" && effort != "off" && isAlphaOnly(effort) {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=\"%s\"", effort))
	}
	args = append(args, "-")
	return args
}

func (p *CodexCLIProvider) command(args []string) (string, []string) {
	if (p.cliPath == "" || p.cliPath == "codex") && fileExists("/usr/local/lib/node_modules/@openai/codex/bin/codex.js") {
		return "node", append([]string{"/usr/local/lib/node_modules/@openai/codex/bin/codex.js"}, args...)
	}
	return p.cliPath, args
}

func (p *CodexCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func (p *CodexCLIProvider) ensureWorkDir(sessionKey string) string {
	safe := sanitizePathSegment(sessionKey)
	if safe == "" {
		safe = "default"
	}
	dir := filepath.Join(p.baseWorkDir, safe)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("codex-cli: failed to create workdir", "dir", dir, "error", err)
		return os.TempDir()
	}
	return dir
}

func (p *CodexCLIProvider) writeAgentsMD(workDir, systemPrompt string) {
	path := filepath.Join(workDir, "AGENTS.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("codex-cli: failed to write AGENTS.md", "path", path, "error", err)
	}
}

func parseCodexCLIEvent(line []byte) (text, thinking, errMsg string, failed bool) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return "", "", "", false
	}
	typ, _ := ev["type"].(string)
	if typ == "error" {
		return "", "", stringValue(ev["message"]), false
	}
	if strings.Contains(typ, "failed") {
		return "", "", nestedString(ev, "error", "message"), true
	}
	if strings.Contains(typ, "reasoning") || strings.Contains(typ, "thinking") {
		return "", firstString(ev, "delta", "text", "message"), "", false
	}
	if strings.Contains(typ, "delta") || strings.Contains(typ, "message") {
		return firstString(ev, "delta", "text", "message"), "", "", false
	}
	return "", "", "", false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := stringValue(m[key]); s != "" {
			return s
		}
	}
	return ""
}

func nestedString(m map[string]any, outer, inner string) string {
	child, _ := m[outer].(map[string]any)
	if child == nil {
		return ""
	}
	return stringValue(child[inner])
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func readOptionalFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
