package skills

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// PipUpdateExecutor implements UpdateExecutor for the "pip" source.
// It upgrades a single package via `pip3 install --upgrade ...`.
// Thread-safe: no mutable state; concurrent package serialization is handled
// upstream by PackageLocker (injected via UpdateRegistry.Apply).
type PipUpdateExecutor struct{}

// NewPipUpdateExecutor returns a PipUpdateExecutor ready for use.
func NewPipUpdateExecutor() *PipUpdateExecutor { return &PipUpdateExecutor{} }

// Source returns "pip".
func (e *PipUpdateExecutor) Source() string { return "pip" }

// Update upgrades `name` to `toVersion` using pip3.
//
// Argument ordering matches UpdateExecutor interface: (ctx, name, toVersion, meta).
// `name` is validated via ValidatePipPackageName before any exec.
// `--pre` is appended when meta["preRelease"]==true OR IsPipPreRelease(toVersion).
// On success, cleanCaches is called for symmetry with dep_installer.go.
// On failure, stderr is classified via ClassifyPipStderr and a wrapped sentinel is returned.
func (e *PipUpdateExecutor) Update(ctx context.Context, name, toVersion string, meta map[string]any) error {
	if err := ValidatePipPackageName(name); err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	args := []string{
		"install", "--upgrade",
		"--no-cache-dir", "--break-system-packages",
		"--upgrade-strategy", "only-if-needed",
	}

	// Determine whether pre-release flag is needed.
	preRelease := false
	if meta != nil {
		if v, ok := meta["preRelease"].(bool); ok && v {
			preRelease = true
		}
	}
	if !preRelease && IsPipPreRelease(toVersion) {
		preRelease = true
	}
	if preRelease {
		args = append(args, "--pre")
	}
	args = append(args, name)

	cmd := exec.CommandContext(cctx, pipBinary, args...)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	durationMs := time.Since(start).Milliseconds()

	if runErr != nil {
		sentinel, reason := ClassifyPipStderr(stderr.String())
		if sentinel == nil {
			sentinel = fmt.Errorf("pip install failed: %w", runErr)
		}
		slog.Warn("package.update.pip.outcome",
			"name", name,
			"status", "failed",
			"err_class", fmt.Sprintf("%T:%v", sentinel, sentinel),
			"reason", reason,
			"duration_ms", durationMs)
		return fmt.Errorf("%w: %s", sentinel, reason)
	}

	// Success path: purge caches for disk symmetry with dep_installer.go.
	cleanCaches(cctx)

	slog.Info("package.update.pip.outcome",
		"name", name,
		"to", toVersion,
		"status", "success",
		"duration_ms", durationMs)
	return nil
}
