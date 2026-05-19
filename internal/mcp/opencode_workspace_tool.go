package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultOpencodeWorkspaceLifetime  = 24 * 60 * 60
	defaultOpencodeWorkspaceKey       = "default"
	defaultOpencodeWorkspaceDir       = "/workspace/repo"
	defaultOpencodeWorkspaceStepSec   = 30
	defaultOpencodeWorkspaceNativeSec = 20
)

type OpencodeWorkspaceStore struct {
	mu         sync.Mutex
	workspaces map[string]*opencodeWorkspace
}

func NewOpencodeWorkspaceStore() *OpencodeWorkspaceStore {
	return &OpencodeWorkspaceStore{workspaces: make(map[string]*opencodeWorkspace)}
}

type opencodeWorkspace struct {
	Key          string
	TenantID     string
	SandboxID    string
	SessionID    string
	SessionSlug  string
	AdapterBase  string
	OpencodeBase string
	WebRootURL   string
	WebURL       string
	NativeWebURL string
	Title        string
	Agent        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastOpenedAt time.Time
	LifetimeSec  int
}

type OpencodeWorkspaceOpenTool struct {
	runner *OpencodeRunTool
	store  *OpencodeWorkspaceStore
}

func NewOpencodeWorkspaceOpenTool(store *OpencodeWorkspaceStore) *OpencodeWorkspaceOpenTool {
	if store == nil {
		store = NewOpencodeWorkspaceStore()
	}
	return &OpencodeWorkspaceOpenTool{
		runner: NewOpencodeRunTool(),
		store:  store,
	}
}

func (t *OpencodeWorkspaceOpenTool) Name() string { return "opencode_workspace_open" }

func (t *OpencodeWorkspaceOpenTool) Description() string {
	return "Create or reuse a persistent opencode workspace and return its native web UI URL. Use this when the user wants to open and continue using opencode interactively in a browser, not for one-off background coding tasks. The workspace is keyed by workspace_key and is reused while alive."
}

func (t *OpencodeWorkspaceOpenTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workspace_key": map[string]any{
				"type":        "string",
				"description": "Stable key used to reuse the same opencode workspace. Defaults to 'default'. Use a project/user-specific value when possible.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Optional opencode session title shown in the UI.",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "Opencode agent to use; defaults to 'build'.",
			},
			"tenant_id": map[string]any{
				"type":        "string",
				"description": "Optional tenant id for sandbox creation.",
			},
			"lifetime_sec": map[string]any{
				"type":        "integer",
				"description": "Sandbox lifetime in seconds. Defaults to OPENCODE_WORKSPACE_LIFETIME_SEC or 86400.",
			},
			"force_new": map[string]any{
				"type":        "boolean",
				"description": "Create a fresh workspace even if a cached workspace_key is still alive.",
			},
			"goclaw_admin_api_key": map[string]any{
				"type":        "string",
				"description": "Optional GoClaw admin API key injected into the sandbox for future callbacks.",
			},
			"goclaw_user_id": map[string]any{
				"type":        "string",
				"description": "Optional GoClaw user id injected into the sandbox for future callbacks.",
			},
			"goclaw_base_url": map[string]any{
				"type":        "string",
				"description": "Optional GoClaw API base URL used for sandbox network allowlist and future task instructions.",
			},
		},
	}
}

func (t *OpencodeWorkspaceOpenTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	cfg := t.runner.config()
	if err := validateOpencodeAgentConfig(cfg); err != nil {
		return tools.ErrorResult("opencode_workspace_open: " + err.Error())
	}

	key := firstNonEmpty(strings.TrimSpace(asString(args["workspace_key"])), defaultOpencodeWorkspaceKey)
	tenantID := firstNonEmpty(strings.TrimSpace(asString(args["tenant_id"])), cfg.TenantID)
	title := firstNonEmpty(strings.TrimSpace(asString(args["title"])), "OpenCode workspace")
	agentName := firstNonEmpty(strings.TrimSpace(asString(args["agent"])), defaultOpencodeAgentName)
	lifetimeSec := intArg(args["lifetime_sec"], envOrInt("OPENCODE_WORKSPACE_LIFETIME_SEC", defaultOpencodeWorkspaceLifetime))
	if lifetimeSec <= 0 {
		lifetimeSec = defaultOpencodeWorkspaceLifetime
	}
	forceNew := boolArg(args["force_new"], false)
	cacheKey := tenantID + ":" + key

	log := slog.With("tool", t.Name(), "workspace_key", key, "tenant_id", tenantID)

	if !forceNew {
		if ws, reused := t.reusableWorkspace(ctx, cacheKey); reused {
			ws.LastOpenedAt = time.Now()
			t.ensureReusableSession(ctx, ws, title, agentName, log)
			t.storeWorkspace(cacheKey, ws)
			log.Info("mcp.opencode.workspace_reused", "sandbox_id", ws.SandboxID, "session_id", ws.SessionID)
			return tools.DirectResult(marshalWorkspaceReply(ws, true))
		}
	}

	cfg.SandboxLifetimeSec = lifetimeSec
	callback := opencodeCallbackInjection{
		AdminAPIKey: strings.TrimSpace(asString(args["goclaw_admin_api_key"])),
		UserID:      strings.TrimSpace(asString(args["goclaw_user_id"])),
		BaseURL:     firstNonEmpty(strings.TrimSpace(asString(args["goclaw_base_url"])), cfg.GoClawBaseURL),
	}

	sessionID := newOpencodeWorkspaceSandboxSessionID(key)
	log.Info("mcp.opencode.workspace_create_sandbox", "session_id", sessionID, "lifetime_sec", lifetimeSec)
	emitOpencodeProgressEvent(ctx, 5, "正在启动 OpenCode 工作区。", "workspace_create_start", sessionID, map[string]any{
		"workspace_key": key,
		"tenant_id":     tenantID,
	})

	sandbox, err := t.runner.createSandbox(ctx, cfg, tenantID, sessionID, callback)
	if err != nil {
		return tools.ErrorResult("opencode_workspace_open: create sandbox: " + err.Error())
	}

	adapterBase, opencodeBase, err := t.runner.waitSandboxReady(ctx, cfg, sandbox.SandboxID)
	if err != nil {
		return tools.ErrorResult("opencode_workspace_open: wait sandbox ready: " + err.Error())
	}
	emitOpencodeProgressEvent(ctx, 35, "OpenCode sandbox 已就绪。", "workspace_sandbox_ready", sessionID, map[string]any{
		"workspace_key": key,
		"sandbox_id":    sandbox.SandboxID,
		"adapter_base":  redactProxyURL(adapterBase),
	})

	stepTimeout := opencodeWorkspaceStepTimeout()
	log.Info("mcp.opencode.workspace_init_adapter_start", "sandbox_id", sandbox.SandboxID, "adapter_base", redactProxyURL(adapterBase), "timeout", stepTimeout)
	emitOpencodeProgressEvent(ctx, 50, "正在初始化 OpenCode 适配服务。", "workspace_init_adapter_start", sessionID, map[string]any{
		"workspace_key": key,
		"sandbox_id":    sandbox.SandboxID,
	})
	initCtx, initCancel := context.WithTimeout(ctx, stepTimeout)
	err = t.runner.ensureAgentReady(initCtx, adapterBase, cfg, callback)
	initCancel()
	if err != nil {
		log.Error("mcp.opencode.workspace_init_adapter_failed", "sandbox_id", sandbox.SandboxID, "error", err)
		return tools.ErrorResult("opencode_workspace_open: init adapter: " + err.Error())
	}
	log.Info("mcp.opencode.workspace_init_adapter_done", "sandbox_id", sandbox.SandboxID)
	emitOpencodeProgressEvent(ctx, 58, "OpenCode 适配服务已完成模型配置。", "workspace_init_adapter_done", sessionID, map[string]any{
		"workspace_key": key,
		"sandbox_id":    sandbox.SandboxID,
	})

	if opencodeBase != "" {
		nativeTimeout := opencodeWorkspaceNativeTimeout(cfg.ReadyTimeout)
		log.Info("mcp.opencode.workspace_wait_native_start", "sandbox_id", sandbox.SandboxID, "opencode_base", redactProxyURL(opencodeBase), "timeout", nativeTimeout)
		nativeCtx, nativeCancel := context.WithTimeout(ctx, nativeTimeout)
		err = t.runner.waitOpencodeNativeReady(nativeCtx, opencodeBase, nativeTimeout)
		nativeCancel()
		if err != nil {
			// The returned UI is served through the adapter's /web proxy. Native
			// readiness is useful diagnostics, but a slow :4096 probe should not
			// make workspace opening hit the outer MCP timeout.
			log.Warn("mcp.opencode.workspace_wait_native_skipped", "sandbox_id", sandbox.SandboxID, "error", err)
			emitOpencodeProgressEvent(ctx, 62, "OpenCode 原生接口探测较慢，继续创建 UI 会话。", "workspace_native_probe_skipped", sessionID, map[string]any{
				"workspace_key": key,
				"sandbox_id":    sandbox.SandboxID,
				"error":         err.Error(),
			})
		} else {
			log.Info("mcp.opencode.workspace_wait_native_done", "sandbox_id", sandbox.SandboxID)
		}
	}
	emitOpencodeProgressEvent(ctx, 65, "OpenCode 服务已可访问，正在创建 UI 会话。", "workspace_native_ready", sessionID, map[string]any{
		"workspace_key": key,
		"sandbox_id":    sandbox.SandboxID,
	})

	log.Info("mcp.opencode.workspace_create_ui_session_start", "sandbox_id", sandbox.SandboxID, "agent", agentName, "timeout", stepTimeout)
	sessionCtx, sessionCancel := context.WithTimeout(ctx, stepTimeout)
	session, err := t.createUISession(sessionCtx, adapterBase, title, agentName)
	sessionCancel()
	if err != nil {
		log.Error("mcp.opencode.workspace_create_ui_session_failed", "sandbox_id", sandbox.SandboxID, "error", err)
		return tools.ErrorResult("opencode_workspace_open: create UI session: " + err.Error())
	}

	now := time.Now()
	expiresAt := parseSandboxExpiresAt(sandbox.ExpiresAt)
	if expiresAt.IsZero() {
		expiresAt = now.Add(time.Duration(lifetimeSec) * time.Second)
	}
	ws := &opencodeWorkspace{
		Key:          key,
		TenantID:     tenantID,
		SandboxID:    sandbox.SandboxID,
		SessionID:    session.ID,
		SessionSlug:  session.Slug,
		AdapterBase:  strings.TrimRight(adapterBase, "/"),
		OpencodeBase: strings.TrimRight(opencodeBase, "/"),
		WebRootURL:   strings.TrimRight(adapterBase, "/") + "/web",
		WebURL:       opencodeWebURL(adapterBase, session.ID),
		NativeWebURL: strings.TrimRight(opencodeBase, "/") + "/",
		Title:        session.Title,
		Agent:        session.Agent,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
		LastOpenedAt: now,
		LifetimeSec:  lifetimeSec,
	}
	t.storeWorkspace(cacheKey, ws)

	log.Info("mcp.opencode.workspace_opened", "sandbox_id", ws.SandboxID, "session_id", ws.SessionID, "web_url", ws.WebURL)
	emitOpencodeProgressEvent(ctx, 100, "OpenCode 工作区已就绪。", "workspace_ready", sessionID, map[string]any{
		"workspace_key": key,
		"sandbox_id":    ws.SandboxID,
		"session_id":    ws.SessionID,
		"web_url":       ws.WebURL,
	})

	return tools.DirectResult(marshalWorkspaceReply(ws, false))
}

func (t *OpencodeWorkspaceOpenTool) reusableWorkspace(ctx context.Context, cacheKey string) (*opencodeWorkspace, bool) {
	t.store.mu.Lock()
	ws := t.store.workspaces[cacheKey]
	t.store.mu.Unlock()
	if ws == nil {
		return nil, false
	}
	if !ws.ExpiresAt.IsZero() && time.Now().After(ws.ExpiresAt.Add(-1*time.Minute)) {
		return nil, false
	}
	if !t.workspaceAlive(ctx, ws) {
		return nil, false
	}
	return ws, true
}

func (t *OpencodeWorkspaceOpenTool) storeWorkspace(cacheKey string, ws *opencodeWorkspace) {
	t.store.mu.Lock()
	defer t.store.mu.Unlock()
	t.store.workspaces[cacheKey] = ws
}

func (t *OpencodeWorkspaceOpenTool) workspaceAlive(ctx context.Context, ws *opencodeWorkspace) bool {
	if ws == nil || ws.AdapterBase == "" {
		return false
	}
	raw, err := t.runner.get(ctx, strings.TrimRight(ws.AdapterBase, "/")+"/healthz")
	if err != nil {
		return false
	}
	var health map[string]any
	if err := json.Unmarshal(raw, &health); err != nil {
		return false
	}
	return asString(health["status"]) == "ok"
}

func (t *OpencodeWorkspaceOpenTool) ensureReusableSession(ctx context.Context, ws *opencodeWorkspace, title, agentName string, log *slog.Logger) {
	if ws == nil || ws.AdapterBase == "" {
		return
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, opencodeWorkspaceStepTimeout())
	alive := t.workspaceSessionAlive(checkCtx, ws)
	checkCancel()
	if alive {
		return
	}
	sessionCtx, sessionCancel := context.WithTimeout(ctx, opencodeWorkspaceStepTimeout())
	session, err := t.createUISession(sessionCtx, ws.AdapterBase, title, agentName)
	sessionCancel()
	if err != nil {
		log.Warn("mcp.opencode.workspace_recreate_session_failed", "sandbox_id", ws.SandboxID, "old_session_id", ws.SessionID, "error", err)
		return
	}
	log.Info("mcp.opencode.workspace_recreated_session", "sandbox_id", ws.SandboxID, "old_session_id", ws.SessionID, "new_session_id", session.ID)
	ws.SessionID = session.ID
	ws.SessionSlug = session.Slug
	ws.Title = firstNonEmpty(session.Title, title)
	ws.Agent = firstNonEmpty(session.Agent, agentName)
	ws.WebURL = opencodeWebURL(ws.AdapterBase, ws.SessionID)
}

func (t *OpencodeWorkspaceOpenTool) workspaceSessionAlive(ctx context.Context, ws *opencodeWorkspace) bool {
	if ws == nil || ws.AdapterBase == "" || ws.SessionID == "" {
		return false
	}
	raw, err := t.runner.get(ctx, strings.TrimRight(ws.AdapterBase, "/")+"/v1/sessions")
	if err != nil {
		return false
	}
	var sessions []opencodeUISession
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return false
	}
	for _, session := range sessions {
		if session.ID == ws.SessionID {
			return true
		}
	}
	return false
}

type opencodeUISession struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Directory string `json:"directory"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Agent     string `json:"agent"`
	Version   string `json:"version"`
}

func (t *OpencodeWorkspaceOpenTool) createUISession(ctx context.Context, adapterBase, title, agentName string) (opencodeUISession, error) {
	raw, err := t.runner.postJSON(ctx, strings.TrimRight(adapterBase, "/")+"/v1/sessions", map[string]any{
		"title": title,
		"agent": agentName,
	})
	if err != nil {
		return opencodeUISession{}, err
	}
	var resp struct {
		Session opencodeUISession `json:"session"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return opencodeUISession{}, fmt.Errorf("decode session response: %w", err)
	}
	if resp.Session.ID == "" {
		return opencodeUISession{}, fmt.Errorf("session response missing id: %s", truncate(string(raw), 500))
	}
	if resp.Session.Title == "" {
		resp.Session.Title = title
	}
	if resp.Session.Agent == "" {
		resp.Session.Agent = agentName
	}
	return resp.Session, nil
}

func opencodeWebURL(adapterBase, sessionID string) string {
	return strings.TrimRight(adapterBase, "/") + "/web/" + opencodeEncodedWorkspaceDir() + "/session/" + sessionID
}

func marshalWorkspaceReply(ws *opencodeWorkspace, reused bool) string {
	uiInit := opencodeWorkspaceUIInit(ws)
	payload := map[string]any{
		"version": "goclaw.gateway.reply.v1",
		"kind":    "result",
		"title":   "OpenCode 工作区已就绪",
		"text":    "可以打开 OpenCode 原生 UI 继续使用。",
		"summary": []map[string]any{
			{"id": "workspace_key", "label": "工作区", "value": ws.Key},
			{"id": "session_id", "label": "OpenCode Session", "value": ws.SessionID},
			{"id": "sandbox_id", "label": "Sandbox", "value": ws.SandboxID},
			{"id": "web_url", "label": "OpenCode UI", "value": ws.WebURL},
			{"id": "expires_at", "label": "过期时间", "value": ws.ExpiresAt.Format(time.RFC3339)},
		},
		"questions": []any{},
		"fields":    []any{},
		"data": map[string]any{
			"workspace_key":  ws.Key,
			"tenant_id":      ws.TenantID,
			"sandbox_id":     ws.SandboxID,
			"session_id":     ws.SessionID,
			"session_slug":   ws.SessionSlug,
			"agent":          ws.Agent,
			"title":          ws.Title,
			"web_url":        ws.WebURL,
			"web_root_url":   ws.WebRootURL,
			"native_web_url": ws.NativeWebURL,
			"adapter_base":   ws.AdapterBase,
			"opencode_base":  ws.OpencodeBase,
			"ui_init":        uiInit,
			"reused":         reused,
			"created_at":     ws.CreatedAt.Format(time.RFC3339),
			"expires_at":     ws.ExpiresAt.Format(time.RFC3339),
			"last_opened_at": ws.LastOpenedAt.Format(time.RFC3339),
			"lifetime_sec":   ws.LifetimeSec,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"version":"goclaw.gateway.reply.v1","kind":"error","title":"OpenCode 工作区返回失败","text":%q}`, err.Error())
	}
	return string(raw)
}

func opencodeEncodedWorkspaceDir() string {
	return base64.RawURLEncoding.EncodeToString([]byte(defaultOpencodeWorkspaceDir))
}

func opencodeWorkspaceUIInit(ws *opencodeWorkspace) map[string]any {
	serverURL := originFromURL(ws.WebURL)
	state := map[string]any{
		"list": []map[string]any{
			{"type": "http", "http": map[string]any{"url": serverURL}},
		},
		"projects": map[string]any{
			serverURL: []map[string]any{{"worktree": defaultOpencodeWorkspaceDir, "expanded": true}},
		},
		"lastProject": map[string]any{
			serverURL: defaultOpencodeWorkspaceDir,
		},
	}
	stateJSON, _ := json.Marshal(state)
	return map[string]any{
		"default_server_storage_key": "opencode.settings.dat:defaultServerUrl",
		"server_state_storage_key":   "opencode.global.dat:server",
		"server_url":                 serverURL,
		"project_dir":                defaultOpencodeWorkspaceDir,
		"encoded_project_dir":        opencodeEncodedWorkspaceDir(),
		"session_id":                 ws.SessionID,
		"session_path":               "/" + opencodeEncodedWorkspaceDir() + "/session/" + ws.SessionID,
		"local_storage": map[string]string{
			"opencode.settings.dat:defaultServerUrl": serverURL,
			"opencode.global.dat:server":             string(stateJSON),
		},
		"server_state": state,
	}
}

func originFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func parseSandboxExpiresAt(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

var workspaceKeyRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeWorkspaceKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return defaultOpencodeWorkspaceKey
	}
	key = workspaceKeyRe.ReplaceAllString(key, "-")
	key = strings.Trim(key, "-._")
	if key == "" {
		return defaultOpencodeWorkspaceKey
	}
	if len(key) > 48 {
		key = key[:48]
	}
	return key
}

func newOpencodeWorkspaceSandboxSessionID(key string) string {
	safeKey := sanitizeWorkspaceKey(key)
	if len(safeKey) > 20 {
		safeKey = strings.Trim(safeKey[:20], "-._")
	}
	if safeKey == "" {
		safeKey = defaultOpencodeWorkspaceKey
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(id) > 12 {
		id = id[:12]
	}
	return "ocws-" + safeKey + "-" + id
}

func opencodeWorkspaceStepTimeout() time.Duration {
	sec := envOrInt("OPENCODE_WORKSPACE_STEP_TIMEOUT_SEC", defaultOpencodeWorkspaceStepSec)
	if sec <= 0 {
		sec = defaultOpencodeWorkspaceStepSec
	}
	return time.Duration(sec) * time.Second
}

func opencodeWorkspaceNativeTimeout(readyTimeout time.Duration) time.Duration {
	sec := envOrInt("OPENCODE_WORKSPACE_NATIVE_READY_TIMEOUT_SEC", defaultOpencodeWorkspaceNativeSec)
	if sec <= 0 {
		sec = defaultOpencodeWorkspaceNativeSec
	}
	timeout := time.Duration(sec) * time.Second
	if readyTimeout > 0 && readyTimeout < timeout {
		return readyTimeout
	}
	return timeout
}

func boolArg(v any, def bool) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	}
	return def
}
