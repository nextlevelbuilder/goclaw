package cmd

import (
	"fmt"
	"os"
)

// requireGateway exits with a helpful error if the gateway is not reachable.
func requireGateway() {
	if !isGatewayReachable() {
		fmt.Fprintln(os.Stderr, "Error: the gateway must be running for this command.")
		fmt.Fprintln(os.Stderr, "Start it first:  goclaw")
		os.Exit(1)
	}
}

// isGatewayReachable tries a quick RPC ping to check if the gateway is up.
func isGatewayReachable() bool {
	_, err := gatewayRPC("ping", nil)
	// Any response (even error) means the gateway is up.
	// Only connection failure means it's down.
	return err == nil
}
