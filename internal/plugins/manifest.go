// Package plugins — Plugin manifest parsing and types (CP-08).
package plugins

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// PluginManifest is the parsed content of plugin.yaml.
type PluginManifest struct {
	Name             string            `yaml:"name"`
	Version          string            `yaml:"version"`
	Description      string            `yaml:"description"`
	Author           string            `yaml:"author"`
	License          string            `yaml:"license"`
	MinGoClawVersion string            `yaml:"min_goclaw_version"`
	Dependencies     []PluginDep       `yaml:"dependencies"`
	Commands         []PluginCommand   `yaml:"commands"`
	Agents           []PluginAgent     `yaml:"agents"`
	Hooks            map[string][]Hook `yaml:"hooks"`
	Servers          PluginServers     `yaml:"servers"`
}

// PluginDep declares a dependency on another plugin.
type PluginDep struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"` // semver constraint
}

// PluginCommand defines a slash command provided by the plugin.
type PluginCommand struct {
	Name        string `yaml:"name"`
	File        string `yaml:"file"`
	Description string `yaml:"description"`
}

// PluginAgent defines a custom agent provided by the plugin.
type PluginAgent struct {
	Name   string   `yaml:"name"`
	File   string   `yaml:"file"`
	Tools  []string `yaml:"tools"`
	Memory string   `yaml:"memory"` // "user", "project", "local"
}

// Hook defines a lifecycle hook handler.
type Hook struct {
	MatchTool string        `yaml:"match_tool"`
	Command   string        `yaml:"command"`
	Timeout   time.Duration `yaml:"timeout"`
}

// PluginServers groups MCP and LSP server definitions.
type PluginServers struct {
	MCP []ServerDef `yaml:"mcp"`
	LSP []ServerDef `yaml:"lsp"`
}

// ServerDef defines an external server provided by the plugin.
type ServerDef struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env"`
}

// ParseManifest reads and parses a plugin.yaml file.
func ParseManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m PluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if m.Name == "" {
		return nil, fmt.Errorf("plugin manifest missing required field: name")
	}
	if m.Version == "" {
		return nil, fmt.Errorf("plugin manifest missing required field: version")
	}

	return &m, nil
}
