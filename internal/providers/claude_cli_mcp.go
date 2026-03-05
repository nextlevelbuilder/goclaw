package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// BuildCLIMCPConfig exports GoClaw's MCP server configs into a temp JSON file
// compatible with Claude CLI's --mcp-config flag.
// When gatewayAddr is non-empty, a "goclaw-bridge" entry is injected so CLI
// can call GoClaw's built-in tools via streamable-http MCP transport.
// Returns the file path and a cleanup function. Caller must call cleanup when done.
func BuildCLIMCPConfig(servers map[string]*config.MCPServerConfig, gatewayAddr string, gatewayToken ...string) (string, func(), error) {
	mcpServers := make(map[string]interface{}, len(servers)+1)

	for name, srv := range servers {
		if !srv.IsEnabled() {
			continue
		}

		entry := make(map[string]interface{})

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

		default:
			continue
		}

		if len(entry) > 0 {
			mcpServers[name] = entry
		}
	}

	// Inject GoClaw bridge entry so CLI can access built-in tools via MCP
	if gatewayAddr != "" {
		bridgeEntry := map[string]interface{}{
			"url":  fmt.Sprintf("http://%s/mcp/bridge", gatewayAddr),
			"type": "http",
		}
		if len(gatewayToken) > 0 && gatewayToken[0] != "" {
			bridgeEntry["headers"] = map[string]string{
				"Authorization": "Bearer " + gatewayToken[0],
			}
		}
		mcpServers["goclaw-bridge"] = bridgeEntry
	}

	if len(mcpServers) == 0 {
		return "", func() {}, nil
	}

	cfg := map[string]interface{}{
		"mcpServers": mcpServers,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("marshal mcp config: %w", err)
	}

	// Write to a temp file with restricted permissions
	tmpDir := filepath.Join(os.TempDir(), "goclaw-mcp-configs")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", nil, fmt.Errorf("create mcp config dir: %w", err)
	}

	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("mcp-%s.json", uuid.New().String()[:8]))
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return "", nil, fmt.Errorf("write mcp config: %w", err)
	}

	cleanup := func() {
		os.Remove(tmpFile)
	}

	return tmpFile, cleanup, nil
}
