package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// resolveGatewayBaseURL reads host/port from config and returns http://host:port.
func resolveGatewayBaseURL() string {
	if base := firstNonEmpty(gatewayServerOverride, os.Getenv("GOCLAW_SERVER"), os.Getenv("GOCLAW_GATEWAY_URL")); base != "" {
		return normalizeGatewayBaseURL(base)
	}

	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return "http://127.0.0.1:18790"
	}
	host := cfg.Gateway.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := cfg.Gateway.Port
	if port == 0 {
		port = 18790
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// resolveGatewayToken returns the gateway auth token.
// Priority: --token flag -> GOCLAW_GATEWAY_TOKEN env -> config file token.
func resolveGatewayToken() string {
	if t := strings.TrimSpace(gatewayTokenOverride); t != "" {
		return t
	}
	if t := os.Getenv("GOCLAW_GATEWAY_TOKEN"); t != "" {
		return t
	}
	cfg, _ := config.Load(resolveConfigPath())
	if cfg != nil {
		return cfg.Gateway.Token
	}
	return ""
}

func resolveGatewayWebSocketURL() (string, error) {
	baseURL := resolveGatewayBaseURL()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse gateway URL %q: %w", baseURL, err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported gateway URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ws"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func normalizeGatewayBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return strings.TrimRight(base, "/")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
