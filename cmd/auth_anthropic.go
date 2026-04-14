package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// setupTokenPattern matches Anthropic OAuth setup tokens in CLI output.
var setupTokenPattern = regexp.MustCompile(`sk-[A-Za-z0-9_-]{40,}`)

func authAnthropicCmd() *cobra.Command {
	var tokenFlag string

	cmd := &cobra.Command{
		Use:   "anthropic",
		Short: "Authenticate with Anthropic using a setup token",
		Long:  "Obtain an Anthropic setup token via 'claude setup-token' or paste one manually.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var token string

			if tokenFlag != "" {
				// Manual token entry via --token flag
				token = strings.TrimSpace(tokenFlag)
			} else {
				// Shell out to claude setup-token
				var err error
				token, err = runClaudeSetupToken()
				if err != nil {
					return err
				}
			}

			// Send token to gateway
			result, err := gatewayRequestJSON("POST", "/v1/auth/anthropic/token", map[string]any{
				"token": token,
			})
			if err != nil {
				return fmt.Errorf("store token: %w", err)
			}

			name, _ := result["provider_name"].(string)
			if name == "" {
				name = "anthropic"
			}
			fmt.Printf("Anthropic setup token stored (provider: %s).\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&tokenFlag, "token", "", "Paste a setup token directly (skip claude setup-token)")
	return cmd
}

// runClaudeSetupToken executes 'claude setup-token' and extracts the token from output.
func runClaudeSetupToken() (string, error) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Println("Claude CLI not found.")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  1. Install Claude CLI: https://docs.anthropic.com/en/docs/claude-cli")
		fmt.Println("  2. Paste a token manually: goclaw auth anthropic --token <your-token>")
		return "", fmt.Errorf("claude CLI not found: %w", err)
	}

	cmd := exec.Command(claudePath, "setup-token")
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude setup-token failed: %w", err)
	}

	// Scan output for sk- token pattern
	token := setupTokenPattern.FindString(string(output))
	if token == "" {
		return "", fmt.Errorf("no setup token found in claude output (expected sk-...)")
	}

	return token, nil
}

// gatewayRequestJSON sends a JSON-body request to the running gateway.
func gatewayRequestJSON(method, path string, body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := resolveGatewayBaseURL() + path
	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if token := os.Getenv("GOCLAW_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach gateway at %s: %w", resolveGatewayBaseURL(), err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("invalid response from gateway: %s", string(respBody))
	}

	if resp.StatusCode >= 400 {
		if msg, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("gateway error: %s", msg)
		}
		return nil, fmt.Errorf("gateway returned status %d", resp.StatusCode)
	}

	return result, nil
}
