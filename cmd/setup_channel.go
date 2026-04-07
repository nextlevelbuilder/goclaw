package cmd

import (
	"fmt"
	"os"
)

// setupChannelStep optionally guides the user through channel setup.
func setupChannelStep() {
	fmt.Println("── Step 3: Channel (optional) ──")
	fmt.Println()

	setup, err := promptConfirm("Set up a messaging channel?", false)
	if err != nil || !setup {
		fmt.Println("  Skipped.")
		fmt.Println()
		return
	}

	typeOptions := []SelectOption[string]{
		{"Telegram", "telegram"},
		{"Discord", "discord"},
		{"Slack", "slack"},
	}
	channelType, err := promptSelect("Channel type", typeOptions, 0)
	if err != nil {
		return
	}

	name, err := promptString("Instance name", "", channelType+"-bot")
	if err != nil {
		return
	}

	// Credentials per type
	creds := map[string]string{}
	switch channelType {
	case "telegram":
		token, err := promptPassword("Bot token", "from @BotFather")
		if err != nil || token == "" {
			return
		}
		creds["token"] = token
	case "discord":
		token, err := promptPassword("Bot token", "from Discord Developer Portal")
		if err != nil || token == "" {
			return
		}
		creds["token"] = token
	case "slack":
		token, err := promptPassword("Bot token", "xoxb-...")
		if err != nil || token == "" {
			return
		}
		creds["token"] = token
		secret, err := promptPassword("Signing secret", "")
		if err != nil || secret == "" {
			return
		}
		creds["signing_secret"] = secret
	}

	// Bind to agent
	agents, err := fetchAgentList()
	if err != nil || len(agents) == 0 {
		fmt.Fprintf(os.Stderr, "  No agents found. Create an agent first.\n")
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
		return
	}

	body := map[string]any{
		"name":         name,
		"channel_type": channelType,
		"agent_id":     agentID,
		"enabled":      true,
		"credentials":  creds,
	}

	_, err = gatewayHTTPPost("/v1/channels/instances", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
		return
	}

	fmt.Printf("  Channel %q (%s) created.\n\n", name, channelType)
	fmt.Println("  Note: For Zalo, Feishu, WhatsApp — use the Web Dashboard.")
	fmt.Println()
}
