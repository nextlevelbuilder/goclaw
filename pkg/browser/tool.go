package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/browsertakeover"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// BrowserTool implements tools.Tool for browser automation.
type BrowserTool struct {
	manager  *Manager
	takeover *browsertakeover.Manager
}

// NewBrowserTool creates a BrowserTool wrapping a Manager.
func NewBrowserTool(manager *Manager) *BrowserTool {
	return &BrowserTool{manager: manager, takeover: browsertakeover.Default()}
}

func (t *BrowserTool) Name() string { return "browser" }

func (t *BrowserTool) Description() string {
	return `Control a browser to navigate web pages, take accessibility snapshots, and interact with elements.

Actions:
- status: Get browser status
- start: Launch browser
- stop: Close browser
- tabs: List open tabs
- open: Open a new tab (requires targetUrl)
- close: Close a tab (requires targetId)
- snapshot: Get page accessibility tree with element refs (use targetId, maxChars, interactive, compact, depth)
- screenshot: Capture page screenshot (use targetId, fullPage)
- navigate: Navigate tab to URL (requires targetId, targetUrl)
- console: Get browser console messages (requires targetId)
- act: Interact with elements (requires request object with kind, ref, etc.)
- takeover: Hand a tab to the user for a step only a person can do (SSO, CAPTCHA, QR, OTP, passkey). Use targetId and reason (say what they must do, never include secrets). Blocks until they finish; then snapshot to continue. Input values stay hidden afterwards.

Act kinds: click, type, press, hover, wait, evaluate, fill_secret, capture_secret
- click: Click element (request: {kind:"click", ref:"e1"})
- type: Type text (request: {kind:"type", ref:"e1", text:"hello"})
- press: Press key (request: {kind:"press", key:"Enter"})
- hover: Hover element (request: {kind:"hover", ref:"e1"})
- wait: Wait for condition (request: {kind:"wait", timeMs:1000} or {kind:"wait", text:"loaded"})
- evaluate: Run JavaScript (request: {kind:"evaluate", fn:"document.title"})
- fill_secret: Fill a vault secret into an input of the page (request: {kind:"fill_secret", ref:"e1", secret:"my-login", field:"username"|"password", submit:false}).
  ALWAYS use this for passwords and API keys; never use type for them and never write the value. The secret must be allowed for the page's domain.
  While a secret is on the page its fields are masked in snapshots, and evaluate, wait with fn, console and screenshot are disabled.
- capture_secret: Save a token the page holds straight into the vault without seeing it (request: {kind:"capture_secret", source:"localStorage"|"sessionStorage"|"cookie", key:"access_token", secret_name:"app-token"}).
  ALWAYS use this instead of reading a token with evaluate. The value never reaches you; you get only a status and the placeholder {{secret:name}}.
  The secret is bound to the page's host and stays pending until the user approves it. Only works when writing secrets is enabled for this agent; the name must be new.

Workflow: start → open URL → snapshot (get refs) → act (use refs) → snapshot again`
}

func (t *BrowserTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "start", "stop", "tabs", "open", "close", "snapshot", "screenshot", "navigate", "console", "act", "takeover"},
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
			"fullPage": map[string]any{
				"type":        "boolean",
				"description": "Capture full page screenshot",
			},
			"timeoutMs": map[string]any{
				"type":        "number",
				"description": "Timeout in milliseconds for actions",
			},
			"request": map[string]any{
				"type":        "object",
				"description": "Action request for 'act' command",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"click", "type", "press", "hover", "wait", "evaluate", "fill_secret", "capture_secret"},
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
						"description": "Key to press (e.g. Enter, Tab, Escape); for capture_secret the storage key or cookie name",
					},
					"secret": map[string]any{
						"type":        "string",
						"description": "Vault secret name for fill_secret (plain name, not a placeholder)",
					},
					"field": map[string]any{
						"type":        "string",
						"enum":        []string{"username", "password"},
						"description": "Which part of the secret fill_secret types (default password)",
					},
					"source": map[string]any{
						"type":        "string",
						"enum":        []string{captureLocalStorage, captureSessionStorage, captureCookie},
						"description": "Where capture_secret reads the value",
					},
					"secret_name": map[string]any{
						"type":        "string",
						"description": "Name of the new vault secret for capture_secret: 1-64 lowercase letters, digits, '-' or '_'",
					},
					"secret_kind": map[string]any{
						"type":        "string",
						"enum":        []string{store.SecretKindAPIKey},
						"description": "Kind of the captured secret (only api_key)",
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

// Execute runs a browser action. Every result passes through the run's secret
// redactor, so page text or an error string can never carry a vault value back
// to the model.
func (t *BrowserTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	res := t.execute(ctx, args)
	if rt := secretvault.RuntimeFrom(ctx); rt != nil && res != nil {
		res.ForLLM = rt.Redactor.Redact(res.ForLLM)
		res.ForUser = rt.Redactor.Redact(res.ForUser)
		res.Deliverable = rt.Redactor.Redact(res.Deliverable)
	}
	return res
}

// secretGuardOp maps a request to the operation name the secret-tab guard
// understands; "" means the request does not touch a page.
func secretGuardOp(action string, args map[string]any) string {
	switch action {
	case "snapshot", "navigate", "close", "screenshot", "console":
		return action
	case "act":
		req, _ := args["request"].(map[string]any)
		kind, _ := req["kind"].(string)
		if fn, _ := req["fn"].(string); kind == "wait" && fn != "" {
			return "wait_fn"
		}
		return kind
	}
	return ""
}

func (t *BrowserTool) execute(ctx context.Context, args map[string]any) *tools.Result {
	action, _ := args["action"].(string)
	if action == "" {
		return tools.ErrorResult("action is required")
	}

	// Propagate tenant ID from store context to browser context for page isolation.
	scope := BrowserScope{}
	if tid := store.TenantIDFromContext(ctx); tid.String() != "00000000-0000-0000-0000-000000000000" {
		scope.TenantID = tid.String()
	}
	scope.UserID = store.CredentialUserIDFromContext(ctx)
	if agentID := store.AgentIDFromContext(ctx); agentID.String() != "00000000-0000-0000-0000-000000000000" {
		scope.AgentID = agentID.String()
	} else {
		scope.AgentID = store.AgentKeyFromContext(ctx)
	}
	ctx = WithScope(ctx, scope)

	if op := secretGuardOp(action, args); op != "" {
		runSession := ""
		if rt := secretvault.RuntimeFrom(ctx); rt != nil {
			runSession = rt.SessionKey
		}
		targetID, _ := args["targetId"].(string)
		if err := t.manager.guardSecretTab(ctx, targetID, op, runSession); err != nil {
			return tools.ErrorResult(err.Error())
		}
	}

	// Apply per-action timeout before startup so remote Chrome failures are bounded too.
	switch action {
	case "open", "navigate", "snapshot", "screenshot", "act", "tabs":
		timeout := t.manager.ActionTimeout()
		if ms, ok := args["timeoutMs"].(float64); ok && ms > 0 {
			timeout = time.Duration(ms) * time.Millisecond
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Auto-start browser for actions that need it
	switch action {
	case "open", "snapshot", "screenshot", "navigate", "act", "tabs":
		if err := t.manager.Start(ctx); err != nil {
			return tools.ErrorResult(fmt.Sprintf("failed to start browser: %v", err))
		}
	}

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
	case "takeover":
		return t.handleTakeover(ctx, args)
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
	if t.manager.Backend() == BackendLightpanda {
		return tools.ErrorResult("screenshot is not supported on the lightpanda backend (returns a placeholder image); use the 'snapshot' action for an accessibility-tree view of the page")
	}

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

	case "fill_secret":
		ref, _ := req["ref"].(string)
		name, _ := req["secret"].(string)
		if ref == "" || name == "" {
			return tools.ErrorResult("request.ref and request.secret are required for fill_secret")
		}
		opts := FillSecretOpts{Secret: name}
		opts.Field, _ = req["field"].(string)
		opts.Submit, _ = req["submit"].(bool)
		if err := t.manager.FillSecret(ctx, targetID, ref, opts); err != nil {
			return tools.ErrorResult(fmt.Sprintf("fill_secret failed: %v", err))
		}
		return tools.NewResult("Secret filled. Its value is hidden from snapshots; do not try to read it back.")

	case "capture_secret":
		opts := CaptureSecretOpts{}
		opts.Source, _ = req["source"].(string)
		opts.Key, _ = req["key"].(string)
		opts.SecretName, _ = req["secret_name"].(string)
		opts.SecretKind, _ = req["secret_kind"].(string)
		status, err := t.manager.CaptureSecret(ctx, targetID, opts)
		if errors.Is(err, secretvault.ErrWriteDisabled) {
			return tools.ErrorResult("capture_secret failed: writing secrets is not enabled for this agent. Ask the user to enable it in the Secret Vault page.")
		}
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("capture_secret failed: %v", err))
		}
		return tools.NewResult(captureResultText(opts.SecretName, status))

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

// captureResultText is the whole capture_secret result: status and placeholder.
func captureResultText(name, status string) string {
	ph := "{{secret:" + name + "}}"
	if status == secretvault.WriteActive {
		return fmt.Sprintf("Secret saved and approved by the user (status: active). Use the placeholder %s.", ph)
	}
	return fmt.Sprintf("Secret saved (status: pending). It cannot be used until the user approves it in the Secret Vault page; then use the placeholder %s.", ph)
}

func jsonResult(v any) *tools.Result {
	data, _ := json.MarshalIndent(v, "", "  ")
	return tools.NewResult(string(data))
}
