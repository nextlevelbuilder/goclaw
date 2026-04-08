package cmd

import (
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/nodehost"
)

func nodeRunCmd() *cobra.Command {
	var (
		host           string
		port           int
		tls            bool
		tlsFingerprint string
		displayName    string
		nodeID         string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run node-host in foreground",
		Long:  "Connect to the gateway and handle system.run commands. Blocks until interrupted.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nodehost.RunNodeHost(cmd.Context(), nodehost.NodeHostRunOptions{
				GatewayHost:           host,
				GatewayPort:           port,
				GatewayTLS:            tls,
				GatewayTLSFingerprint: tlsFingerprint,
				DisplayName:           displayName,
				NodeID:                nodeID,
			})
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "Gateway host address")
	cmd.Flags().IntVar(&port, "port", 18789, "Gateway port")
	cmd.Flags().BoolVar(&tls, "tls", false, "Use TLS (wss://)")
	cmd.Flags().StringVar(&tlsFingerprint, "tls-fingerprint", "", "TLS certificate fingerprint for pinning")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Node display name (default: hostname)")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "Override node ID (default: from config)")

	return cmd
}
