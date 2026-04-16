package providers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// QwenAuthStatus holds the parsed result of `qwen auth status`.
type QwenAuthStatus struct {
	LoggedIn   bool   `json:"loggedIn"`
	AuthMethod string `json:"authMethod,omitempty"`
	Type       string `json:"type,omitempty"`
}

// CheckQwenAuthStatus runs `qwen auth status` and returns the parsed status.
func CheckQwenAuthStatus(ctx context.Context, cliPath string) (*QwenAuthStatus, error) {
	if cliPath == "" {
		cliPath = "qwen"
	}

	resolvedPath, err := exec.LookPath(cliPath)
	if err != nil {
		return nil, fmt.Errorf("qwen CLI binary not found at %q: %w", cliPath, err)
	}

	cmd := exec.CommandContext(ctx, resolvedPath, "auth", "status")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("qwen auth status failed: %w", err)
	}

	return parseQwenAuthStatus(string(output))
}

func parseQwenAuthStatus(output string) (*QwenAuthStatus, error) {
	status := &QwenAuthStatus{}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "\u2713") || strings.HasPrefix(line, "✓") {
			status.LoggedIn = true
		}

		if strings.Contains(line, "Authentication Method:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				status.AuthMethod = strings.TrimSpace(parts[1])
				status.LoggedIn = true
			}
		}

		if strings.Contains(line, "Type:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				status.Type = strings.TrimSpace(parts[1])
			}
		}
	}

	if strings.Contains(output, "Not logged in") || strings.Contains(output, "not authenticated") {
		status.LoggedIn = false
	}

	return status, nil
}
