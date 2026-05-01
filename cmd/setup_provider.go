package cmd

import (
	"fmt"
	"net/url"
	"os"
)

// setupProviderStep guides the user through provider configuration.
func setupProviderStep() {
	fmt.Println("── Step 1: Providers ──")
	fmt.Println()

	providers, err := fetchProviders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching providers: %v\n", err)
		return
	}

	if len(providers) > 0 {
		fmt.Printf("  Found %d existing provider(s):\n", len(providers))
		for _, p := range providers {
			fmt.Printf("    - %s (%s)\n", p.Name, p.ProviderType)
		}
		fmt.Println()

		addMore, err := promptConfirm("Add another provider?", false)
		if err != nil || !addMore {
			return
		}
	} else {
		fmt.Println("  No providers configured yet. Let's add one.")
		fmt.Println()
	}

	for {
		addProvider()

		another, err := promptConfirm("Add another provider?", false)
		if err != nil || !another {
			break
		}
	}
}

func addProvider() {
	typeOptions := []SelectOption[string]{
		{"Anthropic", "anthropic"},
		{"OpenAI", "openai"},
		{"OpenRouter", "openrouter"},
		{"DashScope (Alibaba)", "dashscope"},
		{"OpenAI-compatible", "openai-compat"},
	}
	providerType, err := promptSelect("Provider type", typeOptions, 0)
	if err != nil {
		return
	}

	name, err := promptString("Provider name", "", providerType)
	if err != nil {
		return
	}

	apiKey, err := promptPassword("API key", "will be encrypted at rest")
	if err != nil || apiKey == "" {
		fmt.Println("  Skipped (no API key).")
		return
	}

	baseURL := ""
	if providerType == "openai-compat" {
		baseURL, err = promptString("Base URL", "e.g. https://api.example.com/v1", "")
		if err != nil {
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
		fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
		return
	}

	providerID, _ := resp["id"].(string)
	fmt.Printf("  Provider %q created.\n", name)

	// Auto-verify (ping mode — empty body)
	if providerID != "" {
		fmt.Print("  Verifying... ")
		verifyResp, err := gatewayHTTPPost("/v1/providers/"+url.PathEscape(providerID)+"/verify", nil)
		if err != nil {
			fmt.Printf("FAILED (%v)\n", err)
			return
		}
		if valid, _ := verifyResp["valid"].(bool); valid {
			fmt.Println("OK")
		} else {
			msg, _ := verifyResp["error"].(string)
			if msg == "" {
				msg = "verification failed"
			}
			fmt.Printf("FAILED (%s)\n", msg)
			fmt.Println("  You can update the API key later with 'goclaw providers update'.")
		}
	}
	fmt.Println()
}
