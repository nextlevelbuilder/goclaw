package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "View and manage chat sessions",
	}
	cmd.AddCommand(sessionsListCmd())
	cmd.AddCommand(sessionsDeleteCmd())
	cmd.AddCommand(sessionsResetCmd())
	return cmd
}

func sessionsListCmd() *cobra.Command {
	var jsonOutput bool
	var agentFilter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		Run: func(cmd *cobra.Command, args []string) {
			sessionsListRPC(agentFilter, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().StringVar(&agentFilter, "agent", "", "filter by agent ID")
	return cmd
}

func sessionsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [key]",
		Short: "Delete a session",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sessionsDeleteRPC(args[0])
		},
	}
}

func sessionsResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [key]",
		Short: "Clear session history (keep session)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			sessionsResetRPC(args[0])
		},
	}
}

// --- RPC implementations ---

func sessionsListRPC(agentFilter string, jsonOutput bool) {
	requireGateway()

	params, _ := json.Marshal(map[string]string{"agentId": agentFilter})
	resp, err := gatewayRPC(protocol.MethodSessionsList, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}

	// Parse response payload → []store.SessionInfo
	raw, _ := json.Marshal(resp.Payload)
	var result struct {
		Sessions []store.SessionInfo `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	printSessionInfos(result.Sessions, jsonOutput)
}

func sessionsDeleteRPC(key string) {
	requireGateway()

	params, _ := json.Marshal(map[string]string{"key": key})
	resp, err := gatewayRPC(protocol.MethodSessionsDelete, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}
	fmt.Printf("Deleted session: %s\n", key)
}

func sessionsResetRPC(key string) {
	requireGateway()

	params, _ := json.Marshal(map[string]string{"key": key})
	resp, err := gatewayRPC(protocol.MethodSessionsReset, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}
	fmt.Printf("Reset session: %s\n", key)
}

// --- Shared display ---

func printSessionInfos(infos []store.SessionInfo, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.MarshalIndent(infos, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(infos) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "KEY\tMESSAGES\tCREATED\tUPDATED\n")
	for _, s := range infos {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n",
			truncateStr(s.Key, 50),
			s.MessageCount,
			s.Created.Format(time.DateTime),
			s.Updated.Format(time.DateTime),
		)
	}
	tw.Flush()
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
