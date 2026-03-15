package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
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

func agentListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured agents",
		Run: func(cmd *cobra.Command, args []string) {
			runAgentList(jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

type agentListEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Workspace   string `json:"workspace,omitempty"`
	IsDefault   bool   `json:"isDefault"`
}

func runAgentList(jsonOutput bool) {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	var entries []agentListEntry

	// Default agent (always present)
	d := cfg.Agents.Defaults
	defaultID := cfg.ResolveDefaultAgentID()
	entries = append(entries, agentListEntry{
		ID:          config.DefaultAgentID,
		DisplayName: cfg.ResolveDisplayName(config.DefaultAgentID),
		Provider:    d.Provider,
		Model:       d.Model,
		Workspace:   d.Workspace,
		IsDefault:   defaultID == config.DefaultAgentID,
	})

	// Agents from list
	ids := make([]string, 0, len(cfg.Agents.List))
	for id := range cfg.Agents.List {
		if id == config.DefaultAgentID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		resolved := cfg.ResolveAgent(id)
		spec := cfg.Agents.List[id]
		name := spec.DisplayName
		if name == "" {
			name = id
		}
		entries = append(entries, agentListEntry{
			ID:          id,
			DisplayName: name,
			Provider:    resolved.Provider,
			Model:       resolved.Model,
			Workspace:   resolved.Workspace,
			IsDefault:   id == defaultID,
		})
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(entries) == 0 {
		fmt.Println("No agents configured.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDISPLAY NAME\tPROVIDER\tMODEL\tDEFAULT")
	for _, e := range entries {
		def := ""
		if e.IsDefault {
			def = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.ID, e.DisplayName, e.Provider, e.Model, def)
	}
	w.Flush()
}

// --- agent add ---

func agentAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new agent (interactive wizard)",
		Run: func(cmd *cobra.Command, args []string) {
			runAgentAdd()
		},
	}
}

func runAgentAdd() {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		// Start with default config if no file exists
		if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
			cfg = config.Default()
		} else {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("── Add New Agent ──")
	fmt.Println()

	// Step 1: Agent name (with validation loop)
	var name string
	for {
		name, err = promptString("Agent name", "e.g. coder, researcher, assistant", "")
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
		if name == "" {
			fmt.Println("  Name is required.")
			continue
		}
		id := config.NormalizeAgentID(name)
		if id == config.DefaultAgentID {
			fmt.Printf("  %q is reserved.\n", config.DefaultAgentID)
			continue
		}
		if _, exists := cfg.Agents.List[id]; exists {
			fmt.Printf("  Agent %q already exists.\n", id)
			continue
		}
		break
	}

	agentID := config.NormalizeAgentID(name)
	if name != agentID {
		fmt.Printf("  Normalized ID: %s\n", agentID)
	}

	// Step 2: Display name
	displayName, err := promptString("Display name", "", name)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 3: Provider (optional override)
	providerOptions := []SelectOption[string]{
		{fmt.Sprintf("Inherit from defaults (%s)", cfg.Agents.Defaults.Provider), ""},
		{"OpenRouter", "openrouter"},
		{"Anthropic", "anthropic"},
		{"OpenAI", "openai"},
		{"Groq", "groq"},
		{"DeepSeek", "deepseek"},
		{"Gemini", "gemini"},
		{"Mistral", "mistral"},
	}

	providerChoice, err := promptSelect("Provider", providerOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 4: Model (optional override)
	modelPlaceholder := fmt.Sprintf("(inherit: %s)", cfg.Agents.Defaults.Model)
	model, err := promptString("Model (empty = inherit from defaults)", modelPlaceholder, "")
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 5: Workspace
	defaultWS := fmt.Sprintf("%s/%s", cfg.Agents.Defaults.Workspace, agentID)
	workspace, err := promptString("Workspace directory", "", defaultWS)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Build AgentSpec
	spec := config.AgentSpec{
		DisplayName: displayName,
		Provider:    providerChoice,
		Model:       model,
		Workspace:   workspace,
	}

	// Add to config
	if cfg.Agents.List == nil {
		cfg.Agents.List = make(map[string]config.AgentSpec)
	}
	cfg.Agents.List[agentID] = spec

	// Create workspace directory
	expandedWS := config.ExpandHome(workspace)
	if err := os.MkdirAll(expandedWS, 0755); err != nil {
		fmt.Printf("Warning: could not create workspace: %v\n", err)
	}

	// Save config (strip secrets like onboard does)
	savedProviders := cfg.Providers
	savedGwToken := cfg.Gateway.Token
	savedTgToken := cfg.Channels.Telegram.Token
	cfg.Providers = config.ProvidersConfig{}
	cfg.Gateway.Token = ""
	cfg.Channels.Telegram.Token = ""

	saveErr := config.Save(cfgPath, cfg)

	cfg.Providers = savedProviders
	cfg.Gateway.Token = savedGwToken
	cfg.Channels.Telegram.Token = savedTgToken

	if saveErr != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", saveErr)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Agent %q created successfully.\n", agentID)
	fmt.Printf("  Display name: %s\n", displayName)
	if providerChoice != "" {
		fmt.Printf("  Provider:     %s\n", providerChoice)
	} else {
		fmt.Printf("  Provider:     (inherit: %s)\n", cfg.Agents.Defaults.Provider)
	}
	if model != "" {
		fmt.Printf("  Model:        %s\n", model)
	} else {
		fmt.Printf("  Model:        (inherit: %s)\n", cfg.Agents.Defaults.Model)
	}
	fmt.Printf("  Workspace:    %s\n", workspace)
	fmt.Println()
	fmt.Println("Restart the gateway to activate this agent.")
}

// --- agent delete ---

func agentDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <agent-id>",
		Short: "Delete an agent",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runAgentDelete(args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation")
	return cmd
}

func runAgentDelete(rawID string, force bool) {
	agentID := config.NormalizeAgentID(rawID)

	if agentID == config.DefaultAgentID {
		fmt.Fprintf(os.Stderr, "Error: %q cannot be deleted (reserved).\n", config.DefaultAgentID)
		os.Exit(1)
	}

	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if _, exists := cfg.Agents.List[agentID]; !exists {
		fmt.Fprintf(os.Stderr, "Error: agent %q not found.\n", agentID)
		os.Exit(1)
	}

	if !force {
		confirmed, err := promptConfirm(fmt.Sprintf("Delete agent %q?", agentID), false)
		if err != nil || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
	}

	// Remove agent
	delete(cfg.Agents.List, agentID)

	// Remove bindings that reference this agent
	removedBindings := 0
	if len(cfg.Bindings) > 0 {
		filtered := make([]config.AgentBinding, 0, len(cfg.Bindings))
		for _, b := range cfg.Bindings {
			if config.NormalizeAgentID(b.AgentID) == agentID {
				removedBindings++
				continue
			}
			filtered = append(filtered, b)
		}
		cfg.Bindings = filtered
		if len(cfg.Bindings) == 0 {
			cfg.Bindings = nil
		}
	}

	// Save config (strip secrets)
	savedProviders := cfg.Providers
	savedGwToken := cfg.Gateway.Token
	savedTgToken := cfg.Channels.Telegram.Token
	cfg.Providers = config.ProvidersConfig{}
	cfg.Gateway.Token = ""
	cfg.Channels.Telegram.Token = ""

	saveErr := config.Save(cfgPath, cfg)

	cfg.Providers = savedProviders
	cfg.Gateway.Token = savedGwToken
	cfg.Channels.Telegram.Token = savedTgToken

	if saveErr != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", saveErr)
		os.Exit(1)
	}

	fmt.Printf("Agent %q deleted.\n", agentID)
	if removedBindings > 0 {
		fmt.Printf("Removed %d binding(s) that referenced this agent.\n", removedBindings)
	}
	fmt.Println("Restart the gateway to apply changes.")
}
