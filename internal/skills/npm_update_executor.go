package skills

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// NpmUpdateExecutor implements UpdateExecutor for the "npm" source.
// It upgrades a single global npm package via `npm install --global <name>@<version>`.
// Thread-safe: no mutable state; concurrent package serialization is handled
// upstream by PackageLocker (injected via UpdateRegistry.Apply).
type NpmUpdateExecutor struct{}

// NewNpmUpdateExecutor returns an NpmUpdateExecutor ready for use.
func NewNpmUpdateExecutor() *NpmUpdateExecutor { return &NpmUpdateExecutor{} }

// Source returns "npm".
func (e *NpmUpdateExecutor) Source() string { return "npm" }

// Update upgrades `name` to `toVersion` using npm install --global.
//
// Argument ordering matches UpdateExecutor interface: (ctx, name, toVersion, meta).
// `name` is validated via ValidateNpmPackageName before any exec.
// `toVersion` must be non-empty — callers must pass the exact version string
// from UpdateInfo.LatestVersion; using "@latest" or "@next" is explicitly forbidden
// to prevent registry-swap attacks and non-deterministic upgrades (P2A-H4).
// On success, cleanCaches is called for symmetry with dep_installer.go.
// On failure, stderr is classified via ClassifyNpmStderr and a wrapped sentinel is returned.
func (e *NpmUpdateExecutor) Update(ctx context.Context, name, toVersion string, meta map[string]any) error {
	if err := ValidateNpmPackageName(name); err != nil {
		return err
	}
	if toVersion == "" {
		return fmt.Errorf("npm update: toVersion required (never use @latest/@next tags)")
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Construct the install target as a single argv token: <name>@<version>.
	// This is safe — ValidateNpmPackageName rejects names containing "@version"
	// suffixes, so the only "@" in the token is our version separator.
	target := name + "@" + toVersion

	cmd := exec.CommandContext(cctx, npmBinary, "install", "--global", target)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	durationMs := time.Since(start).Milliseconds()

	if runErr != nil {
		sentinel, reason := ClassifyNpmStderr(stderr.String())
		if sentinel == nil {
			sentinel = fmt.Errorf("npm install failed: %w", runErr)
		}
		slog.Warn("package.update.npm.outcome",
			"name", name,
			"status", "failed",
			"err_class", fmt.Sprintf("%T:%v", sentinel, sentinel),
			"reason", reason,
			"duration_ms", durationMs)
		return fmt.Errorf("%w: %s", sentinel, reason)
	}

	// Success path: purge caches for disk symmetry with dep_installer.go (P2A-M3).
	cleanCaches(cctx)

	slog.Info("package.update.npm.outcome",
		"name", name,
		"to", toVersion,
		"status", "success",
		"duration_ms", durationMs)
	return nil
}
