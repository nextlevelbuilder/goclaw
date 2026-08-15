package cmd

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
)

func agentChatCmd() *cobra.Command {
	var (
		agentName  string
		message    string
		sessionKey string
	)

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Chat with an agent interactively or send a one-shot message",
		Long: `Chat with an agent via the running gateway (WebSocket client mode).

Examples:
  goclaw agent chat                          # Interactive REPL
  goclaw agent chat --name coder             # Chat with "coder" agent
  goclaw agent chat -m "What time is it?"    # One-shot message
  goclaw agent chat -s my-session            # Continue a session`,
		Run: func(cmd *cobra.Command, args []string) {
			runAgentChat(agentName, message, sessionKey)
		},
	}

	cmd.Flags().StringVarP(&agentName, "name", "n", "default", "agent name")
	cmd.Flags().StringVarP(&message, "message", "m", "", "one-shot message (omit for interactive mode)")
	cmd.Flags().StringVarP(&sessionKey, "session", "s", "", "session key (default: auto-generated)")

	return cmd
}

func runAgentChat(agentName, message, sessionKey string) {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Default session key
	if sessionKey == "" {
		sessionKey = sessions.BuildSessionKey(agentName, "cli", sessions.PeerDirect, "local")
	}

	// Try client mode first (connect to running gateway)
	host := cfg.Gateway.Host
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, cfg.Gateway.Port)

	if !isGatewayRunning(addr) {
		fmt.Fprintln(os.Stderr, "Error: the gateway must be running for this command.")
		fmt.Fprintln(os.Stderr, "Start it first:  goclaw")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Connected to gateway at %s\n", addr)
	runClientMode(cfg, addr, agentName, message, sessionKey)
}

// --- Gateway detection ---

func isGatewayRunning(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
