// mcp-sandbox-agent is a standalone MCP server that exposes goclaw sub-agent
// tools (feishu form fill, opencode coding agent, agentfield) over MCP. Each
// tool talks to its own downstream control plane / sandbox broker; this binary
// no longer manages docker containers directly.
//
// Usage:
//
//	mcp-sandbox-agent [--addr :8090]
//
// Environment variables:
//
//	MCP_ADDR listen address (default ":8090")
//
// Register http://<host>:8090/mcp in the GoClaw UI as an MCP server so the
// task management agent can discover the registered tools.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
)

var version = "1.0.0"

func main() {
	addr := flag.String("addr", envOrDefault("MCP_ADDR", ":8090"), "listen address")
	flag.Parse()

	srv := mcpbridge.NewSandboxAgentServer(version)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"mcp-sandbox-agent"}`))
	})
	mux.Handle("/mcp", srv)
	mux.Handle("/mcp/", srv)

	httpSrv := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		slog.Info("mcp-sandbox-agent: shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(ctx) //nolint:errcheck
	}()

	slog.Info("mcp-sandbox-agent: listening", "addr", *addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("mcp-sandbox-agent: server error", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
