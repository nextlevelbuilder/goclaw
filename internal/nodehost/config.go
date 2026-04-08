package nodehost

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// NodeHostGatewayConfig holds gateway connection settings.
type NodeHostGatewayConfig struct {
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	TLS            bool   `json:"tls,omitempty"`
	TLSFingerprint string `json:"tlsFingerprint,omitempty"`
}

// NodeHostConfig is the persistent node identity and gateway configuration.
type NodeHostConfig struct {
	Version     int                    `json:"version"`
	NodeID      string                 `json:"nodeId"`
	Token       string                 `json:"token,omitempty"`
	DisplayName string                 `json:"displayName,omitempty"`
	Gateway     *NodeHostGatewayConfig `json:"gateway,omitempty"`
}

const nodeHostFile = "node.json"

// ResolveNodeHostConfigPath returns the path to the node host config file.
// Uses GOCLAW_STATE_DIR if set, otherwise ~/.goclaw/state/.
func ResolveNodeHostConfigPath() (string, error) {
	stateDir := os.Getenv("GOCLAW_STATE_DIR")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		stateDir = filepath.Join(home, ".goclaw", "state")
	}
	return filepath.Join(stateDir, nodeHostFile), nil
}

// newUUID generates a random UUID v4 using crypto/rand.
func newUUID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16]), nil
}

// normalizeConfig ensures a config has required fields populated.
func normalizeConfig(cfg *NodeHostConfig) (*NodeHostConfig, error) {
	out := &NodeHostConfig{Version: 1}
	if cfg != nil {
		out.Token = cfg.Token
		out.DisplayName = cfg.DisplayName
		out.Gateway = cfg.Gateway
		if cfg.Version >= 1 {
			out.NodeID = cfg.NodeID
		}
	}
	if out.NodeID == "" {
		id, err := newUUID()
		if err != nil {
			return nil, err
		}
		out.NodeID = id
	}
	return out, nil
}

// LoadNodeHostConfig reads and normalizes the config from disk.
// Returns nil if the file does not exist.
func LoadNodeHostConfig() (*NodeHostConfig, error) {
	filePath, err := ResolveNodeHostConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read node config: %w", err)
	}
	var cfg NodeHostConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse node config: %w", err)
	}
	return normalizeConfig(&cfg)
}

// SaveNodeHostConfig writes the config atomically with 0600 permissions.
func SaveNodeHostConfig(cfg *NodeHostConfig) error {
	filePath, err := ResolveNodeHostConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal node config: %w", err)
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}
	return os.Chmod(filePath, 0o600)
}

// EnsureNodeHostConfig loads, normalizes, saves, and returns the config.
// Creates a new config if none exists.
func EnsureNodeHostConfig() (*NodeHostConfig, error) {
	existing, err := LoadNodeHostConfig()
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeConfig(existing)
	if err != nil {
		return nil, err
	}
	if err := SaveNodeHostConfig(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}
