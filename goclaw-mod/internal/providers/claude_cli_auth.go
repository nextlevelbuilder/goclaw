package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// ClaudeAuthStatus holds the parsed result of `claude auth status --json`.
type ClaudeAuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	Email            string `json:"email,omitempty"`
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

// CheckClaudeAuthStatus runs `claude auth status --json` using the given CLI
// path and returns the parsed authentication status.
func CheckClaudeAuthStatus(ctx context.Context, cliPath string) (*ClaudeAuthStatus, error) {
	if cliPath == "" {
		cliPath = "claude"
	}

	resolvedPath, err := exec.LookPath(cliPath)
	if err != nil {
		return nil, fmt.Errorf("claude CLI binary not found at %q: %w", cliPath, err)
	}

	cmd := exec.CommandContext(ctx, resolvedPath, "auth", "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude auth status failed: %w", err)
	}

	var status ClaudeAuthStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("failed to parse auth status: %w", err)
	}
	return &status, nil
}
