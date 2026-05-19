package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

var (
	formFillSensitiveQueryParamRE = regexp.MustCompile(`(?i)([?&])([^&\s"'<>]*?(?:api[_-]?key|token|secret|password|bearer|authorization)[^&\s"'<>]*?)=[^&\s"'<>]*`)
	formFillSafeLogNameRE         = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

type FormFillRunRequest struct {
	HTTPBase string
	Query    string
}

type FormFillInputRequest struct {
	HTTPBase   string
	RunID      string
	QuestionID string
	InputURL   string
	Content    map[string]any
}

type FormFillSSEFinalEvent struct {
	Event     string
	RunID     string
	Timestamp string
	Data      map[string]any
	Raw       json.RawMessage
}

type FormFillSSEBackend struct {
	httpClient *http.Client
}

func NewFormFillSSEBackend(client *http.Client) *FormFillSSEBackend {
	if client == nil {
		client = http.DefaultClient
	}
	return &FormFillSSEBackend{httpClient: client}
}

func (b *FormFillSSEBackend) Run(ctx context.Context, req FormFillRunRequest) (FormFillSSEFinalEvent, error) {
	if strings.TrimSpace(req.HTTPBase) == "" {
		return FormFillSSEFinalEvent{}, fmt.Errorf("HTTPBase is required")
	}
	if strings.TrimSpace(req.Query) == "" {
		return FormFillSSEFinalEvent{}, fmt.Errorf("query is required")
	}
	url := strings.TrimRight(req.HTTPBase, "/") + "/v1/feishu/form-fill/run"
	return b.postSSE(ctx, url, map[string]any{"query": req.Query})
}

func (b *FormFillSSEBackend) Input(ctx context.Context, req FormFillInputRequest) (FormFillSSEFinalEvent, error) {
	if strings.TrimSpace(req.HTTPBase) == "" {
		return FormFillSSEFinalEvent{}, fmt.Errorf("HTTPBase is required")
	}
	if strings.TrimSpace(req.RunID) == "" {
		return FormFillSSEFinalEvent{}, fmt.Errorf("run_id is required")
	}
	if strings.TrimSpace(req.QuestionID) == "" {
		return FormFillSSEFinalEvent{}, fmt.Errorf("question_id is required")
	}
	if len(req.Content) == 0 {
		return FormFillSSEFinalEvent{}, fmt.Errorf("content is required")
	}

	url := strings.TrimSpace(req.InputURL)
	if url == "" {
		url = "/v1/feishu/form-fill/runs/" + req.RunID + "/input"
	}
	if strings.HasPrefix(url, "/") {
		url = strings.TrimRight(req.HTTPBase, "/") + url
	}

	final, err := b.postSSE(ctx, url, map[string]any{
		"question_id": req.QuestionID,
		"content":     req.Content,
	})
	if final.RunID == "" {
		final.RunID = req.RunID
	}
	return final, err
}

func (b *FormFillSSEBackend) postSSE(ctx context.Context, url string, body any) (FormFillSSEFinalEvent, error) {
	var final FormFillSSEFinalEvent
	err := PostSSEStream(ctx, b.httpClient, url, body, "form_fill", func(ctx context.Context, evt sseEvent, sequence int) (bool, error) {
		parsed, err := parseFormFillSSEEvent(evt)
		if err != nil {
			slog.Warn("mcp.form_fill.sse_parse_failed",
				"event", evt.Event,
				"error", err,
				"data", truncate(string(evt.Data), 500),
			)
			return false, nil
		}

		rawRef := appendFormFillRawEvent(parsed, sequence, evt.Data)
		parsed = attachFormFillRawEventRef(parsed, rawRef)
		emitFormFillProgress(ctx, parsed, sequence)
		slog.Info("mcp.form_fill.sse_event", "event", parsed.Event, "run_id", parsed.RunID)
		if isFormFillTerminalEvent(parsed.Event) {
			final = parsed
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return FormFillSSEFinalEvent{}, err
	}
	if len(final.Raw) == 0 {
		return FormFillSSEFinalEvent{}, fmt.Errorf("SSE stream ended without ask_user_question/run_completed/run_failed/run_cancelled")
	}
	return final, nil
}

func parseFormFillSSEEvent(evt sseEvent) (FormFillSSEFinalEvent, error) {
	var obj map[string]any
	if err := json.Unmarshal(evt.Data, &obj); err != nil {
		return FormFillSSEFinalEvent{}, err
	}

	eventName := firstNonEmpty(asString(obj["event"]), evt.Event)
	if eventName == "" {
		eventName = "message"
	}
	runID := asString(obj["run_id"])
	timestamp := asString(obj["timestamp"])
	data, _ := obj["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	data = normalizeFormFillEventData(eventName, data)

	obj["event"] = eventName
	if runID != "" {
		obj["run_id"] = runID
	}
	obj["data"] = data
	sanitizedObj, _ := sanitizeFormFillEventValue(obj).(map[string]any)
	if sanitizedObj == nil {
		sanitizedObj = obj
	}
	if sanitizedData, _ := sanitizedObj["data"].(map[string]any); sanitizedData != nil {
		data = sanitizedData
	}
	raw, err := json.Marshal(sanitizedObj)
	if err != nil {
		return FormFillSSEFinalEvent{}, err
	}
	return FormFillSSEFinalEvent{
		Event:     eventName,
		RunID:     runID,
		Timestamp: timestamp,
		Data:      data,
		Raw:       raw,
	}, nil
}

func isFormFillTerminalEvent(event string) bool {
	switch event {
	case "ask_user_question", "run_completed", "run_failed", "run_cancelled":
		return true
	default:
		return false
	}
}

func emitFormFillProgress(ctx context.Context, event FormFillSSEFinalEvent, sequence int) {
	cb := tools.ToolProgressCallbackFromCtx(ctx)
	if cb == nil || event.Event == "heartbeat" {
		return
	}

	progress := float64(5 + sequence*5)
	if progress > 95 {
		progress = 95
	}
	cb(ctx, tools.ProgressEvent{
		ToolName:  "feishu_form_fill",
		Progress:  progress,
		Total:     100,
		Message:   formFillProgressMessage(event),
		Event:     event.Event,
		RunID:     event.RunID,
		Timestamp: event.Timestamp,
		EventData: formFillEventEnvelope(event),
	})
}

func formFillEventEnvelope(event FormFillSSEFinalEvent) map[string]any {
	envelope := map[string]any{
		"event": event.Event,
		"data":  cloneMap(event.Data),
	}
	if event.RunID != "" {
		envelope["run_id"] = event.RunID
	}
	if event.Timestamp != "" {
		envelope["timestamp"] = event.Timestamp
	}
	return envelope
}

func normalizeFormFillEventData(eventName string, data map[string]any) map[string]any {
	normalized := cloneMap(data)
	if normalized == nil {
		normalized = map[string]any{}
	}
	if strings.TrimSpace(asString(normalized["status"])) == "" || formFillEventBusinessFailed(eventName, normalized) {
		normalized["status"] = formFillEventStatus(eventName, normalized)
	}

	payload, ok := normalized["payload"].(map[string]any)
	if !ok || payload == nil {
		payload = formFillEventPayload(eventName, normalized)
		normalized["payload"] = payload
	}
	ensureFormFillPayloadDefaults(payload, eventName, normalized)
	return normalized
}

func ensureFormFillPayloadDefaults(payload map[string]any, eventName string, data map[string]any) {
	if strings.TrimSpace(asString(payload["version"])) == "" {
		payload["version"] = "goclaw.gateway.reply.v1"
	}
	if strings.TrimSpace(asString(payload["kind"])) == "" || formFillEventBusinessFailed(eventName, data) {
		payload["kind"] = formFillPayloadKind(eventName, data)
	}
	if _, ok := payload["summary"]; !ok {
		payload["summary"] = []any{}
	}
	if _, ok := payload["questions"]; !ok {
		payload["questions"] = []any{}
	}
	if _, ok := payload["fields"]; !ok {
		payload["fields"] = []any{}
	}
}

func formFillEventPayload(eventName string, data map[string]any) map[string]any {
	title, text := formFillPayloadTitleText(eventName, data)
	payload := map[string]any{
		"version":   "goclaw.gateway.reply.v1",
		"kind":      formFillPayloadKind(eventName, data),
		"title":     title,
		"text":      text,
		"summary":   formFillPayloadSummary(eventName, data),
		"questions": []any{},
		"fields":    []any{},
	}
	return payload
}

func formFillPayloadKind(eventName string, data map[string]any) string {
	switch eventName {
	case "ask_user_question":
		return "ask_user"
	case "run_completed":
		if formFillEventBusinessFailed(eventName, data) {
			return "error"
		}
		return "result"
	case "run_cancelled":
		return "result"
	case "run_failed":
		return "error"
	default:
		return "progress"
	}
}

func formFillEventStatus(eventName string, data map[string]any) string {
	switch eventName {
	case "ask_user_question":
		return "awaiting_user"
	case "run_completed":
		if formFillEventBusinessFailed(eventName, data) {
			return "failed"
		}
		return "completed"
	case "run_failed":
		return "failed"
	case "run_cancelled":
		return "cancelled"
	default:
		return "running"
	}
}

func formFillPayloadTitleText(eventName string, data map[string]any) (string, string) {
	switch eventName {
	case "run_started":
		return "表单填写任务", "表单任务已创建。"
	case "phase_started":
		if phase := strings.TrimSpace(asString(data["phase"])); phase != "" {
			return "正在处理表单", "进入 " + phase + " 阶段。"
		}
		return "正在处理表单", "进入表单处理阶段。"
	case "phase_completed":
		if phase := strings.TrimSpace(asString(data["phase"])); phase != "" {
			return "表单处理进度", phase + " 阶段已完成。"
		}
		return "表单处理进度", "阶段已完成。"
	case "user_response_received":
		return "已收到用户回复", "正在根据回复继续处理。"
	case "step_start":
		return "正在填写表单", "开始执行浏览器步骤。"
	case "step_end":
		return "正在填写表单", formFillStepEndText(data)
	case "ask_user_question":
		return "需要确认", firstNonEmpty(asString(data["text"]), "请确认信息是否正确。")
	case "run_completed":
		if formFillEventBusinessFailed(eventName, data) {
			return "表单提交失败", firstNonEmpty(formFillFirstError(data), "表单没有提交成功。")
		}
		return "表单提交结果", firstNonEmpty(asString(data["final_text"]), asString(data["submission_result"]), "表单提交完成。")
	case "run_failed":
		return "表单处理失败", firstNonEmpty(asString(data["error"]), formFillFirstError(data), "表单处理失败。")
	case "run_cancelled":
		return "表单处理已取消", "本次表单处理已取消。"
	default:
		return "表单处理进度", firstNonEmpty(asString(data["message"]), "收到事件："+eventName)
	}
}

func formFillStepEndText(data map[string]any) string {
	action := strings.TrimSpace(asString(data["last_action"]))
	excerpt := strings.TrimSpace(asString(data["last_excerpt"]))
	if action == "input" {
		if typed := extractSingleQuotedValue(excerpt, "Typed "); typed != "" {
			return "已输入：" + typed
		}
		return "已填写表单内容。"
	}
	if action == "click" {
		if strings.Contains(strings.ToLower(excerpt), "submit") {
			return "已点击提交按钮。"
		}
		return "已点击页面按钮。"
	}
	if action == "wait" {
		return "等待页面响应。"
	}
	if strings.Contains(action, "guard_status") {
		return "正在校验提交状态。"
	}
	if action != "" {
		return "浏览器动作：" + action
	}
	return "完成一个浏览器步骤。"
}

func formFillPayloadSummary(eventName string, data map[string]any) []any {
	summary := make([]any, 0, 4)
	addSummary := func(id, label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		summary = append(summary, map[string]any{"id": id, "label": label, "value": value})
	}
	addSummary("event", "事件", eventName)
	addSummary("phase", "阶段", asString(data["phase"]))
	addSummary("step", "步骤", formFillValueString(data["step"]))
	addSummary("action", "动作", asString(data["last_action"]))
	addSummary("detail", "详情", truncate(asString(data["last_excerpt"]), 180))
	addSummary("url", "页面", asString(data["url"]))
	addSummary("success", "是否成功", formFillSuccessSummary(data))
	addSummary("result", "结果", firstNonEmpty(asString(data["submission_result"]), asString(data["final_text"])))
	addSummary("error", "错误", firstNonEmpty(asString(data["error"]), formFillFirstError(data)))
	return summary
}

func formFillEventBusinessFailed(eventName string, data map[string]any) bool {
	if eventName == "run_failed" {
		return true
	}
	if eventName != "run_completed" {
		return false
	}
	if success, ok := formFillBoolValue(data["success"]); ok {
		return !success
	}
	return false
}

func formFillBoolValue(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	}
	return false, false
}

func formFillFirstError(data map[string]any) string {
	if s := strings.TrimSpace(asString(data["error"])); s != "" {
		return s
	}
	errorsValue, ok := data["errors"].([]any)
	if !ok {
		return ""
	}
	for _, item := range errorsValue {
		if s := strings.TrimSpace(asString(item)); s != "" {
			return s
		}
	}
	return ""
}

func formFillSuccessSummary(data map[string]any) string {
	success, ok := formFillBoolValue(data["success"])
	if !ok {
		return ""
	}
	if success {
		return "true"
	}
	return "false"
}

func extractSingleQuotedValue(s, prefix string) string {
	idx := strings.Index(s, prefix+"'")
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(prefix)+1:]
	end := strings.Index(rest, "'")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func formFillValueString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	default:
		return ""
	}
}

func sanitizeFormFillEventValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitizeFormFillEventValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitizeFormFillEventValue(val)
		}
		return out
	case string:
		return sanitizeFormFillString(t)
	default:
		return v
	}
}

func sanitizeFormFillString(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
	out := formFillSensitiveQueryParamRE.ReplaceAllStringFunc(s, func(match string) string {
		parts := formFillSensitiveQueryParamRE.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return parts[1] + "redacted_param=" + parts[2]
	})
	out = strings.ReplaceAll(out, "?&", "?")
	out = strings.ReplaceAll(out, "&&", "&")
	out = strings.TrimSuffix(out, "?")
	out = strings.TrimSuffix(out, "&")
	return out
}

func appendFormFillRawEvent(event FormFillSSEFinalEvent, sequence int, raw []byte) map[string]any {
	if strings.TrimSpace(event.RunID) == "" || len(raw) == 0 {
		return nil
	}
	dir := firstNonEmpty(os.Getenv("FORM_AGENT_RAW_EVENT_LOG_DIR"), filepath.Join(os.TempDir(), "mcp-sandbox-agent-events"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Warn("mcp.form_fill.raw_event_log_mkdir_failed", "dir", dir, "error", err)
		return nil
	}

	fileName := formFillSafeLogNameRE.ReplaceAllString(event.RunID, "_") + ".jsonl"
	path := filepath.Join(dir, fileName)
	record := map[string]any{
		"logged_at": time.Now().UTC().Format(time.RFC3339Nano),
		"sequence":  sequence,
		"event":     event.Event,
		"run_id":    event.RunID,
		"timestamp": event.Timestamp,
		"raw":       json.RawMessage(raw),
	}
	line, err := json.Marshal(record)
	if err != nil {
		slog.Warn("mcp.form_fill.raw_event_log_marshal_failed", "run_id", event.RunID, "event", event.Event, "error", err)
		return nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Warn("mcp.form_fill.raw_event_log_open_failed", "path", path, "error", err)
		return nil
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		slog.Warn("mcp.form_fill.raw_event_log_write_failed", "path", path, "error", err)
		return nil
	}

	return map[string]any{
		"run_id":                  event.RunID,
		"event":                   event.Event,
		"sequence":                sequence,
		"log_path":                path,
		"contains_sensitive_data": true,
	}
}

func attachFormFillRawEventRef(event FormFillSSEFinalEvent, ref map[string]any) FormFillSSEFinalEvent {
	if len(ref) == 0 {
		return event
	}
	data := cloneMap(event.Data)
	if data == nil {
		data = map[string]any{}
	}
	data["raw_event_ref"] = ref
	event.Data = data

	obj := map[string]any{
		"event": event.Event,
		"data":  data,
	}
	if event.RunID != "" {
		obj["run_id"] = event.RunID
	}
	if event.Timestamp != "" {
		obj["timestamp"] = event.Timestamp
	}
	if raw, err := json.Marshal(obj); err == nil {
		event.Raw = raw
	}
	return event
}

func formFillProgressMessage(event FormFillSSEFinalEvent) string {
	if payload, _ := event.Data["payload"].(map[string]any); payload != nil {
		if msg := strings.TrimSpace(asString(payload["text"])); msg != "" {
			return msg
		}
		if title := strings.TrimSpace(asString(payload["title"])); title != "" {
			return title
		}
	}
	for _, key := range []string{"message", "text", "summary", "action", "step", "title"} {
		if msg := strings.TrimSpace(asString(event.Data[key])); msg != "" {
			return msg
		}
	}
	if phase := strings.TrimSpace(asString(event.Data["phase"])); phase != "" {
		return "开始 " + phase + " 阶段"
	}
	switch event.Event {
	case "run_started":
		return "表单任务已创建"
	case "phase_started":
		return "进入表单处理阶段"
	case "user_response_received":
		return "已收到用户确认"
	case "step_start":
		return "开始执行浏览器步骤"
	case "step_end":
		return "完成一个浏览器步骤"
	default:
		return "表单 agent 事件：" + event.Event
	}
}

func redactProxyURL(url string) string {
	if idx := strings.Index(url, "?"); idx >= 0 {
		url = url[:idx]
	}
	return url
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
