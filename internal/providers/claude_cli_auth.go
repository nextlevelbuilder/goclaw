package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// CheckCodexAuthStatus runs `codex login status` using the given CLI path and
// returns a best-effort parsed authentication status.
func CheckCodexAuthStatus(ctx context.Context, cliPath string) (*ClaudeAuthStatus, error) {
	if cliPath == "" {
		cliPath = "codex"
	}
	if status := codexAuthFileStatus(); status.LoggedIn {
		return status, nil
	}

	cmdName := cliPath
	args := []string{"login", "status"}
	if cliPath == "codex" {
		if _, err := exec.LookPath("node"); err == nil && fileExists("/usr/local/lib/node_modules/@openai/codex/bin/codex.js") {
			cmdName = "node"
			args = []string{"/usr/local/lib/node_modules/@openai/codex/bin/codex.js", "login", "status"}
		}
	} else if _, err := exec.LookPath(cliPath); err != nil {
		return nil, fmt.Errorf("codex CLI binary not found at %q: %w", cliPath, err)
	}

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Env = codexCLIAuthEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	text := strings.TrimSpace(string(output))
	if text == "" {
		text = strings.TrimSpace(stderr.String())
	}
	if err != nil {
		if strings.Contains(strings.ToLower(text), "not logged in") {
			if status := codexAuthFileStatus(); status.LoggedIn {
				return status, nil
			}
			return &ClaudeAuthStatus{LoggedIn: false}, nil
		}
		if status := codexAuthFileStatus(); status.LoggedIn {
			return status, nil
		}
		return nil, fmt.Errorf("codex login status failed: %w: %s", err, text)
	}

	lower := strings.ToLower(text)
	if strings.Contains(lower, "not logged in") || strings.Contains(lower, "not authenticated") {
		if status := codexAuthFileStatus(); status.LoggedIn {
			return status, nil
		}
		return &ClaudeAuthStatus{LoggedIn: false}, nil
	}
	status := &ClaudeAuthStatus{
		LoggedIn:         text != "",
		SubscriptionType: "codex",
	}
	if email := extractEmail(text); email != "" {
		status.Email = email
	} else if status.LoggedIn {
		status.Email = "Codex CLI"
	}
	return status, nil
}

// CheckGeminiAuthStatus returns a best-effort parsed Gemini CLI auth status.
func CheckGeminiAuthStatus(ctx context.Context, cliPath string) (*ClaudeAuthStatus, error) {
	if cliPath == "" {
		cliPath = "gemini"
	}
	if status := geminiAuthFileStatus(); status.LoggedIn {
		return status, nil
	}
	if _, err := exec.LookPath(cliPath); err != nil {
		return nil, fmt.Errorf("gemini CLI binary not found at %q: %w", cliPath, err)
	}

	// Gemini CLI does not expose a stable machine-readable auth status command.
	// We rely on the local config/auth files as the primary signal.
	return &ClaudeAuthStatus{LoggedIn: false}, nil
}

func codexAuthFileStatus() *ClaudeAuthStatus {
	for _, path := range codexAuthPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if status := parseCodexAuthJSON(data); status.LoggedIn {
			return status
		}
	}
	return &ClaudeAuthStatus{LoggedIn: false}
}

func parseCodexAuthJSON(data []byte) *ClaudeAuthStatus {
	var raw struct {
		AuthMode string `json:"auth_mode"`
		APIKey   string `json:"OPENAI_API_KEY"`
		Tokens   struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
		LastRefresh struct {
			AccountID string `json:"account_id"`
			Email     string `json:"email"`
		} `json:"last_refresh"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return &ClaudeAuthStatus{LoggedIn: false}
	}
	loggedIn := raw.AuthMode != "" || raw.APIKey != "" || raw.Tokens.IDToken != "" || raw.Tokens.AccessToken != "" || raw.Tokens.RefreshToken != ""
	if !loggedIn {
		return &ClaudeAuthStatus{LoggedIn: false}
	}
	email := raw.LastRefresh.Email
	if email == "" {
		email = raw.Tokens.AccountID
	}
	if email == "" {
		email = "Codex CLI"
	}
	plan := raw.AuthMode
	if plan == "" {
		plan = "codex"
	}
	return &ClaudeAuthStatus{LoggedIn: true, Email: email, SubscriptionType: plan}
}

func geminiAuthFileStatus() *ClaudeAuthStatus {
	for _, path := range geminiSettingsPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if status := parseGeminiSettingsJSON(path, data); status.LoggedIn {
			return status
		}
	}
	return &ClaudeAuthStatus{LoggedIn: false}
}

func parseGeminiSettingsJSON(path string, data []byte) *ClaudeAuthStatus {
	var raw struct {
		Security struct {
			Auth struct {
				SelectedType string `json:"selectedType"`
			} `json:"auth"`
		} `json:"security"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return &ClaudeAuthStatus{LoggedIn: false}
	}
	authType := strings.TrimSpace(raw.Security.Auth.SelectedType)
	if authType == "" {
		return &ClaudeAuthStatus{LoggedIn: false}
	}
	baseDir := filepath.Dir(path)
	switch authType {
	case "oauth-personal":
		if email := parseGeminiGoogleAccount(filepath.Join(baseDir, "google_accounts.json")); email != "" {
			return &ClaudeAuthStatus{LoggedIn: true, Email: email, SubscriptionType: authType}
		}
		if fileExists(filepath.Join(baseDir, "oauth_creds.json")) {
			return &ClaudeAuthStatus{LoggedIn: true, Email: "Google account", SubscriptionType: authType}
		}
	case "gemini-api-key":
		if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
			return &ClaudeAuthStatus{LoggedIn: true, Email: "Gemini API Key", SubscriptionType: authType}
		}
	case "vertex-ai", "USE_VERTEX_AI", "COMPUTE_ADC":
		if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" || os.Getenv("GOOGLE_CLOUD_PROJECT") != "" || os.Getenv("GOOGLE_CLOUD_PROJECT_ID") != "" {
			return &ClaudeAuthStatus{LoggedIn: true, Email: "Google Cloud", SubscriptionType: authType}
		}
	}
	return &ClaudeAuthStatus{LoggedIn: false}
}

func parseGeminiGoogleAccount(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var raw struct {
		Active string `json:"active"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}
	return strings.TrimSpace(raw.Active)
}

func codexAuthPaths() []string {
	seen := make(map[string]bool)
	var paths []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if home := os.Getenv("CODEX_HOME"); home != "" {
		add(filepath.Join(home, "auth.json"))
	}
	add("/app/data/.codex/auth.json")
	add("/app/.codex/auth.json")
	if home := os.Getenv("HOME"); home != "" {
		add(filepath.Join(home, ".codex", "auth.json"))
	}
	add("/home/goclaw/.codex/auth.json")
	add("/root/.codex/auth.json")
	return paths
}

func geminiSettingsPaths() []string {
	seen := make(map[string]bool)
	var paths []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if home := os.Getenv("HOME"); home != "" {
		add(filepath.Join(home, ".gemini", "settings.json"))
	}
	add("/app/data/.gemini/settings.json")
	add("/app/.gemini/settings.json")
	add("/home/goclaw/.gemini/settings.json")
	add("/root/.gemini/settings.json")
	return paths
}

func extractEmail(text string) string {
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, " \t\r\n,;()<>[]{}")
		if strings.Contains(field, "@") && strings.Contains(field, ".") {
			return field
		}
	}
	return ""
}

func codexCLIAuthEnv() []string {
	env := filterCLIEnv(os.Environ())
	if os.Getenv("CODEX_HOME") == "" && fileExists("/app/data/.codex") {
		env = append(env, "CODEX_HOME=/app/data/.codex")
	}
	if os.Getenv("HOME") == "" && fileExists("/app") {
		env = append(env, "HOME=/app")
	}
	return env
}
