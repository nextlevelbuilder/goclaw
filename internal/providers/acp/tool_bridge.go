package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// ToolBridge handles agent→client requests (fs, terminal, permission).
// It enforces workspace sandboxing and shell deny patterns.
type ToolBridge struct {
	workspace      string
	terminals      sync.Map // string → *Terminal
	denyPatterns   []*regexp.Regexp
	permMode       string // "approve-all" (default), "approve-reads", "deny-all"
	nextTermID     atomic.Int64
	maxOutputBytes int
}

// ToolBridgeOption configures a ToolBridge.
type ToolBridgeOption func(*ToolBridge)

// WithDenyPatterns sets shell deny patterns.
func WithDenyPatterns(patterns []*regexp.Regexp) ToolBridgeOption {
	return func(tb *ToolBridge) { tb.denyPatterns = patterns }
}

// WithPermMode sets the permission handling mode.
func WithPermMode(mode string) ToolBridgeOption {
	return func(tb *ToolBridge) {
		if mode != "" {
			tb.permMode = mode
		}
	}
}

// NewToolBridge creates a tool bridge sandboxed to the given workspace.
func NewToolBridge(workspace string, opts ...ToolBridgeOption) *ToolBridge {
	tb := &ToolBridge{
		workspace:      workspace,
		permMode:       "approve-all",
		maxOutputBytes: 10 * 1024 * 1024, // 10MB
	}
	for _, opt := range opts {
		opt(tb)
	}
	return tb
}

// Handle dispatches agent→client requests by method name.
// Implements the RequestHandler signature for Conn.
func (tb *ToolBridge) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	// Method names: ACP spec uses snake_case (fs/read_text_file,
	// session/request_permission, etc). The original camelCase aliases
	// (fs/readTextFile, permission/request) are retained for Claude CLI
	// backward compatibility.
	session := goclawSessionFromCtx(ctx)
	switch method {
	case "fs/read_text_file", "fs/readTextFile":
		if tb.permMode == "deny-all" {
			slog.Warn("security.tool_denied", "session", session, "tool", method, "reason", "deny-all")
			return nil, fmt.Errorf("read denied by permission mode: %s", tb.permMode)
		}
		var req ReadTextFileRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		result, err := tb.readFile(req)
		if err == nil {
			slog.Info("security.tool_granted", "session", session, "tool", method, "path", req.Path)
		}
		return result, err
	case "fs/write_text_file", "fs/writeTextFile":
		if tb.permMode == "deny-all" || tb.permMode == "approve-reads" {
			slog.Warn("security.tool_denied", "session", session, "tool", method, "reason", tb.permMode)
			return nil, fmt.Errorf("write denied by permission mode: %s", tb.permMode)
		}
		var req WriteTextFileRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		result, err := tb.writeFile(req)
		if err == nil {
			slog.Info("security.tool_granted", "session", session, "tool", method, "path", req.Path)
		}
		return result, err
	case "terminal/create":
		if tb.permMode == "deny-all" || tb.permMode == "approve-reads" {
			slog.Warn("security.tool_denied", "session", session, "tool", method, "reason", tb.permMode)
			return nil, fmt.Errorf("terminal denied by permission mode: %s", tb.permMode)
		}
		var req CreateTerminalRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		result, err := tb.createTerminal(req)
		if err == nil {
			slog.Info("security.tool_granted", "session", session, "tool", method, "command", req.Command)
		}
		return result, err
	case "terminal/output":
		var req TerminalOutputRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.terminalOutput(req)
	case "terminal/release":
		var req ReleaseTerminalRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.releaseTerminal(req)
	case "terminal/wait_for_exit", "terminal/waitForExit":
		var req WaitForTerminalExitRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.waitForExit(ctx, req)
	case "terminal/kill":
		if tb.permMode == "deny-all" {
			slog.Warn("security.tool_denied", "session", session, "tool", method, "reason", "deny-all")
			return nil, fmt.Errorf("terminal kill denied by permission mode: %s", tb.permMode)
		}
		var req KillTerminalRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.killTerminal(req)
	case "permission/request":
		var req RequestPermissionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.handlePermission(ctx, req)
	case "session/request_permission":
		var req SessionRequestPermissionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.handleSessionPermission(ctx, req)
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

// readFile reads a file validated against the workspace boundary.
func (tb *ToolBridge) readFile(req ReadTextFileRequest) (*ReadTextFileResponse, error) {
	resolved, err := tb.resolvePath(req.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}
	return &ReadTextFileResponse{Content: string(data)}, nil
}

// writeFile writes a file validated against the workspace boundary.
func (tb *ToolBridge) writeFile(req WriteTextFileRequest) (*WriteTextFileResponse, error) {
	resolved, err := tb.resolvePath(req.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return nil, fmt.Errorf("mkdir failed: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(req.Content), 0644); err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}
	return &WriteTextFileResponse{}, nil
}

// handlePermission responds to permission requests based on configured mode.
func (tb *ToolBridge) handlePermission(ctx context.Context, req RequestPermissionRequest) (*RequestPermissionResponse, error) {
	session := goclawSessionFromCtx(ctx)
	switch tb.permMode {
	case "deny-all":
		slog.Warn("security.tool_denied", "session", session, "tool", req.ToolName, "reason", "deny-all")
		return &RequestPermissionResponse{Outcome: "denied"}, nil
	case "approve-reads":
		lower := strings.ToLower(req.ToolName)
		if strings.Contains(lower, "read") || strings.Contains(lower, "glob") ||
			strings.Contains(lower, "grep") || strings.Contains(lower, "search") ||
			strings.Contains(lower, "list") || strings.Contains(lower, "view") {
			slog.Info("security.tool_granted", "session", session, "tool", req.ToolName, "mode", "approve-reads")
			return &RequestPermissionResponse{Outcome: "approved"}, nil
		}
		slog.Warn("security.tool_denied", "session", session, "tool", req.ToolName, "reason", "approve-reads:write-blocked")
		return &RequestPermissionResponse{Outcome: "denied"}, nil
	default: // "approve-all" or unknown → approve
		slog.Info("security.tool_granted", "session", session, "tool", req.ToolName, "mode", "approve-all")
		return &RequestPermissionResponse{Outcome: "approved"}, nil
	}
}

// handleSessionPermission handles the ACP-spec session/request_permission RPC.
// Selection is by Kind (allow_once / allow_always / reject_once / reject_always)
// rather than by OptionID strings, since the spec leaves OptionID as an
// agent-defined identifier with no guaranteed values.
func (tb *ToolBridge) handleSessionPermission(ctx context.Context, req SessionRequestPermissionRequest) (*SessionRequestPermissionResponse, error) {
	session := goclawSessionFromCtx(ctx)

	pickByKind := func(kinds ...string) string {
		for _, want := range kinds {
			for _, opt := range req.Options {
				if opt.Kind == want {
					return opt.OptionID
				}
			}
		}
		return ""
	}
	cancelled := &SessionRequestPermissionResponse{Outcome: SessionPermOutcome{Outcome: "cancelled"}}

	switch tb.permMode {
	case "deny-all":
		slog.Warn("security.tool_denied", "session", session, "tool", req.ToolCall.Title, "reason", "deny-all")
		return cancelled, nil
	case "approve-reads":
		lower := strings.ToLower(req.ToolCall.Title)
		if strings.Contains(lower, "read") || strings.Contains(lower, "glob") ||
			strings.Contains(lower, "grep") || strings.Contains(lower, "search") ||
			strings.Contains(lower, "list") || strings.Contains(lower, "view") {
			id := pickByKind("allow_once", "allow_always")
			if id == "" {
				return cancelled, nil
			}
			slog.Info("security.tool_granted", "session", session, "tool", req.ToolCall.Title, "mode", "approve-reads", "optionId", id)
			return &SessionRequestPermissionResponse{Outcome: SessionPermOutcome{Outcome: "selected", OptionID: id}}, nil
		}
		slog.Warn("security.tool_denied", "session", session, "tool", req.ToolCall.Title, "reason", "approve-reads:write-blocked")
		return cancelled, nil
	default: // "approve-all"
		// Prefer allow_once over allow_always so we don't blanket-trust
		// across the session. Fall back to allow_always if only that is offered.
		id := pickByKind("allow_once", "allow_always")
		if id == "" {
			slog.Warn("security.tool_denied", "session", session, "tool", req.ToolCall.Title, "reason", "no allow option offered")
			return cancelled, nil
		}
		slog.Info("security.tool_granted", "session", session, "tool", req.ToolCall.Title, "mode", "approve-all", "optionId", id)
		return &SessionRequestPermissionResponse{Outcome: SessionPermOutcome{Outcome: "selected", OptionID: id}}, nil
	}
}

// resolvePath validates that a path stays within the workspace boundary.
func (tb *ToolBridge) resolvePath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(tb.workspace, cleaned)
	}
	// Resolve symlinks for the target (may not exist yet for writes)
	real, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		real = cleaned // file may not exist yet — validate parent
	}
	wsReal, _ := filepath.EvalSymlinks(tb.workspace)
	if wsReal == "" {
		wsReal = tb.workspace
	}
	if real != wsReal && !strings.HasPrefix(real, wsReal+string(filepath.Separator)) {
		slog.Warn("security.acp_path_escape", "path", path, "resolved", real, "workspace", wsReal)
		return "", fmt.Errorf("access denied: path outside workspace")
	}
	return real, nil
}

// Close kills all active terminals.
func (tb *ToolBridge) Close() error {
	tb.terminals.Range(func(key, value any) bool {
		t := value.(*Terminal)
		t.cancel()
		tb.terminals.Delete(key)
		return true
	})
	return nil
}
