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
)

// Chat runs the Cursor CLI synchronously and returns the final response.
func (p *CursorCLIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	systemPrompt, userMsg, _ := extractFromMessages(req.Messages)
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	unlock := p.lockSession(sessionKey)
	defer unlock()

	workDir := p.ensureWorkDir(sessionKey)

	var chatID string
	if sessionKey != "" {
		var err error
		chatID, err = p.getOrCreateChatID(ctx, sessionKey)
		if err != nil {
			slog.Warn("cursor-cli: failed to get/create chat ID, starting fresh chat", "session_key", sessionKey, "error", err)
			chatID = ""
		}
	}
	args := p.buildArgs(model, workDir, chatID, false)

	// Prepend system prompt to user message if present
	prompt := userMsg
	if systemPrompt != "" {
		prompt = systemPrompt + "\n\n" + userMsg
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterCursorEnv(os.Environ())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	slog.Debug("cursor-cli exec", "cmd", fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " ")), "workdir", workDir)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cursor-cli: %w (stderr: %s)", err, stderr.String())
	}

	return p.parseJSONResponse(output)
}

// ChatStream runs the Cursor CLI with stream-json output, calling onChunk for each text delta.
func (p *CursorCLIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	systemPrompt, userMsg, _ := extractFromMessages(req.Messages)
	sessionKey := extractStringOpt(req.Options, OptSessionKey)
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	slog.Debug("cursor-cli: acquiring session lock", "session_key", sessionKey)
	unlock := p.lockSession(sessionKey)
	defer func() {
		unlock()
		slog.Debug("cursor-cli: session lock released", "session_key", sessionKey)
	}()

	workDir := p.ensureWorkDir(sessionKey)

	var chatID string
	if sessionKey != "" {
		var err error
		chatID, err = p.getOrCreateChatID(ctx, sessionKey)
		if err != nil {
			slog.Warn("cursor-cli: failed to get/create chat ID, starting fresh chat", "session_key", sessionKey, "error", err)
			chatID = ""
		}
	}
	args := p.buildArgs(model, workDir, chatID, true)

	prompt := userMsg
	if systemPrompt != "" {
		prompt = systemPrompt + "\n\n" + userMsg
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = workDir
	cmd.Env = filterCursorEnv(os.Environ())

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cursor-cli stdout pipe: %w", err)
	}

	fullCmd := fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " "))
	slog.Debug("cursor-cli stream exec", "cmd", fullCmd, "workdir", workDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cursor-cli start: %w", err)
	}

	var debugFile *os.File
	if os.Getenv("GOCLAW_DEBUG") == "1" {
		debugLogPath := filepath.Join(workDir, "cursor-debug.log")
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
		if line[0] != '{' {
			continue
		}

		var ev cursorStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("cursor-cli: skip malformed stream line", "error", err)
			continue
		}

		switch ev.Type {
		case "assistant":
			text := extractCursorText(ev.Message)
			if text != "" {
				contentBuf.WriteString(text)
				onChunk(StreamChunk{Content: text})
			}

		case "result":
			finalResp.Content = contentBuf.String()
			finalResp.FinishReason = "stop"
			if ev.IsError {
				finalResp.FinishReason = "error"
			}
			if ev.Usage != nil {
				finalResp.Usage = &Usage{
					PromptTokens:     ev.Usage.InputTokens,
					CompletionTokens: ev.Usage.OutputTokens,
					TotalTokens:      ev.Usage.InputTokens + ev.Usage.OutputTokens,
				}
			}

		case "tool_call":
			// Cursor tool calls are emitted but not executed by GoClaw — the CLI handles them internally.
			// We emit them as content placeholders so the stream doesn't stall.
			if ev.Subtype == "started" {
				text := "[Using tool]"
				contentBuf.WriteString(text)
				onChunk(StreamChunk{Content: text})
			}
		}
	}

	if ctx.Err() != nil {
		_ = cmd.Wait()
		return nil, ctx.Err()
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cursor-cli: stream read error: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if debugFile != nil {
			fmt.Fprintf(debugFile, "\n=== STDERR:\n%s\n=== EXIT ERROR: %v\n", stderrBuf.String(), err)
		}
		if finalResp.Content != "" {
			return &finalResp, nil
		}
		return nil, fmt.Errorf("cursor-cli: %w (stderr: %s)", err, stderrBuf.String())
	}
	if debugFile != nil && stderrBuf.Len() > 0 {
		fmt.Fprintf(debugFile, "\n=== STDERR:\n%s\n", stderrBuf.String())
	}

	if finalResp.Content == "" {
		finalResp.Content = contentBuf.String()
		finalResp.FinishReason = "stop"
	}

	onChunk(StreamChunk{Done: true})
	return &finalResp, nil
}

func (p *CursorCLIProvider) parseJSONResponse(output []byte) (*ChatResponse, error) {
	var final ChatResponse
	var contentBuf strings.Builder

	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev cursorStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			text := extractCursorText(ev.Message)
			if text != "" {
				contentBuf.WriteString(text)
			}
		case "result":
			final.Content = contentBuf.String()
			final.FinishReason = "stop"
			if ev.IsError {
				final.FinishReason = "error"
			}
			if ev.Usage != nil {
				final.Usage = &Usage{
					PromptTokens:     ev.Usage.InputTokens,
					CompletionTokens: ev.Usage.OutputTokens,
					TotalTokens:      ev.Usage.InputTokens + ev.Usage.OutputTokens,
				}
			}
		}
	}

	if final.Content == "" {
		final.Content = contentBuf.String()
		final.FinishReason = "stop"
	}
	return &final, nil
}

func extractCursorText(msg *cursorStreamMsg) string {
	if msg == nil {
		return ""
	}
	var parts []string
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "")
}

// filterCursorEnv removes CURSOR* env vars to prevent nested session conflicts.
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
