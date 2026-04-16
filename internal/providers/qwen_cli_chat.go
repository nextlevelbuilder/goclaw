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
	"time"

	"github.com/google/uuid"
)

// Chat runs the CLI synchronously and returns the final response.
func (p *QwenCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	systemPrompt, userMsg, _ := extractFromMessages(req.Messages)
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	unlock := p.lockSession(sessionKey)
	defer unlock()

	workDir := p.ensureWorkDir(sessionKey)
	if systemPrompt != "" {
		p.writeQwenMD(workDir, systemPrompt)
	}

	cliSessionID := deriveSessionUUID(sessionKey)
	args := p.buildArgs(model, workDir, cliSessionID, "json")
	// Use -p flag for non-interactive mode with explicit prompt
	if userMsg != "" {
		args = append(args, "-p", userMsg)
	}

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterQwenCLIEnv(os.Environ())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	slog.Debug("qwen-cli exec", "cmd", fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " ")), "workdir", workDir)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("qwen-cli: %w (stderr: %s)", err, stderr.String())
	}

	return parseQwenJSONResponse(output)
}

// ChatStream runs the CLI with stream-json output, calling onChunk for each text delta.
func (p *QwenCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	systemPrompt, userMsg, _ := extractFromMessages(req.Messages)
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	slog.Debug("qwen-cli: acquiring session lock", "session_key", sessionKey)
	unlock := p.lockSession(sessionKey)
	slog.Debug("qwen-cli: session lock acquired", "session_key", sessionKey)
	defer func() {
		unlock()
		slog.Debug("qwen-cli: session lock released", "session_key", sessionKey)
	}()

	workDir := p.ensureWorkDir(sessionKey)
	if systemPrompt != "" {
		p.writeQwenMD(workDir, systemPrompt)
	}

	cliSessionID := deriveSessionUUID(sessionKey)
	args := p.buildArgs(model, workDir, cliSessionID, "stream-json")
	// Use -p flag for non-interactive mode with explicit prompt
	if userMsg != "" {
		args = append(args, "-p", userMsg)
	}

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workDir
	cmd.Env = filterQwenCLIEnv(os.Environ())

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("qwen-cli stdout pipe: %w", err)
	}

	fullCmd := fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " "))
	slog.Debug("qwen-cli stream exec", "cmd", fullCmd, "workdir", workDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("qwen-cli start: %w", err)
	}

	// Debug log file: only enabled when GOCLAW_DEBUG=1
	var debugFile *os.File
	if os.Getenv("GOCLAW_DEBUG") == "1" {
		debugLogPath := filepath.Join(workDir, "qwen-cli-debug.log")
		debugFile, _ = os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if debugFile != nil {
			fmt.Fprintf(debugFile, "=== CMD: %s\n=== WORKDIR: %s\n=== TIME: %s\n\n", fullCmd, workDir, time.Now().Format(time.RFC3339))
			defer debugFile.Close()
		}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, StdioScanBufInit), StdioScanBufMax)

	var finalResp ChatResponse
	var contentBuf strings.Builder

	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if debugFile != nil {
			fmt.Fprintf(debugFile, "%s\n", line)
		}

		var ev qwenStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("qwen-cli: skip malformed stream line", "error", err)
			continue
		}

		switch ev.Type {
		case "assistant":
			if ev.Message == nil {
				continue
			}
			text, thinking := extractQwenStreamContent(ev.Message)
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
					TotalTokens:      ev.Usage.TotalTokens,
					CacheReadTokens:  ev.Usage.CacheReadInputTokens,
				}
			}
		}
	}

	if ctx.Err() != nil {
		// Kill the process to release Qwen CLI's internal session lock faster
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		// Small delay to ensure Qwen CLI releases its session file lock
		time.Sleep(100 * time.Millisecond)
		return nil, ctx.Err()
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("qwen-cli: stream read error: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if debugFile != nil {
			fmt.Fprintf(debugFile, "\n=== STDERR:\n%s\n=== EXIT ERROR: %v\n", stderrBuf.String(), err)
		}
		if finalResp.Content != "" {
			// Small delay to ensure Qwen CLI releases its session file lock
			time.Sleep(100 * time.Millisecond)
			return &finalResp, nil
		}
		return nil, fmt.Errorf("qwen-cli: %w (stderr: %s)", err, stderrBuf.String())
	}

	// Small delay to ensure Qwen CLI releases its session file lock before next request
	time.Sleep(100 * time.Millisecond)

	if finalResp.Content == "" {
		finalResp.Content = contentBuf.String()
		finalResp.FinishReason = "stop"
	}

	onChunk(StreamChunk{Done: true})
	return &finalResp, nil
}

// buildArgs constructs CLI arguments for qwen.
func (p *QwenCLIProvider) buildArgs(model, workDir string, cliSessionID uuid.UUID, outputFormat string) []string {
	args := []string{
		"-o", outputFormat,
		"--approval-mode", p.approvalMode,
	}

	if model != "" && model != "coder-model" {
		args = append(args, "-m", model)
	}

	sid := cliSessionID.String()
	if qwenSessionFileExists(workDir, cliSessionID) {
		args = append(args, "-r", sid)
	} else {
		args = append(args, "--session-id", sid)
	}

	return args
}

// qwenSessionFileExists checks if a Qwen CLI session file exists.
// Qwen CLI stores sessions at: ~/.qwen/projects/<sanitizedCwd>/chats/<sessionId>.jsonl
// where sanitizedCwd replaces all non-alphanumeric characters with hyphens.
func qwenSessionFileExists(workDir string, sessionID uuid.UUID) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		resolved = workDir
	}
	// Qwen CLI uses sanitizeCwd which replaces ALL non-alphanumeric chars with "-"
	encoded := sanitizeQwenCwd(resolved)
	sessionFile := filepath.Join(home, ".qwen", "projects", encoded, "chats", sessionID.String()+".jsonl")
	_, err = os.Stat(sessionFile)
	return err == nil
}

// sanitizeQwenCwd replicates Qwen CLI's sanitizeCwd function:
// replaces all non-alphanumeric characters with hyphens.
func sanitizeQwenCwd(cwd string) string {
	var result strings.Builder
	for _, r := range cwd {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
		} else {
			result.WriteRune('-')
		}
	}
	return result.String()
}

// filterQwenCLIEnv removes QWEN* env vars to prevent nested session conflicts.
func filterQwenCLIEnv(environ []string) []string {
	var filtered []string
	for _, e := range environ {
		key := e
		if before, _, ok := strings.Cut(e, "="); ok {
			key = before
		}
		if strings.HasPrefix(key, "QWEN") {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}
