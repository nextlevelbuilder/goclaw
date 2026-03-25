package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GWSTool wraps the gws CLI binary and exposes Google Workspace (Gmail) operations
// as structured tool calls. Credentials are fetched at runtime from builtin_tools.settings.
type GWSTool struct {
	bts store.BuiltinToolStore
}

func NewGWSTool(bts store.BuiltinToolStore) *GWSTool {
	return &GWSTool{bts: bts}
}

func (t *GWSTool) Name() string { return "gws" }

func (t *GWSTool) Description() string {
	return "Interact with Google Workspace services (Gmail). Actions: gmail_send, gmail_search, gmail_triage, gmail_reply, gmail_read."
}

func (t *GWSTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"gmail_send", "gmail_search", "gmail_triage", "gmail_reply", "gmail_read"},
				"description": "Gmail operation to perform",
			},
			"to":         map[string]any{"type": "string", "description": "Recipient email (gmail_send)"},
			"subject":    map[string]any{"type": "string", "description": "Email subject (gmail_send)"},
			"body":       map[string]any{"type": "string", "description": "Email body or reply text (gmail_send, gmail_reply)"},
			"query":      map[string]any{"type": "string", "description": "Search query (gmail_search)"},
			"message_id": map[string]any{"type": "string", "description": "Message ID (gmail_reply, gmail_read)"},
		},
		"required": []string{"action"},
	}
}

func (t *GWSTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("gws: action is required. Valid: gmail_send, gmail_search, gmail_triage, gmail_reply, gmail_read")
	}
	if len(action) >= 4 && action[:4] == "auth" {
		return ErrorResult("gws: auth actions are not permitted via this tool")
	}

	cliArgs, err := buildGWSArgs(action, args)
	if err != nil {
		return ErrorResult("gws: " + err.Error())
	}

	binaryPath, err := exec.LookPath("gws")
	if err != nil {
		return ErrorResult("gws CLI not installed. Install via Packages page: @googleworkspace/cli")
	}

	envMap, err := loadGWSCredentials(ctx, t.bts)
	if err != nil {
		return ErrorResult(err.Error())
	}
	for _, v := range envMap {
		if v != "" {
			AddCredentialScrubValues(v)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, cliArgs...)
	cmd.Env = buildCredentialedEnv(envMap)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	execErr := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "STDERR:\n" + stderr.String()
	}

	if execErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ErrorResult("gws: command timed out after 30s")
		}
		exitCode := -1
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return &Result{
			ForLLM:  fmt.Sprintf("gws: command failed (exit %d)\n%s", exitCode, ScrubCredentials(output)),
			ForUser: fmt.Sprintf("gws command failed with exit code %d", exitCode),
			IsError: true,
		}
	}
	if output == "" {
		output = "(command completed with no output)"
	}
	return SilentResult(ScrubCredentials(output))
}

func buildGWSArgs(action string, args map[string]any) ([]string, error) {
	str := func(k string) string { v, _ := args[k].(string); return v }
	switch action {
	case "gmail_send":
		to, subject, body := str("to"), str("subject"), str("body")
		if to == "" || subject == "" || body == "" {
			return nil, fmt.Errorf("gmail_send requires: to, subject, body")
		}
		return []string{"gmail", "+send", "--to", to, "--subject", subject, "--body", body}, nil
	case "gmail_search":
		q := str("query")
		if q == "" {
			return nil, fmt.Errorf("gmail_search requires: query")
		}
		params := fmt.Sprintf(`{"q":"%s","maxResults":10}`, q)
		return []string{"gmail", "users", "messages", "list", "--params", params, "--format", "json"}, nil
	case "gmail_triage":
		return []string{"gmail", "+triage"}, nil
	case "gmail_reply":
		id, body := str("message_id"), str("body")
		if id == "" || body == "" {
			return nil, fmt.Errorf("gmail_reply requires: message_id, body")
		}
		return []string{"gmail", "+reply", "--message-id", id, "--body", body}, nil
	case "gmail_read":
		id := str("message_id")
		if id == "" {
			return nil, fmt.Errorf("gmail_read requires: message_id")
		}
		return []string{"gmail", "+read", "--message-id", id}, nil
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
}

type gwsSettings struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

func loadGWSCredentials(ctx context.Context, bts store.BuiltinToolStore) (map[string]string, error) {
	raw, err := bts.GetSettings(ctx, "gws")
	if err != nil || raw == nil {
		return nil, fmt.Errorf("Google Workspace not configured. Go to Built-in Tools → gws → Settings")
	}
	var s gwsSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("invalid gws settings: %v", err)
	}
	if s.ClientID == "" || s.ClientSecret == "" || s.RefreshToken == "" {
		return nil, fmt.Errorf("Google Workspace not configured. Authorize via Built-in Tools → gws → Settings")
	}

	credsJSON, _ := json.Marshal(map[string]any{
		"type":          "authorized_user",
		"client_id":     s.ClientID,
		"client_secret": s.ClientSecret,
		"refresh_token": s.RefreshToken,
	})

	tmpDir := filepath.Join(os.TempDir(), "goclaw-gws")
	_ = os.MkdirAll(tmpDir, 0755)
	credsPath := filepath.Join(tmpDir, "credentials.json")
	if err := os.WriteFile(credsPath, credsJSON, 0644); err != nil {
		return nil, fmt.Errorf("failed to write credentials: %v", err)
	}

	return map[string]string{"GOOGLE_WORKSPACE_CLI_CREDENTIALS_FILE": credsPath}, nil
}
