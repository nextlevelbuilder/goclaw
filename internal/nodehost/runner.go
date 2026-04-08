package nodehost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// NodeHostRunOptions configures the node host runner.
type NodeHostRunOptions struct {
	GatewayHost           string
	GatewayPort           int
	GatewayTLS            bool
	GatewayTLSFingerprint string
	NodeID                string
	DisplayName           string
}

// NodeHostCommands lists the RPC commands this node supports.
var NodeHostCommands = []string{
	"system.run",
	"system.run.prepare",
	"system.which",
}

// RunNodeHost is the main entry point for the node host service.
// It connects to the gateway via WebSocket and handles RPC commands.
func RunNodeHost(ctx context.Context, opts NodeHostRunOptions) error {
	config, err := EnsureNodeHostConfig()
	if err != nil {
		return fmt.Errorf("ensure node config: %w", err)
	}

	nodeID := firstNonEmpty(opts.NodeID, config.NodeID)
	if nodeID != config.NodeID {
		config.NodeID = nodeID
	}
	displayName := firstNonEmpty(opts.DisplayName, config.DisplayName, hostname())
	config.DisplayName = displayName

	// Save updated config with gateway info.
	config.Gateway = &NodeHostGatewayConfig{
		Host:           opts.GatewayHost,
		Port:           opts.GatewayPort,
		TLS:            opts.GatewayTLS,
		TLSFingerprint: opts.GatewayTLSFingerprint,
	}
	if err := SaveNodeHostConfig(config); err != nil {
		slog.Warn("failed to save node config", "err", err)
	}

	// Resolve credentials from env.
	creds := resolveGatewayCredentials()

	// Build WS URL.
	scheme := "ws"
	if opts.GatewayTLS {
		scheme = "wss"
	}
	host := opts.GatewayHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := opts.GatewayPort
	if port == 0 {
		port = 18789
	}
	wsURL := fmt.Sprintf("%s://%s:%d/ws", scheme, host, port)

	pathEnv := ensureNodePathEnv()
	skillBinsFetch := func(_ context.Context) ([]string, error) {
		return nil, nil // real impl would call skills.bins RPC
	}
	skillBins := NewSkillBinsCache(skillBinsFetch, pathEnv)

	// Setup graceful shutdown.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("node host starting", "url", wsURL, "nodeId", nodeID, "displayName", displayName)

	// Connect loop with reconnect.
	return runWithReconnect(ctx, wsURL, creds, nodeID, displayName, skillBins)
}

// GatewayCredentials holds authentication credentials.
type GatewayCredentials struct {
	Token    string
	Password string
}

func resolveGatewayCredentials() GatewayCredentials {
	return GatewayCredentials{
		Token:    strings.TrimSpace(os.Getenv("GOCLAW_GATEWAY_TOKEN")),
		Password: strings.TrimSpace(os.Getenv("GOCLAW_GATEWAY_PASSWORD")),
	}
}

func ensureNodePathEnv() string {
	current := os.Getenv("PATH")
	if strings.TrimSpace(current) != "" {
		return current
	}
	fallback := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	os.Setenv("PATH", fallback)
	return fallback
}

// --- WebSocket connect loop ---

func runWithReconnect(ctx context.Context, wsURL string, creds GatewayCredentials, nodeID, displayName string, skillBins *SkillBinsCache) error {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		start := time.Now()
		err := connectAndRun(ctx, wsURL, creds, nodeID, displayName, skillBins)
		if ctx.Err() != nil {
			slog.Info("node host shutting down")
			return ctx.Err()
		}
		// Reset backoff if the connection lasted more than 60s (was healthy).
		if time.Since(start) > 60*time.Second {
			backoff = time.Second
		}
		slog.Warn("node host disconnected, reconnecting", "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func connectAndRun(ctx context.Context, wsURL string, creds GatewayCredentials, nodeID, displayName string, skillBins *SkillBinsCache) error {
	u, err := url.Parse(wsURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	client := &wsClient{conn: conn, mu: sync.Mutex{}}

	// Send connect request — gateway expects type="req" with method="connect".
	connectParams := map[string]any{
		"token":             creds.Token,
		"instanceId":        nodeID,
		"clientName":        "node-host",
		"clientDisplayName": displayName,
		"platform":          runtime.GOOS,
		"mode":              "node",
		"role":              "node",
		"caps":              []string{"system"},
		"commands":          NodeHostCommands,
	}
	if creds.Password != "" {
		connectParams["password"] = creds.Password
	}
	connectFrame := map[string]any{
		"type":   "req",
		"id":     "connect-1",
		"method": "connect",
		"params": connectParams,
	}
	if err := client.SendJSON(connectFrame); err != nil {
		return fmt.Errorf("send connect: %w", err)
	}

	slog.Info("node host connected", "url", wsURL)

	// Read message loop.
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		go handleWSMessage(ctx, message, client, skillBins)
	}
}

func handleWSMessage(ctx context.Context, message []byte, client *wsClient, skillBins *SkillBinsCache) {
	var frame struct {
		Type    string          `json:"type"`
		Event   string          `json:"event"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(message, &frame) != nil {
		return
	}
	if frame.Type != "event" || frame.Event != "node.invoke.request" {
		return
	}
	payload := CoerceNodeInvokePayload(frame.Payload)
	if payload == nil {
		slog.Warn("invalid node invoke payload")
		return
	}
	HandleInvoke(ctx, *payload, client, skillBins)
}

// --- wsClient implements GatewayRequester ---

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsClient) SendJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(v)
}

func (c *wsClient) Request(ctx context.Context, method string, params any) error {
	frame := map[string]any{
		"type":   "req",
		"method": method,
		"params": params,
	}
	return c.SendJSON(frame)
}

// --- Helpers ---

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func hostname() string {
	name, _ := os.Hostname()
	if name == "" {
		return "node"
	}
	return name
}
