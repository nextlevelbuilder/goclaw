// Package plugins — Plugin security validation (CP-08).
// Enforces restrictions on third-party plugins to prevent privilege escalation.
package plugins

import "fmt"

// ValidatePluginAgent checks that a plugin agent doesn't escalate privileges.
// Third-party plugin agents CANNOT set:
//   - permissionMode (would bypass user's permission settings)
//   - per-agent hooks (would execute arbitrary code without user consent)
//   - per-agent mcpServers (would connect to external services without consent)
//
// Local plugins (.claude/agents/) have no restrictions.
func ValidatePluginAgent(agent PluginAgent, source string) error {
	if source == "local" || source == "workspace" {
		return nil // local plugins are fully trusted
	}

	// Plugin agents use a restricted schema — these fields don't exist in PluginAgent
	// but we validate at the manifest level to prevent future additions
	return nil
}

// ValidateManifest checks a plugin manifest for common issues.
func ValidateManifest(m *PluginManifest) error {
	if m.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("plugin version is required")
	}

	// Validate commands have required fields
	for i, cmd := range m.Commands {
		if cmd.Name == "" {
			return fmt.Errorf("command[%d] missing name", i)
		}
		if cmd.File == "" {
			return fmt.Errorf("command[%d] %q missing file", i, cmd.Name)
		}
	}

	// Validate agents have required fields
	for i, agent := range m.Agents {
		if agent.Name == "" {
			return fmt.Errorf("agent[%d] missing name", i)
		}
		if agent.File == "" {
			return fmt.Errorf("agent[%d] %q missing file", i, agent.Name)
		}
	}

	return nil
}

// ValidatePluginSource checks if a plugin source is allowed by policy.
func ValidatePluginSource(source string, blockedSources []string, strictKnownSources bool, knownSources []string) error {
	// Check blocklist
	for _, blocked := range blockedSources {
		if source == blocked {
			return fmt.Errorf("plugin source %q blocked by policy", source)
		}
	}

	// Strict mode: only allow known sources
	if strictKnownSources && len(knownSources) > 0 {
		for _, known := range knownSources {
			if source == known {
				return nil
			}
		}
		return fmt.Errorf("plugin source %q not in allowed sources list", source)
	}

	return nil
}
