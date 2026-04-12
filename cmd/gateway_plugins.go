package cmd

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/plugins"
	pluginhooks "github.com/nextlevelbuilder/goclaw/internal/plugins/hooks"
)

func setupPlugins(ctx context.Context, cfg *config.Config, workspace, dataDir string) (*plugins.Registry, *pluginhooks.Executor) {
	registry := plugins.NewRegistry()
	executor := pluginhooks.NewExecutor(pluginHookProvider{registry: registry})

	if cfg == nil || len(cfg.Plugins.Declarations) == 0 {
		return registry, executor
	}

	declared := make([]plugins.PluginDeclaration, 0, len(cfg.Plugins.Declarations))
	for _, decl := range cfg.Plugins.Declarations {
		if decl.Name == "" || decl.Source == "" {
			continue
		}

		sourceType := pluginSourceType(decl.Source)
		if err := plugins.ValidatePluginSource(
			sourceType,
			cfg.Plugins.BlockedSources,
			cfg.Plugins.StrictKnownSources,
			[]string{"local", "git", "marketplace"},
		); err != nil {
			slog.Warn("plugin declaration skipped", "name", decl.Name, "source", decl.Source, "error", err)
			continue
		}

		source := decl.Source
		if sourceType == "local" {
			localPath := strings.TrimPrefix(source, "local:")
			if localPath == source {
				localPath = source
			}
			if !filepath.IsAbs(localPath) {
				localPath = filepath.Join(workspace, localPath)
			}
			source = "local:" + filepath.Clean(localPath)
		}

		declared = append(declared, plugins.PluginDeclaration{
			Name:    decl.Name,
			Source:  source,
			Version: decl.Version,
			Enabled: decl.Enabled,
		})
	}

	reconciler := plugins.NewReconciler(registry, filepath.Join(dataDir, "plugins"))
	if err := reconciler.Reconcile(ctx, declared); err != nil {
		slog.Warn("plugin reconciliation failed", "error", err)
	} else {
		slog.Info("plugins reconciled", "declared", len(declared), "loaded", registry.Count())
	}

	return registry, executor
}

type pluginHookProvider struct {
	registry *plugins.Registry
}

func (p pluginHookProvider) HooksFor(event string) []pluginhooks.HookDef {
	if p.registry == nil {
		return nil
	}
	hooks := p.registry.HooksFor(event)
	defs := make([]pluginhooks.HookDef, 0, len(hooks))
	for _, hook := range hooks {
		defs = append(defs, pluginhooks.HookDef{
			MatchTool: hook.MatchTool,
			Command:   hook.Command,
			Timeout:   hook.Timeout,
		})
	}
	return defs
}

func pluginSourceType(source string) string {
	switch {
	case strings.HasPrefix(source, "local:"):
		return "local"
	case strings.HasPrefix(source, "git:"):
		return "git"
	case strings.HasPrefix(source, "marketplace:"):
		return "marketplace"
	default:
		return "local"
	}
}
