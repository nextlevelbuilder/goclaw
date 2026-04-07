package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate named ChatGPT OAuth accounts",
		Long:  "Manage ChatGPT OAuth authentication via the running gateway. Requires the gateway to be running.",
	}
	cmd.AddCommand(authStatusCmd())
	cmd.AddCommand(authLogoutCmd())
	return cmd
}

// gatewayRequest sends an authenticated request to the running gateway.
// Delegates to the shared HTTP client in gateway_http_client.go.
func gatewayRequest(method, path string) (map[string]any, error) {
	return gatewayHTTPDo(method, path, nil)
}

func authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [provider]",
		Short: "Show OAuth authentication status",
		Long:  "Check if a named ChatGPT OAuth account is authenticated on the running gateway.",
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
				fmt.Printf("ChatGPT OAuth account: active (alias: %s)\n", name)
				fmt.Printf("Use model prefix '%s/' in agent config (e.g. %s/gpt-5.4).\n", name, name)
			} else {
				fmt.Printf("No ChatGPT OAuth tokens found for alias '%s'.\n", provider)
				fmt.Println("Use the web UI to authenticate this ChatGPT OAuth account.")
			}
			return nil
		},
	}
}

func authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [provider]",
		Short: "Disconnect stored ChatGPT OAuth tokens",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := resolveOAuthProviderArg(args)
			_, err := gatewayRequest("POST", fmt.Sprintf("/v1/auth/chatgpt/%s/logout", url.PathEscape(provider)))
			if err != nil {
				return err
			}

			fmt.Printf("ChatGPT OAuth account disconnected for alias '%s'.\n", provider)
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
