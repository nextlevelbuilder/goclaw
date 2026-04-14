//go:build !langsmith

package cmd

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// initLangSmithExporter is a no-op when built without the "langsmith" tag.
// Build with `go build -tags langsmith` to enable LangSmith export.
func initLangSmithExporter(_ context.Context, _ *config.Config, _ *tracing.Collector) {
}
