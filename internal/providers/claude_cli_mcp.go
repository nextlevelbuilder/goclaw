package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// MCPServerEntry represents a single MCP server config for CLI injection.
type MCPServerEntry struct {
	Name      string
	Transport string // "stdio", "sse", "streamable-http"
	Command   string
	Args      []string
	URL       string
	Headers   map[string]string
	Env       map[string]string
}

// MCPServerLookup returns accessible MCP servers for a given agent ID.
// Used to inject per-agent DB-backed MCP servers into CLI MCP config.
type MCPServerLookup func(ctx context.Context, agentID string) []MCPServerEntry

// MCPConfigData holds the base MCP server entries built at startup.
// Per-session configs are written via WriteMCPConfig with agent context injected.
type MCPConfigData struct {
	Servers        map[string]any // external MCP server entries (stdio/sse/http)
	GatewayAddr    string
	GatewayToken   string
	AgentMCPLookup MCPServerLookup // optional: resolves per-agent MCP servers from DB
}

// BuildCLIMCPConfigData builds the base MCP server map from config.
// Does NOT include the goclaw-bridge entry — that's added per-session
// with agent context headers in WriteMCPConfig.
func BuildCLIMCPConfigData(servers map[string]*config.MCPServerConfig, gatewayAddr string, gatewayToken ...string) *MCPConfigData {
	mcpServers := make(map[string]any, len(servers))

	for name, srv := range servers {
		if !srv.IsEnabled() {
			continue
		}
		entry := mcpServerEntryToConfig(MCPServerEntry{
			Name:      name,
			Transport: srv.Transport,
			Command:   srv.Command,
			Args:      srv.Args,
			URL:       srv.URL,
			Headers:   srv.Headers,
			Env:       srv.Env,
		})
		if len(entry) > 0 {
			mcpServers[name] = entry
		}
	}

	token := ""
	if len(gatewayToken) > 0 {
		token = gatewayToken[0]
	}

	return &MCPConfigData{
		Servers:      mcpServers,
		GatewayAddr:  gatewayAddr,
		GatewayToken: token,
	}
}

// mcpConfigBaseDir returns dataDir/mcp-configs, separate from workDir
// so agent cannot read tokens from the MCP config files.
func mcpConfigBaseDir() string {
	return filepath.Join(config.ResolvedDataDirFromEnv(), "mcp-configs")
}

// BridgeContext holds per-call context for MCP bridge headers.
type BridgeContext struct {
	AgentID  string
	UserID   string
	Channel  string
	ChatID   string
	PeerKind string
}

// WriteMCPConfig writes a per-session MCP config file with agent context headers.
// Files are stored at ~/.goclaw/mcp-configs/<safe-session-key>/mcp-config.json,
// outside the agent's workDir so tokens are not exposed.
// Skips write if content is unchanged. Returns the file path.
func (d *MCPConfigData) WriteMCPConfig(ctx context.Context, sessionKey string, bc BridgeContext) string {
	return d.writeMCPConfigInternal(ctx, sessionKey, bc.AgentID, bc.UserID, bc.Channel, bc.ChatID, bc.PeerKind)
}

func (d *MCPConfigData) writeMCPConfigInternal(ctx context.Context, sessionKey, agentID, userID, channel, chatID, peerKind string) string {
	if d == nil || (len(d.Servers) == 0 && d.GatewayAddr == "" && d.AgentMCPLookup == nil) {
		return ""
	}

	// Shallow-copy the outer map so we can add the bridge entry without mutating the shared base.
	// Inner server entries are not modified, so shallow copy is sufficient.
	servers := make(map[string]any, len(d.Servers)+1)
	maps.Copy(servers, d.Servers)

	// Inject per-agent MCP servers from DB (if lookup is configured and agentID is set)
	if d.AgentMCPLookup != nil && agentID != "" {
		for _, srv := range d.AgentMCPLookup(ctx, agentID) {
			if _, exists := servers[srv.Name]; exists {
				continue // don't override static/bridge entries
			}
			entry := mcpServerEntryToConfig(srv)
			if len(entry) > 0 {
				servers[srv.Name] = entry
			}
		}
	}

	// Build bridge entry with per-session agent context headers
	if d.GatewayAddr != "" {
		headers := make(map[string]string)
		if d.GatewayToken != "" {
			headers["Authorization"] = "Bearer " + d.GatewayToken
		}
		if agentID != "" && !strings.ContainsAny(agentID, "\r\n\x00") {
			headers["X-Agent-ID"] = agentID
		}
		if userID != "" && !strings.ContainsAny(userID, "\r\n\x00") {
			headers["X-User-ID"] = userID
		}
		if channel != "" && !strings.ContainsAny(channel, "\r\n\x00") {
			headers["X-Channel"] = channel
		}
		if chatID != "" && !strings.ContainsAny(chatID, "\r\n\x00") {
			headers["X-Chat-ID"] = chatID
		}
		if peerKind != "" && !strings.ContainsAny(peerKind, "\r\n\x00") {
			headers["X-Peer-Kind"] = peerKind
		}
		// HMAC signature over all context fields to prevent header forgery
		if d.GatewayToken != "" && (agentID != "" || userID != "") {
			headers["X-Bridge-Sig"] = SignBridgeContext(d.GatewayToken, agentID, userID, channel, chatID, peerKind)
		}

		bridgeEntry := map[string]any{
			"url":  fmt.Sprintf("http://%s/mcp/bridge", d.GatewayAddr),
			"type": "http",
		}
		if len(headers) > 0 {
			bridgeEntry["headers"] = headers
		}
		servers["goclaw-bridge"] = bridgeEntry
	}

	if len(servers) == 0 {
		return ""
	}

	data, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		slog.Warn("claude-cli: failed to marshal mcp config", "error", err)
		return ""
	}

	// Write to per-session dir outside workDir
	safe := sanitizePathSegment(sessionKey)
	dir := filepath.Join(mcpConfigBaseDir(), safe)
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Warn("claude-cli: failed to create mcp config dir", "error", err)
		return ""
	}

	path := filepath.Join(dir, "mcp-config.json")

	// Skip write if unchanged
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
		return path
	}
	// Atomic write: temp file + rename to prevent partial reads
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		slog.Warn("claude-cli: failed to write mcp config tmp", "error", err)
		return ""
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		slog.Warn("claude-cli: failed to rename mcp config", "error", err)
		return ""
	}

	return path
}

// mcpServerEntryToConfig converts an MCPServerEntry to the CLI MCP config format.
func mcpServerEntryToConfig(srv MCPServerEntry) map[string]any {
	entry := make(map[string]any)
	switch srv.Transport {
	case "stdio":
		if srv.Command != "" {
			entry["command"] = srv.Command
		}
		if len(srv.Args) > 0 {
			entry["args"] = srv.Args
		}
		if len(srv.Env) > 0 {
			entry["env"] = srv.Env
		}
	case "sse":
		if srv.URL != "" {
			entry["url"] = srv.URL
			entry["type"] = "sse"
		}
		if len(srv.Headers) > 0 {
			entry["headers"] = srv.Headers
		}
	case "streamable-http":
		if srv.URL != "" {
			entry["url"] = srv.URL
			entry["type"] = "http"
		}
		if len(srv.Headers) > 0 {
			entry["headers"] = srv.Headers
		}
	}
	return entry
}

// sanitizePathSegment makes a string safe for use as a single filesystem directory name.
// Replaces path separators and special chars, strips null bytes, handles ".." traversal,
// and truncates to 255 chars.
func sanitizePathSegment(s string) string {
	safe := strings.NewReplacer(":", "-", "/", "-", "\\", "-", "\x00", "").Replace(s)
	// Collapse any ".." sequences to prevent traversal
	safe = strings.ReplaceAll(safe, "..", "_")
	if len(safe) > 255 {
		safe = safe[:255]
	}
	if safe == "" || safe == "." {
		safe = "default"
	}
	return safe
}

// SignBridgeContext computes HMAC-SHA256 over all bridge context fields to prevent forgery.
// Payload: agentID|userID|channel|chatID|peerKind
func SignBridgeContext(key, agentID, userID, channel, chatID, peerKind string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(agentID + "|" + userID + "|" + channel + "|" + chatID + "|" + peerKind))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyBridgeContext checks the HMAC signature against the expected bridge context.
func VerifyBridgeContext(key, agentID, userID, channel, chatID, peerKind, sig string) bool {
	expected := SignBridgeContext(key, agentID, userID, channel, chatID, peerKind)
	return hmac.Equal([]byte(expected), []byte(sig))
}
