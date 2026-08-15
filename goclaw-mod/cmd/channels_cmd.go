package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func channelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage messaging channels (requires running gateway)",
	}
	cmd.AddCommand(channelsListCmd())
	cmd.AddCommand(channelsAddCmd())
	cmd.AddCommand(channelsDeleteCmd())
	return cmd
}

// httpChannelInstance is the CLI-side representation of a channel instance from the HTTP API.
type httpChannelInstance struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
	AgentID     string `json:"agent_id"`
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
}

func channelsListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List channel instances",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()

			resp, err := gatewayHTTPGet("/v1/channels/instances")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			raw, _ := json.Marshal(resp["instances"])
			var instances []httpChannelInstance
			if err := json.Unmarshal(raw, &instances); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(instances, "", "  ")
				fmt.Println(string(data))
				return
			}

			if len(instances) == 0 {
				fmt.Println("No channel instances configured.")
				return
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "ID\tNAME\tTYPE\tENABLED\tSTATUS\n")
			for _, inst := range instances {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%s\n",
					inst.ID, inst.Name, inst.ChannelType, inst.Enabled, inst.Status)
			}
			tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func channelsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new channel instance (interactive)",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runChannelsAdd()
		},
	}
}

func runChannelsAdd() {
	fmt.Println("── Add Channel Instance ──")
	fmt.Println()

	// Step 1: Channel type
	typeOptions := []SelectOption[string]{
		{"Telegram", "telegram"},
		{"Discord", "discord"},
		{"Slack", "slack"},
	}
	channelType, err := promptSelect("Channel type", typeOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 2: Name
	name, err := promptString("Instance name", "e.g. my-telegram-bot", channelType+"-bot")
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Step 3: Credentials per type
	creds := map[string]string{}
	switch channelType {
	case "telegram":
		token, err := promptPassword("Bot token", "from @BotFather")
		if err != nil || token == "" {
			fmt.Println("Cancelled.")
			return
		}
		creds["token"] = token
	case "discord":
		token, err := promptPassword("Bot token", "from Discord Developer Portal")
		if err != nil || token == "" {
			fmt.Println("Cancelled.")
			return
		}
		creds["token"] = token
	case "slack":
		token, err := promptPassword("Bot token", "xoxb-...")
		if err != nil || token == "" {
			fmt.Println("Cancelled.")
			return
		}
		creds["token"] = token
		secret, err := promptPassword("Signing secret", "from Slack app settings")
		if err != nil || secret == "" {
			fmt.Println("Cancelled.")
			return
		}
		creds["signing_secret"] = secret
	}

	// Step 4: Bind to agent
	agents, err := fetchAgentList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching agents: %v\n", err)
		os.Exit(1)
	}
	if len(agents) == 0 {
		fmt.Println("No agents found. Create an agent first with 'goclaw agent add'.")
		return
	}

	agentOptions := make([]SelectOption[string], len(agents))
	for i, a := range agents {
		agentOptions[i] = SelectOption[string]{
			Label: fmt.Sprintf("%s (%s)", a.AgentKey, a.DisplayName),
			Value: a.ID,
		}
	}
	agentID, err := promptSelect("Bind to agent", agentOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Create via HTTP API
	body := map[string]any{
		"name":         name,
		"channel_type": channelType,
		"agent_id":     agentID,
		"enabled":      true,
		"credentials":  creds,
	}

	_, err = gatewayHTTPPost("/v1/channels/instances", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating channel: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nChannel %q (%s) created and bound to agent.\n", name, channelType)
	fmt.Println("Note: For Zalo, Feishu, WhatsApp — use the Web Dashboard.")
}

func channelsDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a channel instance",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			if !force {
				confirmed, err := promptConfirm(fmt.Sprintf("Delete channel %q?", args[0]), false)
				if err != nil || !confirmed {
					fmt.Println("Cancelled.")
					return
				}
			}
			if err := gatewayHTTPDelete("/v1/channels/instances/" + url.PathEscape(args[0])); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Channel %q deleted.\n", args[0])
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation")
	return cmd
}

// fetchAgentList returns agents from the gateway for use in selection prompts.
func fetchAgentList() ([]httpAgent, error) {
	resp, err := gatewayHTTPGet("/v1/agents")
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(resp["agents"])
	var agents []httpAgent
	if err := json.Unmarshal(raw, &agents); err != nil {
		return nil, fmt.Errorf("parse agents: %w", err)
	}
	return agents, nil
}
