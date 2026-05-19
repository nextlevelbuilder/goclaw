package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultOpencodeAgentTemplateID      = ""
	defaultOpencodeAgentControlPlaneURL = ""
	defaultOpencodeAgentSandboxLifetime = 3600
	defaultOpencodeAgentReadyTimeout    = 180 * time.Second
	defaultOpencodeAgentRunTimeout      = 600 * time.Second
	defaultOpencodeAgentName            = "build"
)

type opencodeAgentConfig struct {
	ControlPlaneURL    string
	TemplateID         string
	LLMBaseURL         string
	LLMAPIKey          string
	LLMModel           string
	ProviderID         string
	GoClawBaseURL      string
	TenantID           string
	SandboxLifetimeSec int
	ReadyTimeout       time.Duration
	RunTimeout         time.Duration
}

func opencodeAgentConfigFromEnv() opencodeAgentConfig {
	return opencodeAgentConfig{
		ControlPlaneURL: firstNonEmpty(
			os.Getenv("OPENCODE_AGENT_CONTROL_PLANE_URL"),
			os.Getenv("FORM_AGENT_CONTROL_PLANE_URL"),
			defaultOpencodeAgentControlPlaneURL,
		),
		TemplateID: firstNonEmpty(os.Getenv("OPENCODE_AGENT_TEMPLATE_ID"), defaultOpencodeAgentTemplateID),
		LLMBaseURL: firstNonEmpty(
			os.Getenv("OPENCODE_AGENT_LLM_BASE_URL"),
			os.Getenv("LLM_BASE_URL"),
			os.Getenv("AIDER_OPENAI_API_BASE"),
			os.Getenv("OPENAI_API_BASE"),
			os.Getenv("OPENAI_BASE_URL"),
		),
		LLMAPIKey: firstNonEmpty(
			os.Getenv("OPENCODE_AGENT_LLM_API_KEY"),
			os.Getenv("LLM_API_KEY"),
			os.Getenv("AIDER_OPENAI_API_KEY"),
			os.Getenv("OPENAI_API_KEY"),
		),
		LLMModel: firstNonEmpty(
			os.Getenv("OPENCODE_AGENT_LLM_MODEL"),
			os.Getenv("LLM_MODEL"),
			os.Getenv("AIDER_MODEL"),
			"gpt-5.4",
		),
		ProviderID:         strings.TrimSpace(os.Getenv("OPENCODE_AGENT_PROVIDER_ID")),
		GoClawBaseURL:      firstNonEmpty(os.Getenv("OPENCODE_AGENT_GOCLAW_API_BASE_URL"), os.Getenv("GOCLAW_API_BASE_URL"), os.Getenv("GOCLAW_BASE_URL")),
		TenantID:           firstNonEmpty(os.Getenv("OPENCODE_AGENT_TENANT_ID"), "goclaw"),
		SandboxLifetimeSec: envOrInt("OPENCODE_AGENT_SANDBOX_LIFETIME_SEC", defaultOpencodeAgentSandboxLifetime),
		ReadyTimeout:       time.Duration(envOrInt("OPENCODE_AGENT_READY_TIMEOUT_SEC", int(defaultOpencodeAgentReadyTimeout/time.Second))) * time.Second,
		RunTimeout:         time.Duration(envOrInt("OPENCODE_AGENT_RUN_TIMEOUT_SEC", int(defaultOpencodeAgentRunTimeout/time.Second))) * time.Second,
	}
}

type OpencodeRunTool struct {
	httpClient *http.Client
	config     func() opencodeAgentConfig
}

func NewOpencodeRunTool() *OpencodeRunTool {
	return &OpencodeRunTool{
		httpClient: &http.Client{Timeout: 30 * time.Minute},
		config:     opencodeAgentConfigFromEnv,
	}
}

func (t *OpencodeRunTool) Name() string { return "opencode_run" }

func (t *OpencodeRunTool) Description() string {
	return "Run a coding task inside an isolated opencode sandbox. Use this for user requests that require writing, editing, or testing code in a workspace (for example: add a function, write tests, fix bugs, refactor). If the user explicitly asks to call opencode_run, do not fall back to local exec/curl/python after this tool fails; report the opencode result or error. For connectivity/progress probes, keep the requested task tiny and bounded; do not invent large generated reports, many-file workloads, or long-running benchmarks unless the user explicitly asks for them. The sandbox is created on demand and destroyed automatically. Returns opencode's final summary text."
}

func (t *OpencodeRunTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language coding task, e.g. 'Add a multiply(a, b) function in Python, add pytest coverage, run pytest, and fix failures.'",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional short title for the opencode session.",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "Opencode agent to use; defaults to 'build'.",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Max seconds opencode may take. Default 600. Values below the server default are raised to the default so long-running opencode tasks are not cut off too early. The GoClaw MCP server timeout must be greater than this value.",
			},
			"tenant_id": map[string]any{
				"type":        "string",
				"description": "Optional tenant id for sandbox creation.",
			},
			"session_id": map[string]any{
				"type":        "string",
				"description": "Optional business session id for sandbox creation.",
			},
			"goclaw_admin_api_key": map[string]any{
				"type":        "string",
				"description": "Optional GoClaw admin API key (Bearer token). If provided, it is injected into the sandbox as GOCLAW_ADMIN_API_KEY so opencode can perform the requested GoClaw API operation without printing the secret.",
			},
			"goclaw_user_id": map[string]any{
				"type":        "string",
				"description": "Optional GoClaw user id sent as X-GoClaw-User-Id when a GoClaw callback helper is available.",
			},
			"goclaw_base_url": map[string]any{
				"type":        "string",
				"description": "Optional GoClaw API base URL for callbacks. Used for sandbox network allowlist and task instructions. Do not pass a UI URL such as /chat.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *OpencodeRunTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	query := strings.TrimSpace(asString(args["query"]))
	if query == "" {
		return tools.ErrorResult("opencode_run: query is required")
	}

	cfg := t.config()
	if err := validateOpencodeAgentConfig(cfg); err != nil {
		return tools.ErrorResult("opencode_run: " + err.Error())
	}

	title := strings.TrimSpace(asString(args["title"]))
	agentName := firstNonEmpty(strings.TrimSpace(asString(args["agent"])), defaultOpencodeAgentName)
	tenantID := firstNonEmpty(asString(args["tenant_id"]), cfg.TenantID)
	sessionID := firstNonEmpty(asString(args["session_id"]), "opencode-run-"+uuid.NewString())
	runTimeout := cfg.RunTimeout
	if sec := intArg(args["timeout_sec"], 0); sec > 0 {
		requestedTimeout := time.Duration(sec) * time.Second
		if requestedTimeout < cfg.RunTimeout {
			requestedTimeout = cfg.RunTimeout
		}
		runTimeout = requestedTimeout
	}

	callback := opencodeCallbackInjection{
		AdminAPIKey: strings.TrimSpace(asString(args["goclaw_admin_api_key"])),
		UserID:      strings.TrimSpace(asString(args["goclaw_user_id"])),
		BaseURL:     firstNonEmpty(strings.TrimSpace(asString(args["goclaw_base_url"])), cfg.GoClawBaseURL),
	}
	effectiveQuery := query
	if prefix := callback.queryPrefix(); prefix != "" {
		effectiveQuery = prefix + "\n\n" + query
	}

	log := slog.With("tool", t.Name(), "session_id", sessionID, "tenant_id", tenantID, "run_timeout_sec", int(runTimeout/time.Second))
	log.Info("mcp.opencode.create_sandbox", "has_admin_key", callback.AdminAPIKey != "", "has_user_id", callback.UserID != "")
	emitOpencodeProgressEvent(ctx, 2, "正在创建 opencode sandbox。", "sandbox_create_start", sessionID, map[string]any{
		"tenant_id": tenantID,
	})

	sandbox, err := t.createSandbox(ctx, cfg, tenantID, sessionID, callback)
	if err != nil {
		log.Error("mcp.opencode.create_sandbox_failed", "error", err)
		return tools.ErrorResult("opencode_run: create sandbox: " + err.Error())
	}

	httpBase, opencodeBase, err := t.waitSandboxReady(ctx, cfg, sandbox.SandboxID)
	if err != nil {
		log.Error("mcp.opencode.sandbox_not_ready", "sandbox_id", sandbox.SandboxID, "error", err)
		return tools.ErrorResult("opencode_run: wait sandbox ready: " + err.Error())
	}
	log.Info("mcp.opencode.sandbox_ready", "sandbox_id", sandbox.SandboxID, "adapter_base", httpBase, "opencode_base", opencodeBase)
	emitOpencodeProgressEvent(ctx, 10, "opencode sandbox 已就绪。", "sandbox_ready", sessionID, map[string]any{
		"sandbox_id":   sandbox.SandboxID,
		"adapter_base": redactProxyURL(httpBase),
		"has_native":   opencodeBase != "",
	})

	if err := t.ensureAgentReady(ctx, httpBase, cfg, callback); err != nil {
		log.Error("mcp.opencode.agent_not_ready", "sandbox_id", sandbox.SandboxID, "http_base", httpBase, "error", err)
		return tools.ErrorResult("opencode_run: agent init: " + err.Error())
	}
	log.Info("mcp.opencode.agent_ready", "sandbox_id", sandbox.SandboxID)
	emitOpencodeProgressEvent(ctx, 15, "opencode 适配服务已完成模型配置。", "agent_ready", sessionID, map[string]any{
		"sandbox_id":  sandbox.SandboxID,
		"provider_id": cfg.ProviderID,
		"model":       cfg.LLMModel,
	})

	if opencodeBase != "" {
		if err := t.waitOpencodeNativeReady(ctx, opencodeBase, cfg.ReadyTimeout); err != nil {
			log.Error("mcp.opencode.native_not_ready", "sandbox_id", sandbox.SandboxID, "opencode_base", opencodeBase, "error", err)
			return tools.ErrorResult("opencode_run: opencode native API not ready: " + err.Error())
		}
		log.Info("mcp.opencode.native_ready", "sandbox_id", sandbox.SandboxID, "opencode_base", opencodeBase)
		emitOpencodeProgressEvent(ctx, 20, "opencode 原生 API 已可连接。", "native_ready", sessionID, map[string]any{
			"sandbox_id":    sandbox.SandboxID,
			"opencode_base": redactProxyURL(opencodeBase),
		})
	}

	runCtx, cancel := context.WithTimeout(ctx, runTimeout+30*time.Second)
	defer cancel()

	start := time.Now()

	// If :4096 native is reachable, use async + SSE for streaming progress.
	if opencodeBase != "" {
		backend := NewOpencodeSSEBackend(t.httpClient)
		result, err := backend.Run(runCtx, OpencodeSSERequest{
			OpencodeBase: opencodeBase,
			AdapterBase:  httpBase,
			Query:        effectiveQuery,
			Title:        firstNonEmpty(title, "opencode-task"),
			Agent:        agentName,
			Model:        cfg.LLMModel,
			ProviderID:   cfg.ProviderID,
		})
		if err != nil {
			log.Error("mcp.opencode.sse_run_failed", "sandbox_id", sandbox.SandboxID, "error", err)
			return tools.ErrorResult("opencode_run: " + err.Error())
		}
		log.Info("mcp.opencode.run_completed",
			"sandbox_id", sandbox.SandboxID,
			"session_id", result.SessionID,
			"finish", result.Finish,
			"duration_sec", time.Since(start).Seconds(),
		)
		emitOpencodeProgressEvent(ctx, 100, "opencode 执行完成。", "run_completed", result.SessionID, map[string]any{
			"sandbox_id":   sandbox.SandboxID,
			"session_id":   result.SessionID,
			"finish":       result.Finish,
			"duration_sec": time.Since(start).Seconds(),
		})
		return tools.NewResult(result.Text)
	}

	// Fallback: sync mode via adapter when :4096 is not available.
	input := map[string]any{
		"title": firstNonEmpty(title, "opencode-task"),
		"agent": agentName,
	}
	if cfg.ProviderID != "" && cfg.LLMModel != "" {
		input["model"] = cfg.ProviderID + "/" + cfg.LLMModel
	}
	runBody := map[string]any{
		"query":       effectiveQuery,
		"mode":        "general",
		"input":       input,
		"timeout_sec": int(runTimeout / time.Second),
	}
	runURL := strings.TrimRight(httpBase, "/") + "/v1/agent/run"
	log.Info("mcp.opencode.run_post_fallback", "url", runURL)

	raw, err := t.postJSON(runCtx, runURL, runBody)
	if err != nil {
		log.Error("mcp.opencode.run_failed", "sandbox_id", sandbox.SandboxID, "error", err)
		return tools.ErrorResult("opencode_run: " + err.Error())
	}
	log.Info("mcp.opencode.run_completed",
		"sandbox_id", sandbox.SandboxID,
		"duration_sec", time.Since(start).Seconds(),
	)
	return tools.NewResult(string(raw))
}

type opencodeCallbackInjection struct {
	AdminAPIKey string
	UserID      string
	BaseURL     string
}

func (t *OpencodeRunTool) createSandbox(ctx context.Context, cfg opencodeAgentConfig, tenantID, sessionID string, callback opencodeCallbackInjection) (*createSandboxResponse, error) {
	envVars := map[string]string{
		"LLM_BASE_URL": cfg.LLMBaseURL,
		"LLM_API_KEY":  cfg.LLMAPIKey,
		"LLM_MODEL":    cfg.LLMModel,
	}
	if cfg.ProviderID != "" {
		envVars["OPENCODE_PROVIDER_ID"] = cfg.ProviderID
	}
	if callback.BaseURL != "" {
		envVars["GOCLAW_API_BASE_URL"] = callback.BaseURL
		envVars["GOCLAW_BASE_URL"] = callback.BaseURL
	}
	if callback.AdminAPIKey != "" {
		envVars["GOCLAW_ADMIN_API_KEY"] = callback.AdminAPIKey
	}
	if callback.UserID != "" {
		envVars["GOCLAW_USER_ID"] = callback.UserID
	} else if callback.AdminAPIKey != "" {
		envVars["GOCLAW_USER_ID"] = "admin"
	}

	allowOutCIDRs := sandboxAllowOutCIDRs(cfg.ControlPlaneURL, callback.BaseURL)

	body := map[string]any{
		"template_id":           cfg.TemplateID,
		"tenant_id":             tenantID,
		"session_id":            sessionID,
		"source":                "api",
		"description":           "goclaw mcp-agent opencode coding sub-agent",
		"lifetime_sec":          cfg.SandboxLifetimeSec,
		"allow_internet_access": true,
		"network": map[string]any{
			"allowOut": allowOutCIDRs,
		},
		"metadata": map[string]string{
			"caller":     "goclaw-mcp-agent",
			"request_id": uuid.NewString(),
		},
		"env_vars": envVars,
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

func sandboxAllowOutCIDRs(rawURLs ...string) []string {
	cidrs := make([]string, 0, len(rawURLs))
	seen := map[string]bool{}
	for _, raw := range rawURLs {
		host := hostFromURL(raw)
		ip := net.ParseIP(host)
		if ip == nil {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			cidr := ip4.String() + "/32"
			if !seen[cidr] {
				cidrs = append(cidrs, cidr)
				seen[cidr] = true
			}
		}
	}
	return cidrs
}

func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return strings.Trim(raw, "[]")
}

func (t *OpencodeRunTool) waitSandboxReady(ctx context.Context, cfg opencodeAgentConfig, sandboxID string) (adapterBase string, opencodeBase string, err error) {
	deadline := time.Now().Add(cfg.ReadyTimeout)
	control := strings.TrimRight(cfg.ControlPlaneURL, "/")
	for {
		if time.Now().After(deadline) {
			return "", "", fmt.Errorf("timed out after %s waiting for sandbox %s", cfg.ReadyTimeout, sandboxID)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, control+"/api/v1/sandboxes/"+sandboxID, nil)
		if err != nil {
			return "", "", err
		}
		resp, err := t.httpClient.Do(req)
		if err != nil {
			return "", "", err
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", "", readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", "", fmt.Errorf("GET sandbox status %d: %s", resp.StatusCode, truncate(string(raw), 500))
		}

		var status createSandboxResponse
		if err := json.Unmarshal(raw, &status); err != nil {
			return "", "", fmt.Errorf("decode sandbox status: %w", err)
		}
		if status.Status == "failed" {
			return "", "", fmt.Errorf("sandbox failed: %s", truncate(string(raw), 500))
		}
		if status.Status == "ready" {
			adapterBase = strings.TrimRight(status.AccessURLs["49999"], "/")
			if adapterBase == "" {
				return "", "", fmt.Errorf("sandbox ready but access_urls[49999] missing")
			}
			opencodeBase = strings.TrimRight(status.AccessURLs["4096"], "/")
			return adapterBase, opencodeBase, nil
		}

		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (t *OpencodeRunTool) ensureAgentReady(ctx context.Context, httpBase string, cfg opencodeAgentConfig, callback opencodeCallbackInjection) error {
	raw, err := t.get(ctx, httpBase+"/healthz")
	if err != nil {
		return err
	}
	var health map[string]any
	if err := json.Unmarshal(raw, &health); err != nil {
		return fmt.Errorf("decode healthz: %w", err)
	}
	status := asString(health["status"])
	if status == "ok" && cfg.ProviderID == "" {
		return nil
	}
	if status == "ok" && cfg.ProviderID != "" {
		if runtimeConfig, _ := health["runtime_config"].(map[string]any); asString(runtimeConfig["opencode_provider_id"]) == cfg.ProviderID {
			return nil
		}
	}
	if status != "needs_init" && status != "ok" {
		return fmt.Errorf("unexpected healthz status %q: %s", status, truncate(string(raw), 500))
	}

	initBody := map[string]any{
		"llm_base_url": cfg.LLMBaseURL,
		"llm_api_key":  cfg.LLMAPIKey,
		"llm_model":    cfg.LLMModel,
	}
	if cfg.ProviderID != "" {
		initBody["opencode_provider_id"] = cfg.ProviderID
	}
	initURL := httpBase + "/v1/init"
	_, err = t.postJSON(ctx, initURL, initBody)
	return err
}

func (c opencodeCallbackInjection) queryPrefix() string {
	if c.BaseURL == "" && c.AdminAPIKey == "" && c.UserID == "" {
		return ""
	}
	lines := []string{
		"GoClaw integration context:",
	}
	if c.BaseURL != "" {
		lines = append(lines,
			"- Use this GoClaw API base URL exactly: "+c.BaseURL,
			"- Use the GoClaw API key supplied in the task text or available environment variables.",
		)
	}
	if c.AdminAPIKey != "" {
		lines = append(lines,
			"- Use the GoClaw admin API key as `Authorization: Bearer <admin_api_key>`, but never print or echo it.",
			"- For GoClaw agent management, use only `GET /v1/agents` and `POST /v1/agents`; do not call `/api/agents`, `/api/v1/agents`, or `/api/admin/agents`.",
			"- Send `X-GoClaw-User-Id: ${GOCLAW_USER_ID:-admin}` for GoClaw agent management requests.",
			"- Use Python stdlib `urllib.request`; do not use curl for GoClaw API requests.",
			"- For creating a simple test GoClaw agent, use a minimal JSON body with `agent_key`, `display_name`, `agent_type: \"predefined\"`, and, unless the user specifies otherwise, `provider: \"anthropic\"`, `model: \"anthropic/claude-sonnet-4-5-20250929\"`.",
			"- Check for an existing agent with the same `agent_key` at most once. If it already exists, report it instead of retrying creation.",
			"- After the requested GoClaw API operation succeeds and is verified, stop and return a concise result; do not continue endpoint discovery or long reporting.",
		)
	}
	if c.UserID != "" {
		lines = append(lines,
			"- The GoClaw user id is available as environment variable GOCLAW_USER_ID.",
		)
	}
	return strings.Join(lines, "\n")
}

func (t *OpencodeRunTool) waitOpencodeNativeReady(ctx context.Context, opencodeBase string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := strings.TrimRight(opencodeBase, "/") + "/session?directory=%2Fworkspace%2Frepo"
	var lastErr error
	for {
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("timed out after %s waiting for %s: last error: %w", timeout, redactProxyURL(url), lastErr)
			}
			return fmt.Errorf("timed out after %s waiting for %s", timeout, redactProxyURL(url))
		}

		if _, err := t.get(ctx, url); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func (t *OpencodeRunTool) get(ctx context.Context, url string) ([]byte, error) {
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

func (t *OpencodeRunTool) postJSON(ctx context.Context, url string, body any) ([]byte, error) {
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

func validateOpencodeAgentConfig(cfg opencodeAgentConfig) error {
	switch {
	case cfg.ControlPlaneURL == "":
		return fmt.Errorf("OPENCODE_AGENT_CONTROL_PLANE_URL is required")
	case cfg.TemplateID == "":
		return fmt.Errorf("OPENCODE_AGENT_TEMPLATE_ID is required")
	case cfg.LLMBaseURL == "":
		return fmt.Errorf("LLM base URL is required (OPENCODE_AGENT_LLM_BASE_URL or LLM_BASE_URL)")
	case cfg.LLMAPIKey == "":
		return fmt.Errorf("LLM API key is required (OPENCODE_AGENT_LLM_API_KEY or LLM_API_KEY)")
	case cfg.LLMModel == "":
		return fmt.Errorf("LLM model is required (OPENCODE_AGENT_LLM_MODEL or LLM_MODEL)")
	default:
		return nil
	}
}
