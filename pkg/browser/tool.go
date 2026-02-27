package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// BrowserTool implements tools.Tool for browser automation.
type BrowserTool struct {
	manager *Manager
}

// NewBrowserTool creates a BrowserTool wrapping a Manager.
func NewBrowserTool(manager *Manager) *BrowserTool {
	return &BrowserTool{manager: manager}
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

Act kinds: click, type, press, hover, wait, evaluate
- click: Click element (request: {kind:"click", ref:"e1"})
- type: Type text (request: {kind:"type", ref:"e1", text:"hello"})
- press: Press key (request: {kind:"press", key:"Enter"})
- hover: Hover element (request: {kind:"hover", ref:"e1"})
- wait: Wait for condition (request: {kind:"wait", timeMs:1000} or {kind:"wait", text:"loaded"})
- evaluate: Run JavaScript (request: {kind:"evaluate", fn:"document.title"})

Workflow: start → open URL → snapshot (get refs) → act (use refs) → snapshot again`
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"status", "start", "stop", "tabs", "open", "close", "snapshot", "screenshot", "navigate", "console", "act"},
				"description": "The browser action to perform",
			},
			"targetUrl": map[string]interface{}{
				"type":        "string",
				"description": "URL for open/navigate actions",
			},
			"targetId": map[string]interface{}{
				"type":        "string",
				"description": "Tab target ID (omit for current tab)",
			},
			"maxChars": map[string]interface{}{
				"type":        "number",
				"description": "Max characters for snapshot (default 8000)",
			},
			"interactive": map[string]interface{}{
				"type":        "boolean",
				"description": "Only show interactive elements in snapshot",
			},
			"compact": map[string]interface{}{
				"type":        "boolean",
				"description": "Remove empty structural elements from snapshot",
			},
			"depth": map[string]interface{}{
				"type":        "number",
				"description": "Max depth for snapshot tree",
			},
			"fullPage": map[string]interface{}{
				"type":        "boolean",
				"description": "Capture full page screenshot",
			},
			"timeoutMs": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in milliseconds for actions",
			},
			"request": map[string]interface{}{
				"type":        "object",
				"description": "Action request for 'act' command",
				"properties": map[string]interface{}{
					"kind": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"click", "type", "press", "hover", "wait", "evaluate"},
						"description": "The interaction kind",
					},
					"ref": map[string]interface{}{
						"type":        "string",
						"description": "Element ref from snapshot (e.g. e1, e2)",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to type",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Key to press (e.g. Enter, Tab, Escape)",
					},
					"submit": map[string]interface{}{
						"type":        "boolean",
						"description": "Press Enter after typing",
					},
					"fn": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript to evaluate",
					},
					"timeMs": map[string]interface{}{
						"type":        "number",
						"description": "Wait time in milliseconds",
					},
				},
			},
		},
		"required": []string{"action"},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) *tools.Result {
	action, _ := args["action"].(string)
	if action == "" {
		return tools.ErrorResult("action is required")
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
		return t.handleConsole(args)
	case "act":
		return t.handleAct(ctx, args)
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

func (t *BrowserTool) handleOpen(ctx context.Context, args map[string]interface{}) *tools.Result {
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

func (t *BrowserTool) handleClose(ctx context.Context, args map[string]interface{}) *tools.Result {
	targetID, _ := args["targetId"].(string)
	if err := t.manager.CloseTab(ctx, targetID); err != nil {
		return tools.ErrorResult(err.Error())
	}
	return tools.NewResult("Tab closed.")
}

func (t *BrowserTool) handleSnapshot(ctx context.Context, args map[string]interface{}) *tools.Result {
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

func (t *BrowserTool) handleScreenshot(ctx context.Context, args map[string]interface{}) *tools.Result {
	targetID, _ := args["targetId"].(string)
	fullPage, _ := args["fullPage"].(bool)

	data, err := t.manager.Screenshot(ctx, targetID, fullPage)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("screenshot failed: %v", err))
	}

	// Save to temp file so the media pipeline can deliver it (e.g. Telegram sendPhoto)
	imagePath := filepath.Join(os.TempDir(), fmt.Sprintf("goclaw_screenshot_%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(imagePath, data, 0644); err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to save screenshot: %v", err))
	}

	return &tools.Result{ForLLM: fmt.Sprintf("MEDIA:%s", imagePath)}
}

func (t *BrowserTool) handleNavigate(ctx context.Context, args map[string]interface{}) *tools.Result {
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

func (t *BrowserTool) handleConsole(args map[string]interface{}) *tools.Result {
	targetID, _ := args["targetId"].(string)
	msgs := t.manager.ConsoleMessages(targetID)
	return jsonResult(msgs)
}

func (t *BrowserTool) handleAct(ctx context.Context, args map[string]interface{}) *tools.Result {
	req, ok := args["request"].(map[string]interface{})
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

func jsonResult(v interface{}) *tools.Result {
	data, _ := json.MarshalIndent(v, "", "  ")
	return tools.NewResult(string(data))
}
