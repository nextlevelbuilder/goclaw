package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	switch method {
	case "fs/readTextFile":
		if tb.permMode == "deny-all" {
			return nil, fmt.Errorf("read denied by permission mode: %s", tb.permMode)
		}
		var req ReadTextFileRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.readFile(req)
	case "fs/writeTextFile":
		if tb.permMode == "deny-all" || tb.permMode == "approve-reads" {
			return nil, fmt.Errorf("write denied by permission mode: %s", tb.permMode)
		}
		var req WriteTextFileRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.writeFile(req)
	case "terminal/create":
		if tb.permMode == "deny-all" || tb.permMode == "approve-reads" {
			return nil, fmt.Errorf("terminal denied by permission mode: %s", tb.permMode)
		}
		var req CreateTerminalRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.createTerminal(req)
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
	case "terminal/waitForExit":
		var req WaitForTerminalExitRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
		return tb.waitForExit(ctx, req)
	case "terminal/kill":
		if tb.permMode == "deny-all" {
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
		return tb.handlePermission(req)
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
func (tb *ToolBridge) handlePermission(req RequestPermissionRequest) (*RequestPermissionResponse, error) {
	switch tb.permMode {
	case "deny-all":
		return &RequestPermissionResponse{Outcome: "denied"}, nil
	case "approve-reads":
		// Approve read-only tools, deny write/exec tools
		lower := strings.ToLower(req.ToolName)
		if strings.Contains(lower, "read") || strings.Contains(lower, "glob") ||
			strings.Contains(lower, "grep") || strings.Contains(lower, "search") ||
			strings.Contains(lower, "list") || strings.Contains(lower, "view") {
			return &RequestPermissionResponse{Outcome: "approved"}, nil
		}
		return &RequestPermissionResponse{Outcome: "denied"}, nil
	default: // "approve-all" or unknown → approve
		return &RequestPermissionResponse{Outcome: "approved"}, nil
	}
}

// resolvePath validates that a path stays within the workspace boundary.
//
// The returned path is canonical: every symlink in it has been resolved,
// including dangling symlinks and symlinked ancestors of not-yet-existing
// files. Falling back to the lexical path is unsafe because os.MkdirAll and
// os.WriteFile follow symlinks, so a dangling link inside the workspace could
// otherwise redirect a write outside it (#1201).
func (tb *ToolBridge) resolvePath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(tb.workspace, cleaned)
	}
	wsReal, err := filepath.EvalSymlinks(tb.workspace)
	if err != nil || wsReal == "" {
		wsReal = filepath.Clean(tb.workspace)
	}
	real, err := resolveCanonicalPath(cleaned, 0)
	if err != nil {
		slog.Warn("security.acp_path_resolve_failed", "path", path, "error", err)
		return "", fmt.Errorf("access denied: cannot resolve path")
	}
	if real != wsReal && !strings.HasPrefix(real, wsReal+string(filepath.Separator)) {
		slog.Warn("security.acp_path_escape", "path", path, "resolved", real, "workspace", wsReal)
		return "", fmt.Errorf("access denied: path outside workspace")
	}
	return real, nil
}

// maxSymlinkHops bounds dangling-symlink resolution (mirrors the kernel's
// ELOOP limit) so symlink cycles cannot recurse forever.
const maxSymlinkHops = 40

// resolveCanonicalPath returns the fully symlink-resolved form of an absolute,
// cleaned path whose trailing components may not exist yet.
//   - existing path: EvalSymlinks result
//   - path is a dangling symlink: resolve its target (recursively)
//   - otherwise: canonicalize the parent and append the base name
//
// Any error other than "does not exist" (permission denied, ELOOP, ...) is
// returned so the caller rejects the path instead of trusting it lexically.
func resolveCanonicalPath(p string, hops int) (string, error) {
	if hops > maxSymlinkHops {
		return "", fmt.Errorf("too many levels of symbolic links: %s", p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	if info, lerr := os.Lstat(p); lerr == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			// Exists but EvalSymlinks reported not-exist: an ancestor is
			// unresolvable. Never fall back to the lexical path.
			return "", err
		}
		target, rerr := os.Readlink(p)
		if rerr != nil {
			return "", rerr
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(p), target)
		}
		return resolveCanonicalPath(filepath.Clean(target), hops+1)
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return "", lerr
	}

	parent := filepath.Dir(p)
	if parent == p {
		return p, nil
	}
	parentReal, err := resolveCanonicalPath(parent, hops)
	if err != nil {
		return "", err
	}
	return filepath.Join(parentReal, filepath.Base(p)), nil
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
