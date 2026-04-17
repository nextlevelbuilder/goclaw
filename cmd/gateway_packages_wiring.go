package cmd

import (
	"log/slog"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// wirePackagesHandler constructs the UpdateRegistry and wires it into
// PackagesHandler together with the gateway's event publisher.
//
// Called after initGitHubInstaller() so DefaultGitHubInstaller() is non-nil.
// If the installer is not configured (e.g. in integration-test stubs), returns
// a handler with nil registry — the update endpoints return 503.
func wirePackagesHandler(d *gatewayDeps) *httpapi.PackagesHandler {
	installer := skills.DefaultGitHubInstaller()
	if installer == nil {
		slog.Warn("packages: github installer not configured; update endpoints disabled")
		return httpapi.NewPackagesHandler(nil, d.msgBus)
	}

	// Cache file lives next to the manifest dir so it shares the same atomic-
	// write guarantees on the same filesystem (no cross-device rename risk).
	cachePath := filepath.Join(filepath.Dir(installer.Config.ManifestPath), "updates-cache.json")

	cache, err := skills.LoadUpdateCache(cachePath)
	if err != nil {
		// ErrUpdateCacheCorrupt — log and proceed with an empty cache; a
		// background refresh will repopulate on first GET /v1/packages/updates.
		slog.Warn("packages: update cache corrupt; starting fresh", "path", cachePath, "error", err)
	}

	ttl := d.cfg.Packages.UpdatesCheckTTLDuration()
	registry := skills.NewUpdateRegistry(cache, cachePath, ttl)

	// Share the installer's locker so Install and Update share per-package locks.
	registry.Locker = installer.Locker
	skills.SetSharedPackageLocker(registry.Locker)

	// Register checker + executor for "github" source.
	registry.RegisterChecker(skills.NewGitHubUpdateChecker(installer))

	executor := skills.NewGitHubUpdateExecutor(installer)
	if d.cfg.Packages.ScratchDir != "" {
		executor.ScratchDir = d.cfg.Packages.ScratchDir
	}
	registry.RegisterExecutor(executor)

	// Register pip + npm checkers/executors when the edition supports them.
	if edition.Current().SupportsPipNpm {
		registry.RegisterChecker(skills.NewPipUpdateChecker())
		registry.RegisterExecutor(skills.NewPipUpdateExecutor())
		registry.RegisterChecker(skills.NewNpmUpdateChecker())
		registry.RegisterExecutor(skills.NewNpmUpdateExecutor())
	}

	slog.Info("packages: update registry wired",
		"cache", cachePath,
		"ttl", ttl,
		"sources", registry.Sources(),
	)

	return httpapi.NewPackagesHandler(registry, d.msgBus)
}
