package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// pipBinary is the pip3 executable name. Tests override this to inject a
// fixture script without touching PATH globally.
var pipBinary = "pip3"

// pipLookPath is exec.LookPath by default; tests override to simulate pip3-absent systems.
var pipLookPath = exec.LookPath

// PipUpdateChecker implements UpdateChecker for the "pip" source.
// It enumerates outdated packages via `pip3 list --outdated --format json`.
// Thread-safe: no mutable state; test hooks (pipBinary/pipLookPath) are
// package-level vars that MUST only be mutated from single-goroutine test setup.
type PipUpdateChecker struct{}

// NewPipUpdateChecker returns a PipUpdateChecker ready for use.
func NewPipUpdateChecker() *PipUpdateChecker { return &PipUpdateChecker{} }

// Source returns "pip".
func (c *PipUpdateChecker) Source() string { return "pip" }

// Check polls `pip3 list --outdated` and returns UpdateCheckResult.
//
// LookPath miss → Available:false, nil Err, empty Updates.
// Exec failure  → Available:true, Err set.
// Success       → Available:true, Updates populated.
//
// knownETags is ignored: pip has no ETag / conditional-fetch mechanism.
func (c *PipUpdateChecker) Check(ctx context.Context, knownETags map[string]string) UpdateCheckResult {
	start := time.Now()

	if _, err := pipLookPath(pipBinary); err != nil {
		slog.Info("package.update.pip.unavailable", "reason", "pip3 not found")
		return UpdateCheckResult{Source: "pip", Available: false}
	}

	// Primary call: stable packages only (no --pre).
	primary, err := c.runOutdated(ctx, false)
	if err != nil {
		return UpdateCheckResult{
			Source:    "pip",
			Available: true,
			Err:       fmt.Errorf("pip list --outdated: %w", err),
		}
	}

	// Detect pre-release currents — if any, run secondary call with --pre so
	// users on pre-release channels receive the best available upgrade target.
	hasPre := false
	for _, e := range primary {
		if IsPipPreRelease(e.Version) {
			hasPre = true
			break
		}
	}

	merged := primary
	if hasPre {
		secondary, serr := c.runOutdated(ctx, true)
		if serr == nil {
			merged = mergePipResults(primary, secondary)
		} else {
			slog.Warn("package.update.pip.check", "secondary_error", serr)
		}
	}

	infos := make([]UpdateInfo, 0, len(merged))
	for _, e := range merged {
		meta := map[string]any{"filetype": e.LatestFiletype}
		if IsPipPreRelease(e.Version) {
			meta["preRelease"] = true
		}
		infos = append(infos, UpdateInfo{
			Source:         "pip",
			Name:           e.Name,
			CurrentVersion: e.Version,
			LatestVersion:  e.LatestVersion,
			CheckedAt:      time.Now().UTC(),
			Meta:           meta,
		})
	}

	slog.Info("package.update.pip.check",
		"count", len(infos),
		"duration_ms", time.Since(start).Milliseconds())

	return UpdateCheckResult{Source: "pip", Available: true, Updates: infos}
}

// pipOutdatedEntry mirrors a single element from `pip3 list --outdated --format json`.
type pipOutdatedEntry struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	LatestVersion  string `json:"latest_version"`
	LatestFiletype string `json:"latest_filetype"`
}

// runOutdated executes `pip3 list --outdated --format json [--pre]` with a 30s
// timeout and parses the JSON response.
func (c *PipUpdateChecker) runOutdated(ctx context.Context, includePre bool) ([]pipOutdatedEntry, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"list", "--outdated", "--format", "json", "--break-system-packages"}
	if includePre {
		args = append(args, "--pre")
	}

	cmd := exec.CommandContext(cctx, pipBinary, args...)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("exec (stderr: %s): %w",
			truncateStderr(stderr.String(), 500), err)
	}

	var entries []pipOutdatedEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	return entries, nil
}

// mergePipResults unions primary and secondary results by package name.
// When the same name appears in both, the entry with the lexicographically
// higher latest_version string is kept. String comparison is sufficient for
// the pip ecosystem in Phase 2a; proper PEP 440 ordering is deferred.
func mergePipResults(primary, secondary []pipOutdatedEntry) []pipOutdatedEntry {
	idx := make(map[string]int, len(primary)+len(secondary))
	out := make([]pipOutdatedEntry, 0, len(primary)+len(secondary))

	add := func(e pipOutdatedEntry) {
		if existingIdx, ok := idx[e.Name]; ok {
			if e.LatestVersion > out[existingIdx].LatestVersion {
				out[existingIdx] = e
			}
			return
		}
		idx[e.Name] = len(out)
		out = append(out, e)
	}

	for _, e := range primary {
		add(e)
	}
	for _, e := range secondary {
		add(e)
	}
	return out
}
