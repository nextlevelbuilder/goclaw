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
	if systemPrompt != "" {
		p.writeAgentsMD(workDir, systemPrompt)
	}

	bc := bridgeContextFromOpts(req.Options)
	hasMCP := p.resolveCursorMCPConfig(ctx, workDir, sessionKey, bc)
	chatID := readCursorSessionID(workDir)
	args := p.buildArgs(model, workDir, hasMCP, chatID, "json")
	slog.Debug("cursor-cli exec", "cmd", fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " ")), "workdir", workDir)
	args = append(args, userMsg)

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterCursorEnv(os.Environ())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cursor-cli: %w (stderr: %s)", err, stderr.String())
	}

	resp, err := parseJSONResponse(output)
	if err != nil {
		return nil, err
	}

	// Persist chat ID from JSON response for session continuity
	if newChatID := parseCursorChatIDFromJSON(output); newChatID != "" {
		writeCursorSessionID(workDir, newChatID)
	}

	return resp, nil
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
	slog.Debug("cursor-cli: session lock acquired", "session_key", sessionKey)
	defer func() {
		unlock()
		slog.Debug("cursor-cli: session lock released", "session_key", sessionKey)
	}()

	workDir := p.ensureWorkDir(sessionKey)
	if systemPrompt != "" {
		p.writeAgentsMD(workDir, systemPrompt)
	}

	bc := bridgeContextFromOpts(req.Options)
	hasMCP := p.resolveCursorMCPConfig(ctx, workDir, sessionKey, bc)
	chatID := readCursorSessionID(workDir)
	args := p.buildArgs(model, workDir, hasMCP, chatID, "stream-json")
	fullCmd := fmt.Sprintf("%s %s", p.cliPath, strings.Join(args, " "))
	slog.Debug("cursor-cli stream exec", "cmd", fullCmd, "workdir", workDir)
	args = append(args, userMsg)

	cmd := exec.CommandContext(ctx, p.cliPath, args...)
	cmd.Dir = workDir
	cmd.Env = filterCursorEnv(os.Environ())

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cursor-cli stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cursor-cli start: %w", err)
	}

	// Debug log file: only enabled when GOCLAW_DEBUG=1
	var debugFile *os.File
	if os.Getenv("GOCLAW_DEBUG") == "1" {
		debugLogPath := filepath.Join(workDir, "cursor-cli-debug.log")
		debugFile, _ = os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if debugFile != nil {
			fmt.Fprintf(debugFile, "=== CMD: %s\n=== WORKDIR: %s\n=== TIME: %s\n\n", fullCmd, workDir, time.Now().Format(time.RFC3339))
			defer debugFile.Close()
		}
	}

	// Parse stream-json line-by-line
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, StdioScanBufInit), StdioScanBufMax)

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

		var ev cursorStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			slog.Debug("cursor-cli: skip malformed stream line", "error", err)
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
			// Persist server-assigned chat ID for session continuity
			if newChatID := extractCursorChatID(&ev); newChatID != "" {
				writeCursorSessionID(workDir, newChatID)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cursor-cli: stream read error: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if debugFile != nil {
			fmt.Fprintf(debugFile, "\n=== STDERR:\n%s\n=== EXIT ERROR: %v\n", stderrBuf.String(), err)
		}
		// If we got partial content, return it with the error
		if finalResp.Content != "" {
			return &finalResp, nil
		}
		return nil, fmt.Errorf("cursor-cli: %w (stderr: %s)", err, stderrBuf.String())
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

// parseCursorChatIDFromJSON extracts session_id from a non-streaming JSON response.
func parseCursorChatIDFromJSON(data []byte) string {
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev cursorStreamEvent
		if err := json.Unmarshal(line, &ev); err == nil && ev.Type == "result" && ev.SessionID != "" {
			return ev.SessionID
		}
	}
	return ""
}
