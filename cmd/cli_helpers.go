package cmd

import "net/http"

// requireGateway exits with a helpful error if the gateway is not reachable.
// Uses HTTP /health endpoint (faster and doesn't require WS handshake).
func requireGateway() {
	requireRunningGatewayHTTP()
}

// isGatewayReachable checks if the gateway is up via HTTP health endpoint.
func isGatewayReachable() bool {
	base := resolveGatewayBaseURL()
	resp, err := healthClient.Get(base + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
