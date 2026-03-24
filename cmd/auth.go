package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with LLM providers via OAuth",
		Long:  "Manage OAuth authentication via the running gateway. Requires the gateway to be running.",
	}
	cmd.AddCommand(authStatusCmd())
	cmd.AddCommand(authLogoutCmd())
	return cmd
}

// gatewayURL returns the base URL for the running gateway.
func gatewayURL() string {
	if u := os.Getenv("GOCLAW_GATEWAY_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	host := os.Getenv("GOCLAW_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("GOCLAW_PORT")
	if port == "" {
		port = "3577"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// gatewayRequest sends an authenticated request to the running gateway.
func gatewayRequest(method, path string) (map[string]any, error) {
	url := gatewayURL() + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if token := os.Getenv("GOCLAW_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach gateway at %s: %w", gatewayURL(), err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid response from gateway: %s", string(body))
	}

	if resp.StatusCode >= 400 {
		if msg, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("gateway error: %s", msg)
		}
		return nil, fmt.Errorf("gateway returned status %d", resp.StatusCode)
	}

	return result, nil
}

func authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [provider]",
		Short: "Show OAuth authentication status",
		Long:  "Check if ChatGPT OAuth is configured on the running gateway.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := resolveOAuthProviderArg(args)
			result, err := gatewayRequest("GET", fmt.Sprintf("/v1/auth/chatgpt/%s/status", url.PathEscape(provider)))
			if err != nil {
				return err
			}

			if auth, _ := result["authenticated"].(bool); auth {
				name, _ := result["provider_name"].(string)
				if name == "" {
					name = provider
				}
				fmt.Printf("OpenAI OAuth: active (provider: %s)\n", name)
				fmt.Printf("Use model prefix '%s/' in agent config (e.g. %s/gpt-5.4).\n", name, name)
			} else {
				fmt.Printf("No OAuth tokens found for provider '%s'.\n", provider)
				fmt.Println("Use the web UI to authenticate with ChatGPT OAuth.")
			}
			return nil
		},
	}
}

func authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [provider]",
		Short: "Remove stored OAuth tokens",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := resolveOAuthProviderArg(args)
			_, err := gatewayRequest("POST", fmt.Sprintf("/v1/auth/chatgpt/%s/logout", url.PathEscape(provider)))
			if err != nil {
				return err
			}

			fmt.Printf("OpenAI OAuth token removed for provider '%s'.\n", provider)
			return nil
		},
	}
}

func resolveOAuthProviderArg(args []string) string {
	if len(args) == 0 {
		return oauth.DefaultProviderName
	}
	provider := strings.TrimSpace(args[0])
	if provider == "" || provider == "openai" {
		return oauth.DefaultProviderName
	}
	return provider
}
