package providers

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

// resolveCursorMCPConfig writes the MCP config to <workdir>/.cursor/mcp.json.
// Cursor reads project-level MCP config from this path (not via --mcp-config flag).
// Reuses WriteMCPConfig to generate the JSON, then copies to the Cursor-specific path.
// Returns true if MCP config was written, false if no config data or write failed.
func (p *CursorCLIProvider) resolveCursorMCPConfig(ctx context.Context, workDir, sessionKey string, bc BridgeContext) bool {
	if p.mcpConfigData == nil {
		return false
	}

	// Generate config via shared helper (writes to mcp-configs/<session>/mcp-config.json)
	srcPath := p.mcpConfigData.WriteMCPConfig(ctx, sessionKey, bc)
	if srcPath == "" {
		return false
	}

	data, err := os.ReadFile(srcPath)
	if err != nil {
		slog.Warn("cursor-cli: failed to read mcp config source", "path", srcPath, "error", err)
		return false
	}

	// Ensure <workdir>/.cursor/ directory exists
	cursorDir := filepath.Join(workDir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		slog.Warn("cursor-cli: failed to create .cursor dir", "dir", cursorDir, "error", err)
		return false
	}

	targetPath := filepath.Join(cursorDir, "mcp.json")

	// Skip write if content unchanged
	if existing, err := os.ReadFile(targetPath); err == nil && string(existing) == string(data) {
		return true
	}

	if err := os.WriteFile(targetPath, data, 0600); err != nil {
		slog.Warn("cursor-cli: failed to write .cursor/mcp.json", "path", targetPath, "error", err)
		return false
	}

	return true
}
