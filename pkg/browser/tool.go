package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// BrowserTool implements tools.Tool for browser automation.
type BrowserTool struct {
	manager   *Manager
	storage   *StorageManager
	proxy     *ProxyManager
	extension *ExtensionManager
	audit     *AuditLogger
	sessions  store.ScreencastSessionStore
	publicURL string // base URL for shareable live view links (e.g. "https://goclaw.example.com")
}

// NewBrowserTool creates a BrowserTool wrapping a Manager and optional managers.
func NewBrowserTool(manager *Manager, storage *StorageManager, proxy *ProxyManager, extension *ExtensionManager, audit *AuditLogger) *BrowserTool {
	return &BrowserTool{
		manager:   manager,
		storage:   storage,
		proxy:     proxy,
		extension: extension,
		audit:     audit,
	}
}

func (t *BrowserTool) Name() string { return "browser" }

// SetProxyManager sets the proxy manager (wired after stores are initialized).
func (t *BrowserTool) SetProxyManager(pm *ProxyManager) { t.proxy = pm }

// SetExtensionManager sets the extension manager (wired after stores are initialized).
func (t *BrowserTool) SetExtensionManager(em *ExtensionManager) { t.extension = em }

// SetAuditLogger sets the audit logger (wired after stores are initialized).
func (t *BrowserTool) SetAuditLogger(al *AuditLogger) { t.audit = al }

// SetScreencastSessions sets the screencast session store (wired after stores are initialized).
func (t *BrowserTool) SetScreencastSessions(ss store.ScreencastSessionStore) { t.sessions = ss }

// SetPublicURL sets the base URL for shareable live view links.
func (t *BrowserTool) SetPublicURL(u string) { t.publicURL = u }

// SetManager replaces the underlying browser Manager (used for config hot-reload).
func (t *BrowserTool) SetManager(m *Manager) { t.manager = m }

// Manager returns the current browser Manager.
func (t *BrowserTool) Manager() *Manager { return t.manager }

func (t *BrowserTool) Description() string {
	return `Control a real Chrome/Chromium browser to navigate web pages, take accessibility snapshots, and interact with elements.
The browser runs as a real Chrome instance (headless or visible depending on server config). When headless is disabled, Chrome window appears on the server's display.

Actions:
- status: Get browser status (shows if running, headless mode, open tabs)
- start: Launch the browser engine
- stop: Close the browser engine and all tabs
- tabs: List open tabs
- open: Open a new tab (requires targetUrl; optional profile for Chrome profile dir)
- close: Close a tab (requires targetId)
- focusTab: Activate/focus a tab (requires targetId)
- snapshot: Get page accessibility tree with element refs (use targetId, maxChars, interactive, compact, depth, includeFrames, frameId)
- frames: List all frames/iframes in the page (use targetId)
- screenshot: Capture page screenshot (use targetId, fullPage)
- navigate: Navigate tab to URL (requires targetId, targetUrl)
- console: Get browser console messages (requires targetId)
- act: Interact with elements (requires request object with kind, ref, etc.)
- attach: Connect to existing browser (requires cdpUrl)
- getCookies: Get page cookies (use targetId)
- setCookie: Set a cookie (requires cookie object with name, value, domain, etc.)
- clearCookies: Clear all cookies (use targetId)
- getStorage: Get localStorage/sessionStorage items (use targetId, storageKind: "local"|"session")
- setStorage: Set storage item (requires storageKey, storageValue; use storageKind)
- clearStorage: Clear storage (use targetId, storageKind)
- profiles: List saved browser profiles
- deleteProfile: Delete a browser profile (requires profile name)
- errors: Get captured JavaScript exceptions (use targetId)
- emulate: Set device/viewport emulation (use targetId, userAgent, width, height, scale, isMobile, hasTouch, landscape)
- pdf: Generate PDF from page (use targetId, landscape)
- setHeaders: Set extra HTTP headers (requires headers object, use targetId)
- setOffline: Enable/disable offline mode (use targetId, offline)
- startScreencast: Start streaming JPEG frames (use targetId, fps, quality)
- stopScreencast: Stop screencast streaming (use targetId)
- extension.list: List registered extensions
- audit.list: List browser audit log entries (optional auditAction, auditLimit)
- storage.purge: Purge a browser profile session (requires profile)
- storage.cleanup: Remove old profiles (requires maxAge in hours)
- liveview.create: Create a shareable public link for a browser tab (requires targetId; optional mode: view/takeover). IMPORTANT: Only use this when the user explicitly asks to share or create a public link. Do NOT call this automatically when opening or navigating tabs — the user can already see the browser live in the chat panel without a token.

Act kinds: click, type, press, hover, wait, evaluate
- click: Click element (request: {kind:"click", ref:"e1"})
- type: Type text (request: {kind:"type", ref:"e1", text:"hello"})
- press: Press key (request: {kind:"press", key:"Enter"})
- hover: Hover element (request: {kind:"hover", ref:"e1"})
- wait: Wait for condition (request: {kind:"wait", timeMs:1000} or {kind:"wait", text:"loaded"})
- evaluate: Run JavaScript (request: {kind:"evaluate", fn:"document.title"})

Workflow: start → open URL → snapshot (get refs) → act (use refs) → snapshot again

Note: If proxy is configured for this agent, it is applied automatically. You do not need to configure or select a proxy — just use the browser normally.`
}

func (t *BrowserTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					"status", "start", "stop", "tabs", "open", "close", "focusTab",
					"snapshot", "screenshot", "navigate", "console", "act", "frames",
					"attach", "getCookies", "setCookie", "clearCookies",
					"getStorage", "setStorage", "clearStorage",
					"profiles", "deleteProfile", "errors",
					"emulate", "pdf", "setHeaders", "setOffline",
					"startScreencast", "stopScreencast",
					"extension.list",
					"audit.list",
					"storage.purge", "storage.cleanup",
					"liveview.create",
				},
				"description": "The browser action to perform",
			},
			"targetUrl": map[string]any{
				"type":        "string",
				"description": "URL for open/navigate actions",
			},
			"targetId": map[string]any{
				"type":        "string",
				"description": "Tab target ID (omit for current tab)",
			},
			"cdpUrl": map[string]any{
				"type":        "string",
				"description": "CDP WebSocket URL for attach action",
			},
			"profile": map[string]any{
				"type":        "string",
				"description": "Browser profile name for open/deleteProfile actions",
			},
			"maxChars": map[string]any{
				"type":        "number",
				"description": "Max characters for snapshot (default 8000)",
			},
			"interactive": map[string]any{
				"type":        "boolean",
				"description": "Only show interactive elements in snapshot",
			},
			"compact": map[string]any{
				"type":        "boolean",
				"description": "Remove empty structural elements from snapshot",
			},
			"depth": map[string]any{
				"type":        "number",
				"description": "Max depth for snapshot tree",
			},
			"includeFrames": map[string]any{
				"type":        "boolean",
				"description": "Include child iframes in snapshot (opt-in, default false)",
			},
			"frameId": map[string]any{
				"type":        "string",
				"description": "Snapshot a specific frame by frame ID (from frames action)",
			},
			"fullPage": map[string]any{
				"type":        "boolean",
				"description": "Capture full page screenshot",
			},
			"timeoutMs": map[string]any{
				"type":        "number",
				"description": "Timeout in milliseconds for actions",
			},
			"cookie": map[string]any{
				"type":        "object",
				"description": "Cookie object for setCookie (name, value, domain, path, secure, httpOnly, sameSite, expires, url)",
			},
			"storageKind": map[string]any{
				"type":        "string",
				"enum":        []string{"local", "session"},
				"description": "Storage type: 'local' (default) or 'session'",
			},
			"storageKey": map[string]any{
				"type":        "string",
				"description": "Key for setStorage action",
			},
			"storageValue": map[string]any{
				"type":        "string",
				"description": "Value for setStorage action",
			},
			"userAgent": map[string]any{
				"type":        "string",
				"description": "User agent string for emulate action",
			},
			"width": map[string]any{
				"type":        "number",
				"description": "Viewport width for emulate action",
			},
			"height": map[string]any{
				"type":        "number",
				"description": "Viewport height for emulate action",
			},
			"scale": map[string]any{
				"type":        "number",
				"description": "Device scale factor for emulate action (default 1)",
			},
			"isMobile": map[string]any{
				"type":        "boolean",
				"description": "Enable mobile emulation",
			},
			"hasTouch": map[string]any{
				"type":        "boolean",
				"description": "Enable touch emulation",
			},
			"landscape": map[string]any{
				"type":        "boolean",
				"description": "Landscape orientation for emulate/pdf actions",
			},
			"headers": map[string]any{
				"type":        "object",
				"description": "HTTP headers object for setHeaders action",
			},
			"offline": map[string]any{
				"type":        "boolean",
				"description": "Enable offline mode for setOffline action",
			},
			"fps": map[string]any{
				"type":        "number",
				"description": "Frames per second for startScreencast (default 10)",
			},
			"quality": map[string]any{
				"type":        "number",
				"description": "JPEG quality for startScreencast (default 80)",
			},
			"auditAction": map[string]any{
				"type":        "string",
				"description": "Filter audit log by action name",
			},
			"auditLimit": map[string]any{
				"type":        "number",
				"description": "Max entries for audit.list (default 50)",
			},
			"maxAge": map[string]any{
				"type":        "number",
				"description": "Max age in hours for storage.cleanup",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"view", "takeover"},
				"description": "LiveView mode: view (default) or takeover",
			},
			"expiresMinutes": map[string]any{
				"type":        "number",
				"description": "Token expiry in minutes for liveview.create (default 60, max 1440)",
			},
			"request": map[string]any{
				"type":        "object",
				"description": "Action request for 'act' command",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"click", "type", "press", "hover", "wait", "evaluate"},
						"description": "The interaction kind",
					},
					"ref": map[string]any{
						"type":        "string",
						"description": "Element ref from snapshot (e.g. e1, e2)",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "Text to type",
					},
					"key": map[string]any{
						"type":        "string",
						"description": "Key to press (e.g. Enter, Tab, Escape)",
					},
					"submit": map[string]any{
						"type":        "boolean",
						"description": "Press Enter after typing",
					},
					"fn": map[string]any{
						"type":        "string",
						"description": "JavaScript to evaluate",
					},
					"timeMs": map[string]any{
						"type":        "number",
						"description": "Wait time in milliseconds",
					},
				},
			},
		},
		"required": []string{"action"},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	action, _ := args["action"].(string)
	if action == "" {
		return tools.ErrorResult("action is required")
	}

	// Propagate tenant ID from store context to browser context for page isolation.
	if tid := store.TenantIDFromContext(ctx); tid.String() != "00000000-0000-0000-0000-000000000000" {
		ctx = WithTenantID(ctx, tid.String())
	}
	// Propagate agent key for per-agent page tracking.
	if ak := store.AgentKeyFromContext(ctx); ak != "" {
		ctx = WithAgentKey(ctx, ak)
	}
	// Propagate session key for per-session page isolation.
	if sk := tools.ToolSessionKeyFromCtx(ctx); sk != "" {
		ctx = WithSessionKey(ctx, sk)
	}

	// Propagate per-agent proxy opt-in to browser context (default false = no proxy).
	useProxy := tools.BrowserUseProxyFromCtx(ctx)
	if useProxy {
		ctx = WithUseProxy(ctx, true)
	}
	t.manager.logger.Info("browser tool execute",
		"action", action, "useProxy", useProxy,
		"engine", t.manager.engine.Name(),
		"proxyMgrWired", t.proxy != nil)

	// Auto-start browser for actions that need it
	switch action {
	case "open", "snapshot", "screenshot", "navigate", "act", "tabs", "frames",
		"getCookies", "setCookie", "clearCookies",
		"getStorage", "setStorage", "clearStorage",
		"errors", "focusTab",
		"emulate", "pdf", "setHeaders", "setOffline",
		"startScreencast", "stopScreencast":
		if err := t.manager.Start(ctx); err != nil {
			return tools.ErrorResult(fmt.Sprintf("failed to start browser: %v", err))
		}
	}

	// Apply per-action timeout for heavy operations
	switch action {
	case "open", "navigate", "snapshot", "screenshot", "act", "frames",
		"getCookies", "setCookie", "getStorage", "setStorage",
		"emulate", "pdf", "setHeaders":
		timeout := t.manager.ActionTimeout()
		if ms, ok := args["timeoutMs"].(float64); ok && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	targetID, _ := args["targetId"].(string)
	result := t.dispatch(ctx, action, args)

	// Backfill agent/session keys for pre-existing tabs that were opened before tracking was deployed.
	// This ensures the first interaction by an agent associates the mapping.
	if !result.IsError && targetID != "" {
		if ak := agentKeyFromCtx(ctx); ak != "" {
			t.manager.BackfillAgentKey(targetID, ak)
		}
		if sk := sessionKeyFromCtx(ctx); sk != "" {
			t.manager.BackfillSessionKey(targetID, sk)
		}
	}

	// Audit logging (fire-and-forget)
	if t.audit != nil {
		var resultErr error
		if result.IsError {
			if result.Err != nil {
				resultErr = result.Err
			} else {
				resultErr = fmt.Errorf("%s", result.ForLLM)
			}
		}
		tenantID := tenantIDFromCtx(ctx)
		t.audit.Log(ctx, tenantID, action, targetID, args, time.Since(start), resultErr)
	}

	return result
}

// dispatch routes the action to the appropriate handler.
func (t *BrowserTool) dispatch(ctx context.Context, action string, args map[string]any) *tools.Result {
	switch action {
	case "status":
		return t.handleStatus()
	case "start":
		return t.handleStart(ctx)
	case "stop":
		return t.handleStop(ctx)
	case "tabs":
		return t.handleTabs(ctx)
	case "open":
		return t.handleOpen(ctx, args)
	case "close":
		return t.handleClose(ctx, args)
	case "focusTab":
		return t.handleFocusTab(ctx, args)
	case "snapshot":
		return t.handleSnapshot(ctx, args)
	case "screenshot":
		return t.handleScreenshot(ctx, args)
	case "navigate":
		return t.handleNavigate(ctx, args)
	case "console":
		return t.handleConsole(ctx, args)
	case "act":
		return t.handleAct(ctx, args)
	case "frames":
		return t.handleFrames(ctx, args)
	case "attach":
		return t.handleAttach(ctx, args)
	case "getCookies":
		return t.handleGetCookies(ctx, args)
	case "setCookie":
		return t.handleSetCookie(ctx, args)
	case "clearCookies":
		return t.handleClearCookies(ctx, args)
	case "getStorage":
		return t.handleGetStorage(ctx, args)
	case "setStorage":
		return t.handleSetStorage(ctx, args)
	case "clearStorage":
		return t.handleClearStorage(ctx, args)
	case "profiles":
		return t.handleProfiles(ctx, args)
	case "deleteProfile":
		return t.handleDeleteProfile(ctx, args)
	case "errors":
		return t.handleErrors(ctx, args)
	case "emulate":
		return t.handleEmulate(ctx, args)
	case "pdf":
		return t.handlePDF(ctx, args)
	case "setHeaders":
		return t.handleSetHeaders(ctx, args)
	case "setOffline":
		return t.handleSetOffline(ctx, args)
	case "startScreencast":
		return t.handleStartScreencast(ctx, args)
	case "stopScreencast":
		return t.handleStopScreencast(ctx, args)
	case "proxy.list":
		return t.handleProxyList(ctx)
	case "proxy.add":
		return t.handleProxyAdd(ctx, args)
	case "proxy.remove":
		return t.handleProxyRemove(ctx, args)
	case "proxy.health":
		return t.handleProxyHealth(ctx)
	case "extension.list":
		return t.handleExtensionList(ctx)
	case "extension.add":
		return t.handleExtensionAdd(ctx, args)
	case "extension.remove":
		return t.handleExtensionRemove(ctx, args)
	case "audit.list":
		return t.handleAuditList(ctx, args)
	case "storage.purge":
		return t.handleStoragePurge(ctx, args)
	case "storage.cleanup":
		return t.handleStorageCleanup(ctx, args)
	case "liveview.create":
		return t.handleLiveViewCreate(ctx, args)
	default:
		return tools.ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *BrowserTool) handleStatus() *tools.Result {
	status := t.manager.Status()
	return jsonResult(status)
}

func (t *BrowserTool) handleStart(ctx context.Context) *tools.Result {
	if err := t.manager.Start(ctx); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to start browser: %v", err))
	}
	return tools.NewResult("Browser started successfully.")
}

func (t *BrowserTool) handleStop(ctx context.Context) *tools.Result {
	if err := t.manager.Stop(ctx); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to stop browser: %v", err))
	}
	return tools.NewResult("Browser stopped.")
}

func (t *BrowserTool) handleTabs(ctx context.Context) *tools.Result {
	tabs, err := t.manager.ListTabs(ctx)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	return jsonResult(tabs)
}

func (t *BrowserTool) handleOpen(ctx context.Context, args map[string]any) *tools.Result {
	url, _ := args["targetUrl"].(string)
	if url == "" {
		return tools.ErrorResult("targetUrl is required for open action")
	}
	if err := ValidateBrowserURL(url); err != nil {
		return tools.ErrorResult(fmt.Sprintf("open blocked: %v", err))
	}

	// Pass profile name via context so each open request routes to the correct container.
	// Also update activeProfile as default for subsequent requests without explicit profile.
	if profile, ok := args["profile"].(string); ok && profile != "" {
		ctx = WithProfileName(ctx, profile)
		t.manager.mu.Lock()
		t.manager.activeProfile = profile
		t.manager.mu.Unlock()
	}

	tab, err := t.manager.OpenTab(ctx, url)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	return jsonResult(tab)
}

func (t *BrowserTool) handleClose(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	if err := t.manager.CloseTab(ctx, targetID); err != nil {
		return tools.ErrorResult(err.Error())
	}
	return tools.NewResult("Tab closed.")
}

func (t *BrowserTool) handleSnapshot(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	opts := DefaultSnapshotOptions()

	if mc, ok := args["maxChars"].(float64); ok {
		opts.MaxChars = int(mc)
	}
	if inter, ok := args["interactive"].(bool); ok {
		opts.Interactive = inter
	}
	if comp, ok := args["compact"].(bool); ok {
		opts.Compact = comp
	}
	if d, ok := args["depth"].(float64); ok {
		opts.MaxDepth = int(d)
	}
	if incFr, ok := args["includeFrames"].(bool); ok {
		opts.IncludeFrames = incFr
	}
	if fid, ok := args["frameId"].(string); ok {
		opts.FrameID = fid
	}

	snap, err := t.manager.Snapshot(ctx, targetID, opts)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("snapshot failed: %v", err))
	}

	// Return snapshot text directly (optimized for LLM consumption)
	header := fmt.Sprintf("Page: %s\nURL: %s\nTargetID: %s\nStats: %d refs, %d interactive\n\n",
		snap.Title, snap.URL, snap.TargetID, snap.Stats.Refs, snap.Stats.Interactive)
	return tools.NewResult(header + snap.Snapshot)
}

func (t *BrowserTool) handleScreenshot(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	fullPage, _ := args["fullPage"].(bool)

	data, err := t.manager.Screenshot(ctx, targetID, fullPage)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("screenshot failed: %v", err))
	}

	// Save to workspace/screenshots/ so the agent can access the file.
	// Falls back to os.TempDir() if workspace is not available.
	screenshotDir := filepath.Join(os.TempDir(), "goclaw_screenshots")
	if ws := tools.ToolWorkspaceFromCtx(ctx); ws != "" {
		screenshotDir = filepath.Join(ws, "screenshots")
	}
	if err := os.MkdirAll(screenshotDir, 0755); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to create screenshots directory: %v", err))
	}
	imagePath := filepath.Join(screenshotDir, fmt.Sprintf("screenshot_%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(imagePath, data, 0644); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to save screenshot: %v", err))
	}

	return &tools.Result{ForLLM: fmt.Sprintf("MEDIA:%s", imagePath)}
}

func (t *BrowserTool) handleNavigate(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	url, _ := args["targetUrl"].(string)
	if url == "" {
		return tools.ErrorResult("targetUrl is required for navigate action")
	}
	if err := ValidateBrowserURL(url); err != nil {
		return tools.ErrorResult(fmt.Sprintf("navigate blocked: %v", err))
	}

	if err := t.manager.Navigate(ctx, targetID, url); err != nil {
		return tools.ErrorResult(err.Error())
	}
	return tools.NewResult(fmt.Sprintf("Navigated to %s", url))
}

func (t *BrowserTool) handleConsole(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	msgs := t.manager.ConsoleMessages(ctx, targetID)
	return jsonResult(msgs)
}

func (t *BrowserTool) handleAct(ctx context.Context, args map[string]any) *tools.Result {
	req, ok := args["request"].(map[string]any)
	if !ok {
		return tools.ErrorResult("request object is required for act action")
	}

	kind, _ := req["kind"].(string)
	if kind == "" {
		return tools.ErrorResult("request.kind is required")
	}

	targetID, _ := args["targetId"].(string)

	switch kind {
	case "click":
		ref, _ := req["ref"].(string)
		if ref == "" {
			return tools.ErrorResult("request.ref is required for click")
		}
		opts := ClickOpts{}
		if dc, ok := req["doubleClick"].(bool); ok {
			opts.DoubleClick = dc
		}
		if btn, ok := req["button"].(string); ok {
			opts.Button = btn
		}
		if err := t.manager.Click(ctx, targetID, ref, opts); err != nil {
			return tools.ErrorResult(fmt.Sprintf("click failed: %v", err))
		}
		return tools.NewResult("Clicked successfully.")

	case "type":
		ref, _ := req["ref"].(string)
		if ref == "" {
			return tools.ErrorResult("request.ref is required for type")
		}
		text, _ := req["text"].(string)
		opts := TypeOpts{}
		if sub, ok := req["submit"].(bool); ok {
			opts.Submit = sub
		}
		if sl, ok := req["slowly"].(bool); ok {
			opts.Slowly = sl
		}
		if err := t.manager.Type(ctx, targetID, ref, text, opts); err != nil {
			return tools.ErrorResult(fmt.Sprintf("type failed: %v", err))
		}
		return tools.NewResult("Typed successfully.")

	case "press":
		key, _ := req["key"].(string)
		if key == "" {
			return tools.ErrorResult("request.key is required for press")
		}
		if err := t.manager.Press(ctx, targetID, key); err != nil {
			return tools.ErrorResult(fmt.Sprintf("press failed: %v", err))
		}
		return tools.NewResult(fmt.Sprintf("Pressed %s.", key))

	case "hover":
		ref, _ := req["ref"].(string)
		if ref == "" {
			return tools.ErrorResult("request.ref is required for hover")
		}
		if err := t.manager.Hover(ctx, targetID, ref); err != nil {
			return tools.ErrorResult(fmt.Sprintf("hover failed: %v", err))
		}
		return tools.NewResult("Hovered successfully.")

	case "wait":
		opts := WaitOpts{}
		if ms, ok := req["timeMs"].(float64); ok {
			opts.TimeMs = int(ms)
		}
		if txt, ok := req["text"].(string); ok {
			opts.Text = txt
		}
		if tg, ok := req["textGone"].(string); ok {
			opts.TextGone = tg
		}
		if u, ok := req["url"].(string); ok {
			opts.URL = u
		}
		if fn, ok := req["fn"].(string); ok {
			opts.Fn = fn
		}
		if err := t.manager.Wait(ctx, targetID, opts); err != nil {
			return tools.ErrorResult(fmt.Sprintf("wait failed: %v", err))
		}
		return tools.NewResult("Wait condition met.")

	case "evaluate":
		fn, _ := req["fn"].(string)
		if fn == "" {
			return tools.ErrorResult("request.fn is required for evaluate")
		}
		result, err := t.manager.Evaluate(ctx, targetID, fn)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("evaluate failed: %v", err))
		}
		return tools.NewResult(result)

	default:
		return tools.ErrorResult(fmt.Sprintf("unknown act kind: %s", kind))
	}
}

func (t *BrowserTool) handleFrames(ctx context.Context, args map[string]any) *tools.Result {
	targetID, _ := args["targetId"].(string)
	frames, err := t.manager.ListFrames(ctx, targetID)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("list frames failed: %v", err))
	}
	if len(frames) == 0 {
		return tools.NewResult("No frames found.")
	}
	return jsonResult(frames)
}

func jsonResult(v any) *tools.Result {
	data, _ := json.MarshalIndent(v, "", "  ")
	return tools.NewResult(string(data))
}
