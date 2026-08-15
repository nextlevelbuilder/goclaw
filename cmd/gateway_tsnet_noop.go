//go:build !tsnet

package cmd

import (
	"context"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// initTailscale is a no-op when built without the "tsnet" tag.
// Build with `go build -tags tsnet` to enable Tailscale listener.
func initTailscale(_ context.Context, _ *config.Config, _ http.Handler) func() {
	return nil
}
