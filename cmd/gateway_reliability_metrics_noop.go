//go:build !otel

package cmd

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// wireReliabilityMetrics is a no-op when built without the "otel" tag. Build
// with `go build -tags otel` to export reliability counters via OTLP.
func wireReliabilityMetrics(_ context.Context, _ *config.Config) (func(), error) {
	return nil, nil
}
