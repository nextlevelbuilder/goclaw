package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func providersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Manage LLM providers (requires running gateway)",
	}
	cmd.AddCommand(providersListCmd())
	cmd.AddCommand(providersAddCmd())
	cmd.AddCommand(providersUpdateCmd())
	cmd.AddCommand(providersDeleteCmd())
	cmd.AddCommand(providersVerifyCmd())
	return cmd
}

// httpProviderFull is a detailed provider representation from the HTTP API.
type httpProviderFull struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
	BaseURL      string `json:"base_url"`
	Enabled      bool   `json:"enabled"`
	HasAPIKey    bool   `json:"has_api_key"`
}

func providersListCmd() *cobra.Command {
	var jsonOutput bool
	var showModels bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured providers",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runProvidersList(jsonOutput, showModels)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&showModels, "models", false, "also show available models per provider")
	return cmd
}

func runProvidersList(jsonOutput, showModels bool) {
	providers, err := fetchProviders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput && !showModels {
		data, _ := json.MarshalIndent(providers, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(providers) == 0 {
		fmt.Println("No providers configured.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tNAME\tTYPE\tENABLED\n")
	for _, p := range providers {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", p.ID, p.Name, p.ProviderType, p.Enabled)
	}
	tw.Flush()

	if showModels {
		fmt.Println()
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			fmt.Printf("── Models for %s (%s) ──\n", p.Name, p.ProviderType)
			resp, err := gatewayHTTPGet("/v1/providers/" + url.PathEscape(p.ID) + "/models")
			if err != nil {
				fmt.Printf("  Error: %v\n", err)
				continue
			}
			raw, _ := json.Marshal(resp["models"])
			var models []httpProviderModel
			if err := json.Unmarshal(raw, &models); err != nil {
				fmt.Printf("  Error parsing models: %v\n", err)
				continue
			}
			if len(models) == 0 {
				fmt.Println("  (no models available)")
				continue
			}
			for _, m := range models {
				fmt.Printf("  %s\n", m.ID)
			}
			fmt.Println()
		}
	}
}

func providersAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new provider (interactive)",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runProvidersAdd()
		},
	}
}

func runProvidersAdd() {
	fmt.Println("── Add Provider ──")
	fmt.Println()

	// Step 1: Provider type
	typeOptions := []SelectOption[string]{
		{"Anthropic", "anthropic"},
		{"OpenAI", "openai"},
		{"OpenRouter", "openrouter"},
		{"DashScope (Alibaba)", "dashscope"},
		{"OpenAI-compatible", "openai-compat"},
	}
	providerType, err := promptSelect("Provider type", typeOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 2: Name
	name, err := promptString("Provider name", "", providerType)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 3: API key
	apiKey, err := promptPassword("API key", "will be encrypted at rest")
	if err != nil || apiKey == "" {
		fmt.Println("Cancelled.")
		return
	}

	// Step 4: Base URL (pre-fill per type, editable)
	defaultURL := defaultBaseURL(providerType)
	baseURL := ""
	if providerType == "openai-compat" {
		baseURL, err = promptString("Base URL", "e.g. https://api.example.com/v1", defaultURL)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
	}

	body := map[string]any{
		"name":          name,
		"provider_type": providerType,
		"api_key":       apiKey,
		"enabled":       true,
	}
	if baseURL != "" {
		body["base_url"] = baseURL
	}

	resp, err := gatewayHTTPPost("/v1/providers", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating provider: %v\n", err)
		os.Exit(1)
	}

	providerID, _ := resp["id"].(string)
	fmt.Printf("\nProvider %q (%s) created.\n", name, providerType)

	// Offer to verify
	if providerID != "" {
		verify, err := promptConfirm("Verify connection now?", true)
		if err == nil && verify {
			runProviderVerify(providerID)
		}
	}
}

func providersUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update <id>",
		Short: "Update a provider",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runProvidersUpdate(args[0])
		},
	}
}

func runProvidersUpdate(providerID string) {
	// Fetch current provider
	resp, err := gatewayHTTPGet("/v1/providers/" + url.PathEscape(providerID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	currentName, _ := resp["name"].(string)
	currentType, _ := resp["provider_type"].(string)

	fmt.Printf("Updating provider: %s (%s)\n", currentName, currentType)
	fmt.Println("Press Enter to keep current value.")
	fmt.Println()

	name, err := promptString("Name", "", currentName)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	apiKey, err := promptPassword("New API key (leave empty to keep current)", "")
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	body := map[string]any{"name": name}
	if apiKey != "" {
		body["api_key"] = apiKey
	}

	_, err = gatewayHTTPPut("/v1/providers/"+url.PathEscape(providerID), body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Provider updated.")
}

func providersDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a provider",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			if !force {
				confirmed, err := promptConfirm(fmt.Sprintf("Delete provider %q?", args[0]), false)
				if err != nil || !confirmed {
					fmt.Println("Cancelled.")
					return
				}
			}
			if err := gatewayHTTPDelete("/v1/providers/" + url.PathEscape(args[0])); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Provider %q deleted.\n", args[0])
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation")
	return cmd
}

func providersVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <id>",
		Short: "Verify provider connectivity and list models",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runProviderVerify(args[0])
		},
	}
}

func runProviderVerify(providerID string) {
	fmt.Print("Verifying provider... ")
	resp, err := gatewayHTTPPost("/v1/providers/"+url.PathEscape(providerID)+"/verify", nil)
	if err != nil {
		fmt.Printf("FAILED\n  %v\n", err)
		return
	}

	if ok, _ := resp["success"].(bool); ok {
		fmt.Println("OK")
		// Show available models
		raw, _ := json.Marshal(resp["models"])
		var models []httpProviderModel
		if json.Unmarshal(raw, &models) == nil && len(models) > 0 {
			fmt.Printf("  Available models: %d\n", len(models))
			limit := 10
			for i, m := range models {
				if i >= limit {
					fmt.Printf("  ... and %d more\n", len(models)-limit)
					break
				}
				fmt.Printf("  - %s\n", m.ID)
			}
		}
	} else {
		msg, _ := resp["error"].(string)
		fmt.Printf("FAILED\n  %s\n", msg)
	}
}

// defaultBaseURL returns the default API base URL for a provider type.
func defaultBaseURL(providerType string) string {
	switch providerType {
	case "anthropic":
		return "https://api.anthropic.com"
	case "openai":
		return "https://api.openai.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "dashscope":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	default:
		return ""
	}
}
