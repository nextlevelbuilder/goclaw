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

// GeminiCLIProvider implements Provider by shelling out to `gemini`.
type GeminiCLIProvider struct {
	name         string
	cliPath      string
	defaultModel string
	baseWorkDir  string
	mu           sync.Mutex
	sessionMu    sync.Map
	started      sync.Map
}

type GeminiCLIOption func(*GeminiCLIProvider)

func WithGeminiCLIName(name string) GeminiCLIOption {
	return func(p *GeminiCLIProvider) {
		if name != "" {
			p.name = name
		}
	}
}

func WithGeminiCLIModel(model string) GeminiCLIOption {
	return func(p *GeminiCLIProvider) {
		if model != "" {
			p.defaultModel = model
		}
	}
}

func WithGeminiCLIWorkDir(dir string) GeminiCLIOption {
	return func(p *GeminiCLIProvider) {
		if dir != "" {
			p.baseWorkDir = dir
		}
	}
}

func NewGeminiCLIProvider(cliPath string, opts ...GeminiCLIOption) *GeminiCLIProvider {
	if cliPath == "" {
		cliPath = "gemini"
	}
	p := &GeminiCLIProvider{
		name:         "gemini-cli",
		cliPath:      cliPath,
		defaultModel: "auto",
		baseWorkDir:  defaultCLIWorkDir(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *GeminiCLIProvider) Name() string         { return p.name }
func (p *GeminiCLIProvider) DefaultModel() string { return p.defaultModel }

func (p *GeminiCLIProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         false,
		Vision:           false,
		CacheControl:     false,
		MaxContextWindow: 1_000_000,
		TokenizerID:      "cl100k_base",
	}
}

func (p *GeminiCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
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

func (p *GeminiCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.run(ctx, req, onChunk)
}

func (p *GeminiCLIProvider) run(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	systemPrompt, userMsg, images := extractFromMessages(req.Messages)
	if len(images) > 0 {
		return nil, fmt.Errorf("gemini-cli: image input is not supported")
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
		p.writeGeminiMD(workDir, systemPrompt)
	}

	args := p.buildArgs(sessionKey, model, userMsg)
	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workDir
	cmd.Env = filterCLIEnv(os.Environ())

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("gemini-cli stdout pipe: %w", err)
	}

	slog.Debug("gemini-cli exec", "cmd", fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " ")), "workdir", workDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("gemini-cli start: %w", err)
	}

	var contentBuf strings.Builder
	finalText := ""
	streamErr := ""
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, StdioScanBufInit), StdioScanBufMax)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		chunk, resultText, errText, done := parseGeminiCLIEvent(line)
		if chunk != "" {
			contentBuf.WriteString(chunk)
			onChunk(StreamChunk{Content: chunk})
		}
		if resultText != "" {
			finalText = resultText
		}
		if errText != "" {
			streamErr = errText
		}
		if done {
			p.started.Store(sessionKey, true)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("gemini-cli: stream read error: %w", err)
	}

	waitErr := cmd.Wait()
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
		return nil, fmt.Errorf("gemini-cli: %w (stderr: %s)", waitErr, stderrStr)
	}
	if finalText == "" {
		return nil, fmt.Errorf("gemini-cli: empty response")
	}
	onChunk(StreamChunk{Done: true})
	return &ChatResponse{Content: finalText, FinishReason: "stop"}, nil
}

func (p *GeminiCLIProvider) buildArgs(sessionKey, model, prompt string) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--model", model,
		"--approval-mode", "yolo",
		"--skip-trust",
	}
	if started, _ := p.started.Load(sessionKey); started == true {
		args = append(args, "--resume", "latest")
	}
	return args
}

func (p *GeminiCLIProvider) lockSession(sessionKey string) func() {
	actual, _ := p.sessionMu.LoadOrStore(sessionKey, &sync.Mutex{})
	m := actual.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func (p *GeminiCLIProvider) ensureWorkDir(sessionKey string) string {
	safe := sanitizePathSegment(sessionKey)
	if safe == "" {
		safe = "default"
	}
	dir := filepath.Join(p.baseWorkDir, safe)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Warn("gemini-cli: failed to create workdir", "dir", dir, "error", err)
		return os.TempDir()
	}
	return dir
}

func (p *GeminiCLIProvider) writeGeminiMD(workDir, systemPrompt string) {
	path := filepath.Join(workDir, "GEMINI.md")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == systemPrompt {
		return
	}
	if err := os.WriteFile(path, []byte(systemPrompt), 0600); err != nil {
		slog.Warn("gemini-cli: failed to write GEMINI.md", "path", path, "error", err)
	}
}

func parseGeminiCLIEvent(line []byte) (chunk, resultText, errText string, done bool) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return "", "", "", false
	}
	typ, _ := ev["type"].(string)
	switch typ {
	case "message":
		role, _ := ev["role"].(string)
		if role != "assistant" {
			return "", "", "", false
		}
		return extractGeminiText(ev["content"]), "", "", false
	case "result":
		if resp, _ := ev["response"].(string); strings.TrimSpace(resp) != "" {
			return "", strings.TrimSpace(resp), "", true
		}
		if errMap, ok := ev["error"].(map[string]any); ok {
			return "", "", strings.TrimSpace(extractGeminiText(errMap)), true
		}
		return "", "", "", true
	case "error":
		if msg, _ := ev["message"].(string); msg != "" {
			return "", "", strings.TrimSpace(msg), false
		}
		return "", "", strings.TrimSpace(extractGeminiText(ev)), false
	default:
		return "", "", "", false
	}
}

func extractGeminiText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, item := range x {
			if text := extractGeminiText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if text, _ := x["text"].(string); text != "" {
			return text
		}
		if content, ok := x["content"]; ok {
			return extractGeminiText(content)
		}
		if parts, ok := x["parts"]; ok {
			return extractGeminiText(parts)
		}
	}
	return ""
}
