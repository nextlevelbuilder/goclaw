//go:build !prometheus

package cmd

import (
	"context"
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// wirePrometheusMetrics is a no-op when built without the "prometheus" tag.
// Build with `go build -tags prometheus` to serve a Prometheus /metrics
// endpoint on the configured telemetry.prometheus_port.
func wirePrometheusMetrics(_ context.Context, _ *config.Config, _ *sql.DB) (func(), error) {
	return nil, nil
}
