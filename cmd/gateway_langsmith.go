//go:build langsmith

package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/internal/tracing/langsmithexport"
)

// initLangSmithExporter creates and wires the LangSmith exporter when
// the API key is configured. Only compiled with -tags langsmith.
func initLangSmithExporter(_ context.Context, cfg *config.Config, collector *tracing.Collector) {
	if collector == nil {
		return
	}
	if cfg.LangSmith.APIKey == "" {
		slog.Debug("LangSmith export available but not enabled (set LANGSMITH_API_KEY)")
		return
	}

	exp, err := langsmithexport.New(langsmithexport.Config{
		APIKey:  cfg.LangSmith.APIKey,
		Project: cfg.LangSmith.Project,
		APIUrl:  cfg.LangSmith.APIUrl,
	})
	if err != nil {
		slog.Warn("failed to create LangSmith exporter", "error", err)
		return
	}

	collector.AddExporter(exp)

	project := cfg.LangSmith.Project
	if project == "" {
		project = "default"
	}
	slog.Info("LangSmith export enabled", "project", project)
}
