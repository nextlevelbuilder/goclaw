//go:build tsnet

package cmd

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"tailscale.com/tsnet"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// initTailscale starts an additional Tailscale listener alongside the main gateway.
// Only compiled with -tags tsnet. The listener shares the same http.Handler (mux).
func initTailscale(ctx context.Context, cfg *config.Config, mux http.Handler) func() {
	tc := cfg.Tailscale
	if tc.Hostname == "" {
		slog.Debug("Tailscale available but not configured (set GOCLAW_TSNET_HOSTNAME to enable)")
		return nil
	}

	srv := &tsnet.Server{
		Hostname:  tc.Hostname,
		AuthKey:   tc.AuthKey,
		Ephemeral: tc.Ephemeral,
	}
	if tc.StateDir != "" {
		srv.Dir = tc.StateDir
	}

	var (
		ln  net.Listener
		err error
	)

	if tc.EnableTLS {
		ln, err = srv.ListenTLS("tcp", ":443")
	} else {
		ln, err = srv.Listen("tcp", ":80")
	}
	if err != nil {
		slog.Warn("Tailscale listener failed to start", "error", err)
		return nil
	}

	port := ":80"
	if tc.EnableTLS {
		port = ":443 (TLS)"
	}
	slog.Info("Tailscale listener started",
		"hostname", tc.Hostname,
		"port", port,
	)

	httpSrv := &http.Server{Handler: mux}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("Tailscale HTTP server error", "error", err)
		}
	}()

	// Graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	return func() {
		httpSrv.Close()
		ln.Close()
		srv.Close()
		slog.Info("Tailscale listener stopped")
	}
}
