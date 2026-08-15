package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agents — add, list, delete",
	}
	cmd.AddCommand(agentListCmd())
	cmd.AddCommand(agentAddCmd())
	cmd.AddCommand(agentDeleteCmd())
	cmd.AddCommand(agentChatCmd())
	return cmd
}

// --- agent list ---

// httpAgent is the CLI-side representation of an agent from the HTTP API.
type httpAgent struct {
	ID          string `json:"id"`
	AgentKey    string `json:"agent_key"`
	DisplayName string `json:"display_name"`
	AgentType   string `json:"agent_type"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	IsDefault   bool   `json:"is_default"`
}

func agentListCmd() *cobra.Command {
	var jsonOutput bool
	var agentType string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all agents (requires running gateway)",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runAgentList(jsonOutput, agentType)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().StringVar(&agentType, "type", "", "filter by agent type (open|predefined)")
	return cmd
}

func runAgentList(jsonOutput bool, agentType string) {
	path := "/v1/agents"
	resp, err := gatewayHTTPGet(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Parse agents array from response
	raw, _ := json.Marshal(resp["agents"])
	var agents []httpAgent
	if err := json.Unmarshal(raw, &agents); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing agent list: %v\n", err)
		os.Exit(1)
	}

	// Apply type filter
	if agentType != "" {
		var filtered []httpAgent
		for _, a := range agents {
			if a.AgentType == agentType {
				filtered = append(filtered, a)
			}
		}
		agents = filtered
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(agents, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tDISPLAY NAME\tTYPE\tPROVIDER\tMODEL\tSTATUS")
	for _, a := range agents {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			a.AgentKey, a.DisplayName, a.AgentType, a.Provider, a.Model, a.Status)
	}
	w.Flush()
}

// --- agent add ---

func agentAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new agent (interactive, requires running gateway)",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runAgentAdd()
		},
	}
}

// httpProvider is the CLI-side representation of a provider from the HTTP API.
type httpProvider struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
	Enabled      bool   `json:"enabled"`
}

// httpProviderModel is a model entry from a provider's model list.
type httpProviderModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

func runAgentAdd() {
	fmt.Println("── Add New Agent ──")
	fmt.Println()

	// Step 1: Agent key
	agentKey, err := promptString("Agent key (slug)", "e.g. coder, researcher, assistant", "")
	if err != nil || agentKey == "" {
		fmt.Println("Cancelled.")
		return
	}

	// Step 2: Display name
	displayName, err := promptString("Display name", "", agentKey)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 3: Agent type
	typeOptions := []SelectOption[string]{
		{"Open (per-user context)", "open"},
		{"Predefined (shared context)", "predefined"},
	}
	agentType, err := promptSelect("Agent type", typeOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 4: Provider (fetched from gateway)
	providers, err := fetchProviders()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching providers: %v\n", err)
		os.Exit(1)
	}
	if len(providers) == 0 {
		fmt.Println("No providers configured. Run 'goclaw providers add' first.")
		return
	}

	providerOptions := make([]SelectOption[string], len(providers))
	for i, p := range providers {
		label := fmt.Sprintf("%s (%s)", p.Name, p.ProviderType)
		providerOptions[i] = SelectOption[string]{Label: label, Value: p.ID}
	}
	providerID, err := promptSelect("Provider", providerOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 5: Model (fetched from selected provider)
	model, err := selectModel(providerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create agent via HTTP API
	body := map[string]any{
		"agent_key":    agentKey,
		"display_name": displayName,
		"agent_type":   agentType,
		"provider":     findProviderType(providers, providerID),
		"model":        model,
	}

	_, err = gatewayHTTPPost("/v1/agents", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Agent %q created successfully.\n", agentKey)
	fmt.Printf("  Type:     %s\n", agentType)
	fmt.Printf("  Model:    %s\n", model)
}

// fetchProviders returns the list of providers from the gateway.
func fetchProviders() ([]httpProvider, error) {
	resp, err := gatewayHTTPGet("/v1/providers")
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(resp["providers"])
	var providers []httpProvider
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("parse providers: %w", err)
	}
	return providers, nil
}

// selectModel fetches models from a provider and prompts the user to pick one.
func selectModel(providerID string) (string, error) {
	resp, err := gatewayHTTPGet("/v1/providers/" + url.PathEscape(providerID) + "/models")
	if err != nil {
		// Fallback: manual model input if provider doesn't support model listing
		model, promptErr := promptString("Model name", "e.g. claude-sonnet-4-20250514", "")
		if promptErr != nil || model == "" {
			return "", fmt.Errorf("cancelled")
		}
		return model, nil
	}

	raw, _ := json.Marshal(resp["models"])
	var models []httpProviderModel
	if err := json.Unmarshal(raw, &models); err != nil || len(models) == 0 {
		// Fallback to manual input
		model, promptErr := promptString("Model name", "e.g. claude-sonnet-4-20250514", "")
		if promptErr != nil || model == "" {
			return "", fmt.Errorf("cancelled")
		}
		return model, nil
	}

	options := make([]SelectOption[string], len(models))
	for i, m := range models {
		label := m.ID
		if m.Name != "" && m.Name != m.ID {
			label = fmt.Sprintf("%s (%s)", m.ID, m.Name)
		}
		options[i] = SelectOption[string]{Label: label, Value: m.ID}
	}

	selected, err := promptSelect("Model", options, 0)
	if err != nil {
		return "", fmt.Errorf("cancelled")
	}
	return selected, nil
}

// findProviderType returns the provider_type for a given provider ID.
func findProviderType(providers []httpProvider, id string) string {
	for _, p := range providers {
		if p.ID == id {
			return p.ProviderType
		}
	}
	return ""
}

// --- agent delete ---

func agentDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <agent-id>",
		Short: "Delete an agent (requires running gateway)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runAgentDelete(args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation")
	return cmd
}

func runAgentDelete(agentID string, force bool) {
	if !force {
		confirmed, err := promptConfirm(fmt.Sprintf("Delete agent %q?", agentID), false)
		if err != nil || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
	}

	if err := gatewayHTTPDelete("/v1/agents/" + url.PathEscape(agentID)); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting agent: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Agent %q deleted.\n", agentID)
}
