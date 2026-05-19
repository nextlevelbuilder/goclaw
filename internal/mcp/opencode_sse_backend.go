package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// OpencodeSSEBackend handles async opencode execution via native :4096 API:
// create session → prompt_async → subscribe /global/event SSE → collect final result.
type OpencodeSSEBackend struct {
	httpClient *http.Client
}

func NewOpencodeSSEBackend(client *http.Client) *OpencodeSSEBackend {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpencodeSSEBackend{httpClient: client}
}

// OpencodeSSERequest holds the parameters for an async opencode run.
type OpencodeSSERequest struct {
	OpencodeBase string // :4096 native base URL
	AdapterBase  string // :49999 adapter base URL (for session creation fallback)
	SessionID    string // pre-created session ID (optional)
	Query        string
	Title        string
	Agent        string // defaults to "build"
	Model        string // optional model override, e.g. "gpt-5.4-mini"
	ProviderID   string // optional provider override, e.g. "deepseek"
}

// OpencodeSSEResult holds the final output from the SSE stream.
type OpencodeSSEResult struct {
	SessionID string
	Finish    string // "stop", "error", etc.
	Text      string // final assistant text
	Raw       json.RawMessage
}

// Run executes the async opencode flow: create session → prompt_async → SSE stream → final result.
func (b *OpencodeSSEBackend) Run(ctx context.Context, req OpencodeSSERequest) (OpencodeSSEResult, error) {
	if req.Query == "" {
		return OpencodeSSEResult{}, fmt.Errorf("query is required")
	}

	base := strings.TrimRight(req.OpencodeBase, "/")
	if base == "" {
		return OpencodeSSEResult{}, fmt.Errorf("opencode base URL is required")
	}

	agent := firstNonEmpty(req.Agent, "build")
	directory := "/workspace/repo"

	// Create session if not provided.
	sessionID := req.SessionID
	if sessionID == "" {
		sessURL := base + "/session?directory=%2Fworkspace%2Frepo"
		sessBody := map[string]any{
			"title": firstNonEmpty(req.Title, "opencode-task"),
			"agent": agent,
		}
		raw, err := postJSONWithClient(ctx, b.httpClient, sessURL, sessBody)
		if err != nil {
			return OpencodeSSEResult{}, fmt.Errorf("create session: %w", err)
		}
		var sess struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &sess); err != nil || sess.ID == "" {
			return OpencodeSSEResult{}, fmt.Errorf("create session: bad response: %s", truncate(string(raw), 500))
		}
		sessionID = sess.ID
		slog.Info("mcp.opencode.session_created", "session_id", sessionID)
		emitOpencodeProgressEvent(ctx, 25, "opencode session 已创建。", "session_created", sessionID, map[string]any{
			"session_id": sessionID,
			"agent":      agent,
		})
	}

	// Open SSE stream BEFORE prompt_async to avoid missing early events.
	sseURL := base + "/global/event"

	// Submit async prompt in a goroutine after SSE is established.
	promptErrCh := make(chan error, 1)
	promptSubmitted := make(chan struct{})

	go func() {
		defer close(promptErrCh)
		promptURL := fmt.Sprintf("%s/session/%s/prompt_async?directory=%s", base, sessionID, urlEncodePath(directory))
		promptBody := buildPromptAsyncBody(req.Query, agent, req.ProviderID, req.Model)
		raw, err := postJSONWithClient(ctx, b.httpClient, promptURL, promptBody)
		close(promptSubmitted)
		if err != nil {
			promptErrCh <- fmt.Errorf("prompt_async: %w", err)
			return
		}
		// 204 No Content is expected — no body to parse.
		_ = raw
		slog.Info("mcp.opencode.prompt_async_submitted", "session_id", sessionID)
		emitOpencodeProgressEvent(ctx, 30, "已提交 opencode 异步任务，等待模型和工具执行。", "prompt_submitted", sessionID, map[string]any{
			"session_id":  sessionID,
			"provider_id": req.ProviderID,
			"model":       req.Model,
		})
	}()

	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	resultCh := make(chan OpencodeSSEResult, 1)
	sseErrCh := make(chan error, 1)
	sendResult := func(res OpencodeSSEResult) {
		select {
		case resultCh <- res:
		default:
		}
	}

	go func() {
		err := GetSSEStream(streamCtx, b.httpClient, sseURL, "opencode", func(ctx context.Context, evt sseEvent, sequence int) (bool, error) {
			parsed, err := parseOpencodeEvent(evt)
			if err != nil {
				slog.Warn("mcp.opencode.sse_parse_failed",
					"event", evt.Event,
					"error", err,
					"data", truncate(string(evt.Data), 500),
				)
				return false, nil
			}

			// Filter: only process events for our session.
			if parsed.SessionID != "" && parsed.SessionID != sessionID {
				return false, nil
			}

			emitOpencodeProgress(ctx, parsed, sequence)
			slog.Debug("mcp.opencode.sse_event",
				"event", parsed.EventType,
				"session_id", parsed.SessionID,
				"finish", parsed.Finish,
				"sequence", sequence,
			)

			if isOpencodeTerminalEvent(parsed) {
				sendResult(OpencodeSSEResult{
					SessionID: sessionID,
					Finish:    parsed.Finish,
					Text:      parsed.Text,
					Raw:       evt.Data,
				})
				cancelStream()
				return true, nil
			}
			return false, nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			sseErrCh <- err
			return
		}
		sseErrCh <- nil
	}()

	// Some opencode deployments complete the message but do not reliably emit a
	// terminal /global/event frame through the proxy. Poll /message as a fallback
	// so the MCP tool can still finish once info.finish == "stop" is persisted.
	go func() {
		select {
		case <-promptSubmitted:
		case <-streamCtx.Done():
			return
		}

		msgURL := fmt.Sprintf("%s/session/%s/message?directory=%s", base, sessionID, urlEncodePath(directory))
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var lastStatusSignature string
		var lastStatusEmit time.Time
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ticker.C:
			}

			raw, err := getWithClient(streamCtx, b.httpClient, msgURL)
			if err != nil {
				slog.Debug("mcp.opencode.poll_message_failed", "session_id", sessionID, "error", err)
				continue
			}
			if status, ok := extractOpencodePollStatus(sessionID, raw); ok {
				signature := status.signature()
				if signature != lastStatusSignature || time.Since(lastStatusEmit) >= 8*time.Second {
					lastStatusSignature = signature
					lastStatusEmit = time.Now()
					emitOpencodeProgressEvent(ctx, status.progress(), status.message(), "message_poll", sessionID, status.eventData())
				}
			}
			if res, ok := extractOpencodeTerminalResult(sessionID, raw); ok {
				slog.Info("mcp.opencode.poll_message_terminal",
					"session_id", sessionID,
					"finish", res.Finish,
				)
				emitOpencodeProgressEvent(ctx, 100, "opencode 已产生最终结果。", "message_terminal", sessionID, map[string]any{
					"session_id": sessionID,
					"finish":     res.Finish,
				})
				sendResult(res)
				cancelStream()
				return
			}
		}
	}()

	for {
		select {
		case res := <-resultCh:
			cancelStream()
			select {
			case promptErr := <-promptErrCh:
				if promptErr != nil {
					return OpencodeSSEResult{}, promptErr
				}
			default:
			}
			return res, nil
		case promptErr := <-promptErrCh:
			if promptErr != nil {
				cancelStream()
				return OpencodeSSEResult{}, promptErr
			}
			promptErrCh = nil
		case sseErr := <-sseErrCh:
			if sseErr != nil {
				cancelStream()
				return OpencodeSSEResult{}, fmt.Errorf("SSE stream: %w", sseErr)
			}
			sseErrCh = nil
		case <-ctx.Done():
			cancelStream()
			return OpencodeSSEResult{}, ctx.Err()
		}
	}
}

// opencodeParsedEvent is a structured representation of an opencode SSE event.
type opencodeParsedEvent struct {
	EventType string // SSE event name (e.g. "message.updated")
	SessionID string
	Role      string // "user" or "assistant"
	Finish    string // "stop", "tool-calls", etc.
	Text      string // extracted text from parts
	Parts     []map[string]any
	Raw       map[string]any
}

// parseOpencodeEvent decodes a raw SSE event into a structured opencode event.
func parseOpencodeEvent(evt sseEvent) (opencodeParsedEvent, error) {
	if len(evt.Data) == 0 {
		return opencodeParsedEvent{EventType: evt.Event}, nil
	}

	var obj map[string]any
	if err := json.Unmarshal(evt.Data, &obj); err != nil {
		return opencodeParsedEvent{}, err
	}

	parsed := opencodeParsedEvent{
		EventType: firstNonEmpty(asString(obj["event"]), evt.Event),
		Raw:       obj,
	}

	// Extract common fields from various opencode event shapes.
	// The /global/event SSE may wrap messages differently.
	// We look for sessionID, info.finish, info.role, parts in known locations.

	// Try top-level sessionID.
	parsed.SessionID = asString(obj["sessionID"])

	// Try info sub-object (from message response format).
	if info, ok := obj["info"].(map[string]any); ok {
		if parsed.SessionID == "" {
			parsed.SessionID = asString(info["sessionID"])
		}
		parsed.Role = asString(info["role"])
		parsed.Finish = asString(info["finish"])
	}

	// Try data wrapper.
	if data, ok := obj["data"].(map[string]any); ok {
		if parsed.SessionID == "" {
			parsed.SessionID = asString(data["sessionID"])
		}
		if info, ok := data["info"].(map[string]any); ok {
			if parsed.SessionID == "" {
				parsed.SessionID = asString(info["sessionID"])
			}
			if parsed.Role == "" {
				parsed.Role = asString(info["role"])
			}
			if parsed.Finish == "" {
				parsed.Finish = asString(info["finish"])
			}
		}
		if parsed.Text == "" {
			parsed.Text = extractTextFromParts(data["parts"])
		}
	}

	// Extract text from parts.
	if parsed.Text == "" {
		parsed.Text = extractTextFromParts(obj["parts"])
	}

	// Store parts for detailed inspection.
	if parts, ok := obj["parts"].([]any); ok {
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				parsed.Parts = append(parsed.Parts, pm)
			}
		}
	}

	return parsed, nil
}

// extractTextFromParts pulls text content from opencode message parts.
func extractTextFromParts(parts any) string {
	arr, ok := parts.([]any)
	if !ok {
		return ""
	}
	var texts []string
	for _, p := range arr {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if asString(pm["type"]) == "text" {
			if t := asString(pm["text"]); t != "" {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, "\n")
}

// isOpencodeTerminalEvent returns true when the event indicates the session has finished.
func isOpencodeTerminalEvent(evt opencodeParsedEvent) bool {
	if evt.Role != "assistant" {
		return false
	}
	return evt.Finish == "stop"
}

// emitOpencodeProgress sends progress events to the UI.
func emitOpencodeProgress(ctx context.Context, evt opencodeParsedEvent, sequence int) {
	// Skip non-relevant events.
	if evt.EventType == "" || evt.Role == "user" {
		return
	}

	progress := float64(5 + sequence*3)
	if progress > 95 {
		progress = 95
	}

	emitOpencodeProgressEvent(ctx, progress, opencodeProgressMessage(evt), evt.EventType, evt.SessionID, evt.Raw)
}

func emitOpencodeProgressEvent(ctx context.Context, progress float64, message, event, runID string, eventData map[string]any) {
	cb := tools.ToolProgressCallbackFromCtx(ctx)
	if cb == nil {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	cb(ctx, tools.ProgressEvent{
		ToolName:  "opencode_run",
		Progress:  progress,
		Total:     100,
		Message:   message,
		Event:     event,
		RunID:     runID,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		EventData: eventData,
	})
	slog.Info("mcp.opencode.progress_emit",
		"event", event,
		"run_id", runID,
		"progress", progress,
		"message", message,
	)
}

// opencodeProgressMessage builds a human-readable progress string from an opencode event.
func opencodeProgressMessage(evt opencodeParsedEvent) string {
	if evt.Text != "" {
		maxLen := 200
		if len(evt.Text) > maxLen {
			return evt.Text[:maxLen] + "…"
		}
		return evt.Text
	}

	// Build message from parts.
	for _, p := range evt.Parts {
		ptype := asString(p["type"])
		switch ptype {
		case "tool":
			if name := asString(p["name"]); name != "" {
				return "🔧 " + name
			}
		case "step-start":
			if name := asString(p["name"]); name != "" {
				return "▶ " + name
			}
		case "step-finish":
			if name := asString(p["name"]); name != "" {
				return "✓ " + name
			}
		}
	}

	if evt.EventType != "" {
		return evt.EventType
	}
	return "processing…"
}

// buildPromptAsyncBody constructs the request body for POST /session/{id}/prompt_async.
func buildPromptAsyncBody(query, agent, providerID, model string) map[string]any {
	body := map[string]any{
		"parts": []map[string]any{
			{"type": "text", "text": query},
		},
		"agent": agent,
	}
	if model != "" {
		providerID = firstNonEmpty(providerID, "openai")
		body["model"] = map[string]any{
			"providerID": providerID,
			"modelID":    model,
		}
	}
	return body
}

func extractOpencodeTerminalResult(sessionID string, raw json.RawMessage) (OpencodeSSEResult, bool) {
	var messages []struct {
		Info struct {
			SessionID string `json:"sessionID"`
			Role      string `json:"role"`
			Finish    string `json:"finish"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return OpencodeSSEResult{}, false
	}

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Info.Role != "assistant" || msg.Info.Finish != "stop" {
			continue
		}
		if sessionID != "" && msg.Info.SessionID != "" && msg.Info.SessionID != sessionID {
			continue
		}

		var texts []string
		for _, p := range msg.Parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return OpencodeSSEResult{
			SessionID: firstNonEmpty(msg.Info.SessionID, sessionID),
			Finish:    msg.Info.Finish,
			Text:      strings.Join(texts, "\n"),
			Raw:       raw,
		}, true
	}

	return OpencodeSSEResult{}, false
}

type opencodePollStatus struct {
	SessionID         string
	MessageCount      int
	AssistantMessages int
	LastRole          string
	LastFinish        string
	LastText          string
	LastPartTypes     []string
}

func (s opencodePollStatus) signature() string {
	return fmt.Sprintf("%d:%d:%s:%d", s.MessageCount, s.AssistantMessages, s.LastFinish, len(s.LastText))
}

func (s opencodePollStatus) progress() float64 {
	progress := 30 + float64(s.AssistantMessages*8)
	if progress > 95 {
		return 95
	}
	return progress
}

func (s opencodePollStatus) message() string {
	if s.LastText != "" {
		return "opencode 执行中：" + truncate(s.LastText, 160)
	}
	if s.LastFinish == "tool-calls" {
		return fmt.Sprintf("opencode 正在执行工具步骤（已产生 %d 个 assistant 步骤）。", s.AssistantMessages)
	}
	if s.AssistantMessages > 0 {
		return fmt.Sprintf("opencode 正在思考/执行（已产生 %d 个 assistant 步骤）。", s.AssistantMessages)
	}
	return "opencode 正在等待模型首个输出。"
}

func (s opencodePollStatus) eventData() map[string]any {
	return map[string]any{
		"session_id":         s.SessionID,
		"message_count":      s.MessageCount,
		"assistant_messages": s.AssistantMessages,
		"last_role":          s.LastRole,
		"last_finish":        s.LastFinish,
		"last_part_types":    s.LastPartTypes,
	}
}

func extractOpencodePollStatus(sessionID string, raw json.RawMessage) (opencodePollStatus, bool) {
	var messages []struct {
		Info struct {
			SessionID string `json:"sessionID"`
			Role      string `json:"role"`
			Finish    string `json:"finish"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return opencodePollStatus{}, false
	}

	status := opencodePollStatus{SessionID: sessionID, MessageCount: len(messages)}
	for i := range messages {
		msg := messages[i]
		if sessionID != "" && msg.Info.SessionID != "" && msg.Info.SessionID != sessionID {
			continue
		}
		if msg.Info.Role == "assistant" {
			status.AssistantMessages++
		}
		status.LastRole = msg.Info.Role
		status.LastFinish = msg.Info.Finish
		status.LastPartTypes = status.LastPartTypes[:0]
		var texts []string
		for _, p := range msg.Parts {
			if p.Type != "" {
				status.LastPartTypes = append(status.LastPartTypes, p.Type)
			}
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		status.LastText = strings.Join(texts, "\n")
	}
	return status, true
}

// extractOpencodeFinalText extracts the final assistant text from a /session/{id}/message response.
func extractOpencodeFinalText(raw json.RawMessage) string {
	var messages []struct {
		Info struct {
			Role   string `json:"role"`
			Finish string `json:"finish"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return string(raw)
	}

	// Find the last assistant message with finish=="stop".
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Info.Role == "assistant" && messages[i].Info.Finish == "stop" {
			var texts []string
			for _, p := range messages[i].Parts {
				if p.Type == "text" && p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
			if len(texts) > 0 {
				return strings.Join(texts, "\n")
			}
		}
	}
	return string(raw)
}

// urlEncodePath percent-encodes a path for use in query parameters.
func urlEncodePath(path string) string {
	return strings.ReplaceAll(path, "/", "%2F")
}

// postJSONWithClient is a helper that POSTs JSON using a specific *http.Client.
func postJSONWithClient(ctx context.Context, client *http.Client, url string, body any) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s status %d: %s", redactProxyURL(url), resp.StatusCode, truncate(string(raw), 500))
	}
	return json.RawMessage(raw), nil
}

// getWithClient is a helper that GETs using a specific *http.Client.
func getWithClient(ctx context.Context, client *http.Client, url string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s status %d: %s", redactProxyURL(url), resp.StatusCode, truncate(string(raw), 500))
	}
	return json.RawMessage(raw), nil
}
