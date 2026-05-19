package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultFormAgentControlPlaneURL = ""
	defaultFormAgentTemplateID      = ""
	defaultFormAgentSandboxLifetime = 3600
	defaultFormAgentReadyTimeout    = 180 * time.Second
	defaultFormAgentSubmitTimeout   = 900
	defaultFormAgentMaxSteps        = 60
)

type formFillSession struct {
	RunID      string
	QuestionID string
	InputURL   string
	SandboxID  string
	HTTPBase   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

type FormFillSessionStore struct {
	mu       sync.Mutex
	sessions map[string]formFillSession
}

func NewFormFillSessionStore() *FormFillSessionStore {
	return &FormFillSessionStore{sessions: make(map[string]formFillSession)}
}

func (s *FormFillSessionStore) Put(key string, session formFillSession) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[key] = session
}

func (s *FormFillSessionStore) Get(key string) (formFillSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return formFillSession{}, false
	}
	if !session.ExpiresAt.IsZero() && time.Now().After(session.ExpiresAt) {
		delete(s.sessions, key)
		return formFillSession{}, false
	}
	return session, true
}

func (s *FormFillSessionStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}

type formFillConfig struct {
	ControlPlaneURL    string
	TemplateID         string
	LLMBaseURL         string
	LLMAPIKey          string
	LLMModel           string
	TenantID           string
	SandboxLifetimeSec int
	ReadyTimeout       time.Duration
}

func formFillConfigFromEnv() formFillConfig {
	return formFillConfig{
		ControlPlaneURL: firstNonEmpty(os.Getenv("FORM_AGENT_CONTROL_PLANE_URL"), defaultFormAgentControlPlaneURL),
		TemplateID:      firstNonEmpty(os.Getenv("FORM_AGENT_TEMPLATE_ID"), defaultFormAgentTemplateID),
		LLMBaseURL: firstNonEmpty(
			os.Getenv("FORM_AGENT_LLM_BASE_URL"),
			os.Getenv("LLM_BASE_URL"),
			os.Getenv("AIDER_OPENAI_API_BASE"),
			os.Getenv("OPENAI_API_BASE"),
			os.Getenv("OPENAI_BASE_URL"),
		),
		LLMAPIKey: firstNonEmpty(
			os.Getenv("FORM_AGENT_LLM_API_KEY"),
			os.Getenv("LLM_API_KEY"),
			os.Getenv("AIDER_OPENAI_API_KEY"),
			os.Getenv("OPENAI_API_KEY"),
		),
		LLMModel: firstNonEmpty(
			os.Getenv("FORM_AGENT_LLM_MODEL"),
			os.Getenv("LLM_MODEL"),
			os.Getenv("AIDER_MODEL"),
			"gpt-5.4",
		),
		TenantID: firstNonEmpty(os.Getenv("FORM_AGENT_TENANT_ID"), "goclaw"),
		SandboxLifetimeSec: envOrInt(
			"FORM_AGENT_SANDBOX_LIFETIME_SEC",
			envOrInt("FORM_AGENT_SANDBOX_TIMEOUT_SEC", defaultFormAgentSandboxLifetime),
		),
		ReadyTimeout: time.Duration(envOrInt("FORM_AGENT_READY_TIMEOUT_SEC", int(defaultFormAgentReadyTimeout/time.Second))) * time.Second,
	}
}

type formFillBaseTool struct {
	httpClient *http.Client
	store      *FormFillSessionStore
	config     func() formFillConfig
}

func newFormFillBaseTool(store *FormFillSessionStore) formFillBaseTool {
	return formFillBaseTool{
		httpClient: &http.Client{Timeout: 15 * time.Minute},
		store:      store,
		config:     formFillConfigFromEnv,
	}
}

type FeishuFormFillRunTool struct {
	formFillBaseTool
	backend *FormFillSSEBackend
}

func NewFeishuFormFillRunTool(store *FormFillSessionStore) *FeishuFormFillRunTool {
	base := newFormFillBaseTool(store)
	return &FeishuFormFillRunTool{
		formFillBaseTool: base,
		backend:          NewFormFillSSEBackend(base.httpClient),
	}
}

func (t *FeishuFormFillRunTool) Name() string { return "feishu_form_fill_run" }

func (t *FeishuFormFillRunTool) Description() string {
	return "Start a Feishu meeting questionnaire form-fill run from natural language. Streams progress from the browser-use agent and returns the final SSE event JSON verbatim, usually ask_user_question for confirmation."
}

func (t *FeishuFormFillRunTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "User's natural-language meeting registration request, e.g. 我叫张三，5月8号参会，3个人参加。",
			},
			"tenant_id": map[string]any{
				"type":        "string",
				"description": "Optional tenant/caller identifier for sandbox creation.",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Optional business session id for sandbox creation.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *FeishuFormFillRunTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	query := strings.TrimSpace(asString(args["query"]))
	if query == "" {
		return tools.ErrorResult("feishu_form_fill_run: query is required")
	}

	cfg := t.config()
	if err := validateFormFillConfig(cfg); err != nil {
		return tools.ErrorResult("feishu_form_fill_run: " + err.Error())
	}

	tenantID := firstNonEmpty(asString(args["tenant_id"]), cfg.TenantID)
	sessionID := firstNonEmpty(asString(args["session_id"]), "meeting-form-"+uuid.NewString())

	log := slog.With("tool", t.Name(), "session_id", sessionID, "tenant_id", tenantID)
	log.Info("mcp.form_fill.create_sandbox")

	sandbox, err := t.createSandbox(ctx, cfg, tenantID, sessionID)
	if err != nil {
		log.Error("mcp.form_fill.create_sandbox_failed", "error", err)
		return tools.ErrorResult("feishu_form_fill_run: create sandbox: " + err.Error())
	}

	session, err := t.waitSandboxReady(ctx, cfg, sandbox.SandboxID)
	if err != nil {
		log.Error("mcp.form_fill.sandbox_not_ready", "sandbox_id", sandbox.SandboxID, "error", err)
		return tools.ErrorResult("feishu_form_fill_run: wait sandbox ready: " + err.Error())
	}

	if err := t.ensureAgentReady(ctx, session.HTTPBase, cfg); err != nil {
		log.Error("mcp.form_fill.agent_not_ready", "sandbox_id", session.SandboxID, "http_base", session.HTTPBase, "error", err)
		return tools.ErrorResult("feishu_form_fill_run: agent init: " + err.Error())
	}

	final, err := t.backend.Run(ctx, FormFillRunRequest{
		HTTPBase: session.HTTPBase,
		Query:    query,
	})
	if err != nil {
		log.Error("mcp.form_fill.run_failed", "sandbox_id", session.SandboxID, "error", err)
		return tools.ErrorResult("feishu_form_fill_run: run: " + err.Error())
	}

	t.rememberFinalEvent(session, final)
	log.Info("mcp.form_fill.run_completed", "sandbox_id", session.SandboxID, "run_id", final.RunID, "event", final.Event)
	return tools.DirectResult(string(final.Raw))
}

func (t *FeishuFormFillRunTool) rememberFinalEvent(session formFillSession, final FormFillSSEFinalEvent) {
	switch final.Event {
	case "ask_user_question":
		questionID := asString(final.Data["question_id"])
		inputURL := asString(final.Data["input_url"])
		expiresAt := unixFloatToTime(final.Data["expires_at"])
		if expiresAt.IsZero() {
			expiresAt = time.Now().Add(30 * time.Minute)
		}
		if final.RunID != "" {
			session.RunID = final.RunID
			session.QuestionID = questionID
			session.InputURL = inputURL
			session.ExpiresAt = expiresAt
			t.store.Put(final.RunID, session)
		}
	case "run_completed", "run_failed", "run_cancelled":
		if final.RunID != "" {
			t.store.Delete(final.RunID)
		}
	}
}

type FeishuFormFillInputTool struct {
	formFillBaseTool
	backend *FormFillSSEBackend
}

func NewFeishuFormFillInputTool(store *FormFillSessionStore) *FeishuFormFillInputTool {
	base := newFormFillBaseTool(store)
	return &FeishuFormFillInputTool{
		formFillBaseTool: base,
		backend:          NewFormFillSSEBackend(base.httpClient),
	}
}

func (t *FeishuFormFillInputTool) Name() string { return "feishu_form_fill_input" }

func (t *FeishuFormFillInputTool) Description() string {
	return "Continue a Feishu meeting questionnaire form-fill run after the user confirms, edits, supplements, chats, or cancels. Streams progress and returns the final SSE event JSON verbatim."
}

func (t *FeishuFormFillInputTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"run_id": map[string]any{
				"type":        "string",
				"description": "The run_id returned by feishu_form_fill_run.",
			},
			"question_id": map[string]any{
				"type":        "string",
				"description": "The question_id returned in the previous ask_user_question event.",
			},
			"content": map[string]any{
				"type":        "object",
				"description": "User response content. Use {\"text\":\"确认\"} for natural language, {\"decision\":\"confirm\"} for button confirmation, or include fields for structured edits.",
				"properties": map[string]any{
					"text": map[string]any{
						"type": "string",
					},
					"decision": map[string]any{
						"type":        "string",
						"description": "confirm, edit, cancel, or another action accepted by the downstream form agent.",
					},
					"fields": map[string]any{
						"type": "object",
					},
					"supplement": map[string]any{
						"type": "object",
					},
				},
			},
		},
		"required": []string{"run_id", "question_id", "content"},
	}
}

func (t *FeishuFormFillInputTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	runID := strings.TrimSpace(asString(args["run_id"]))
	if runID == "" {
		return tools.ErrorResult("feishu_form_fill_input: run_id is required")
	}

	session, ok := t.store.Get(runID)
	if !ok || session.HTTPBase == "" {
		return tools.ErrorResult("feishu_form_fill_input: run not found or expired; please start a new feishu_form_fill_run")
	}

	questionID := firstNonEmpty(asString(args["question_id"]), session.QuestionID)
	if questionID == "" {
		return tools.ErrorResult("feishu_form_fill_input: question_id is required")
	}

	content, ok := args["content"].(map[string]any)
	if !ok || len(content) == 0 {
		if text := strings.TrimSpace(asString(args["text"])); text != "" {
			content = map[string]any{"text": text}
		} else if decision := strings.TrimSpace(asString(args["decision"])); decision != "" {
			content = map[string]any{"decision": decision}
		} else {
			return tools.ErrorResult("feishu_form_fill_input: content object is required")
		}
	}

	final, err := t.backend.Input(ctx, FormFillInputRequest{
		HTTPBase:   session.HTTPBase,
		RunID:      runID,
		QuestionID: questionID,
		InputURL:   session.InputURL,
		Content:    content,
	})
	if err != nil {
		return tools.ErrorResult("feishu_form_fill_input: input: " + err.Error())
	}

	(&FeishuFormFillRunTool{formFillBaseTool: t.formFillBaseTool}).rememberFinalEvent(session, final)
	slog.Info("mcp.form_fill.input_completed", "sandbox_id", session.SandboxID, "run_id", final.RunID, "event", final.Event)
	return tools.DirectResult(string(final.Raw))
}

type createSandboxResponse struct {
	SandboxID  string            `json:"sandbox_id"`
	Status     string            `json:"status"`
	TenantID   string            `json:"tenant_id"`
	TemplateID string            `json:"template_id"`
	AccessURLs map[string]string `json:"access_urls"`
	ExpiresAt  string            `json:"expires_at"`
}

func (t formFillBaseTool) createSandbox(ctx context.Context, cfg formFillConfig, tenantID, sessionID string) (*createSandboxResponse, error) {
	allowOutCIDRs := sandboxAllowOutCIDRs(cfg.ControlPlaneURL)
	body := map[string]any{
		"template_id":           cfg.TemplateID,
		"tenant_id":             tenantID,
		"session_id":            sessionID,
		"source":                "api",
		"description":           "goclaw mcp-agent meeting form-fill test",
		"lifetime_sec":          cfg.SandboxLifetimeSec,
		"allow_internet_access": true,
		"network": map[string]any{
			"allowOut": allowOutCIDRs,
		},
		"metadata": map[string]string{
			"caller":     "goclaw-mcp-agent",
			"request_id": uuid.NewString(),
		},
		"env_vars": map[string]string{
			"LLM_BASE_URL":     cfg.LLMBaseURL,
			"LLM_API_KEY":      cfg.LLMAPIKey,
			"LLM_MODEL":        cfg.LLMModel,
			"LLM_TEMPERATURE":  firstNonEmpty(os.Getenv("FORM_AGENT_LLM_TEMPERATURE"), "1"),
			"BROWSER_HEADLESS": firstNonEmpty(os.Getenv("FORM_AGENT_BROWSER_HEADLESS"), "true"),
		},
	}

	raw, err := t.postJSON(ctx, strings.TrimRight(cfg.ControlPlaneURL, "/")+"/api/v1/sandboxes", body)
	if err != nil {
		return nil, err
	}
	var resp createSandboxResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode create sandbox response: %w", err)
	}
	if resp.SandboxID == "" {
		return nil, fmt.Errorf("create sandbox response missing sandbox_id: %s", truncate(string(raw), 500))
	}
	return &resp, nil
}

func (t formFillBaseTool) waitSandboxReady(ctx context.Context, cfg formFillConfig, sandboxID string) (formFillSession, error) {
	deadline := time.Now().Add(cfg.ReadyTimeout)
	control := strings.TrimRight(cfg.ControlPlaneURL, "/")
	for {
		if time.Now().After(deadline) {
			return formFillSession{}, fmt.Errorf("timed out after %s waiting for sandbox %s", cfg.ReadyTimeout, sandboxID)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, control+"/api/v1/sandboxes/"+sandboxID, nil)
		if err != nil {
			return formFillSession{}, err
		}
		resp, err := t.httpClient.Do(req)
		if err != nil {
			return formFillSession{}, err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return formFillSession{}, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return formFillSession{}, fmt.Errorf("GET sandbox status %d: %s", resp.StatusCode, truncate(string(raw), 500))
		}

		var status createSandboxResponse
		if err := json.Unmarshal(raw, &status); err != nil {
			return formFillSession{}, fmt.Errorf("decode sandbox status: %w", err)
		}
		if status.Status == "failed" {
			return formFillSession{}, fmt.Errorf("sandbox failed: %s", truncate(string(raw), 500))
		}
		if status.Status == "ready" {
			httpBase := strings.TrimRight(status.AccessURLs["49999"], "/")
			if httpBase == "" {
				return formFillSession{}, fmt.Errorf("sandbox ready but access_urls[49999] missing")
			}
			return formFillSession{SandboxID: sandboxID, HTTPBase: httpBase, CreatedAt: time.Now()}, nil
		}

		select {
		case <-ctx.Done():
			return formFillSession{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (t formFillBaseTool) ensureAgentReady(ctx context.Context, httpBase string, cfg formFillConfig) error {
	raw, err := t.get(ctx, httpBase+"/healthz")
	if err != nil {
		return err
	}
	var health map[string]any
	if err := json.Unmarshal(raw, &health); err != nil {
		return fmt.Errorf("decode healthz: %w", err)
	}
	status := asString(health["status"])
	if status == "ok" {
		return nil
	}
	if status != "needs_init" {
		return fmt.Errorf("unexpected healthz status %q: %s", status, truncate(string(raw), 500))
	}

	_, err = t.postJSON(ctx, httpBase+"/v1/init", map[string]any{
		"config": map[string]string{
			"LLM_BASE_URL":    cfg.LLMBaseURL,
			"LLM_API_KEY":     cfg.LLMAPIKey,
			"LLM_MODEL":       cfg.LLMModel,
			"LLM_TEMPERATURE": firstNonEmpty(os.Getenv("FORM_AGENT_LLM_TEMPERATURE"), "1"),
		},
	})
	return err
}

func (t formFillBaseTool) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s status %d: %s", url, resp.StatusCode, truncate(string(raw), 500))
	}
	return raw, nil
}

func (t formFillBaseTool) postJSON(ctx context.Context, url string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s status %d: %s", url, resp.StatusCode, truncate(string(raw), 500))
	}
	return raw, nil
}

func validateFormFillConfig(cfg formFillConfig) error {
	switch {
	case cfg.ControlPlaneURL == "":
		return fmt.Errorf("FORM_AGENT_CONTROL_PLANE_URL is required")
	case cfg.TemplateID == "":
		return fmt.Errorf("FORM_AGENT_TEMPLATE_ID is required")
	case cfg.LLMBaseURL == "":
		return fmt.Errorf("LLM base URL is required (FORM_AGENT_LLM_BASE_URL, LLM_BASE_URL, or AIDER_OPENAI_API_BASE)")
	case cfg.LLMAPIKey == "":
		return fmt.Errorf("LLM API key is required (FORM_AGENT_LLM_API_KEY, LLM_API_KEY, or AIDER_OPENAI_API_KEY)")
	case cfg.LLMModel == "":
		return fmt.Errorf("LLM model is required (FORM_AGENT_LLM_MODEL, LLM_MODEL, or AIDER_MODEL)")
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal compact result: %s"}`, err)
	}
	return string(b)
}

func intArg(v any, def int) int {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return n
		}
	case float64:
		if n > 0 {
			return int(n)
		}
	case json.Number:
		if i, err := n.Int64(); err == nil && i > 0 {
			return int(i)
		}
	}
	return def
}

func envOrInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

func unixFloatToTime(v any) time.Time {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			sec := int64(n)
			nsec := int64((n - float64(sec)) * 1e9)
			return time.Unix(sec, nsec)
		}
	case json.Number:
		if f, err := n.Float64(); err == nil && f > 0 {
			sec := int64(f)
			nsec := int64((f - float64(sec)) * 1e9)
			return time.Unix(sec, nsec)
		}
	}
	return time.Time{}
}
