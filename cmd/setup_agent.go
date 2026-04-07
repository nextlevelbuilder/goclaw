package cmd

import (
	"fmt"
	"os"
)

// setupAgentStep guides the user through agent creation.
func setupAgentStep() {
	fmt.Println("── Step 2: Agent ──")
	fmt.Println()

	agents, err := fetchAgentList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching agents: %v\n", err)
		return
	}

	if len(agents) > 0 {
		fmt.Printf("  Found %d existing agent(s):\n", len(agents))
		for _, a := range agents {
			fmt.Printf("    - %s (%s / %s)\n", a.AgentKey, a.Provider, a.Model)
		}
		fmt.Println()

		create, err := promptConfirm("Create another agent?", false)
		if err != nil || !create {
			return
		}
	} else {
		fmt.Println("  No agents yet. Let's create your first one.")
		fmt.Println()
	}

	createAgent()
}

func createAgent() {
	agentKey, err := promptString("Agent key (slug)", "e.g. assistant, coder", "assistant")
	if err != nil {
		return
	}

	displayName, err := promptString("Display name", "", agentKey)
	if err != nil {
		return
	}

	typeOptions := []SelectOption[string]{
		{"Open (per-user context)", "open"},
		{"Predefined (shared context)", "predefined"},
	}
	agentType, err := promptSelect("Agent type", typeOptions, 0)
	if err != nil {
		return
	}

	// Fetch providers for selection
	providers, err := fetchProviders()
	if err != nil || len(providers) == 0 {
		fmt.Println("  No providers available. Add a provider first.")
		return
	}

	providerOptions := make([]SelectOption[string], len(providers))
	for i, p := range providers {
		providerOptions[i] = SelectOption[string]{
			Label: fmt.Sprintf("%s (%s)", p.Name, p.ProviderType),
			Value: p.ID,
		}
	}
	providerID, err := promptSelect("Provider", providerOptions, 0)
	if err != nil {
		return
	}

	model, err := selectModel(providerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		return
	}

	body := map[string]any{
		"agent_key":    agentKey,
		"display_name": displayName,
		"agent_type":   agentType,
		"provider":     findProviderType(providers, providerID),
		"model":        model,
	}

	_, err = gatewayHTTPPost("/v1/agents", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
		return
	}

	fmt.Printf("  Agent %q created (%s).\n\n", agentKey, model)
}
