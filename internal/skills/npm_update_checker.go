package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// npmBinary is the npm executable name. Tests override this to inject a fixture
// script without touching PATH globally.
var npmBinary = "npm"

// npmLookPath is exec.LookPath by default; tests override to simulate npm-absent systems.
var npmLookPath = exec.LookPath

// NpmUpdateChecker implements UpdateChecker for the "npm" source.
// It enumerates globally-outdated npm packages via `npm outdated --global --json`.
// Thread-safe: no mutable state; test hooks (npmBinary/npmLookPath) are
// package-level vars that MUST only be mutated from single-goroutine test setup.
type NpmUpdateChecker struct{}

// NewNpmUpdateChecker returns an NpmUpdateChecker ready for use.
func NewNpmUpdateChecker() *NpmUpdateChecker { return &NpmUpdateChecker{} }

// Source returns "npm".
func (c *NpmUpdateChecker) Source() string { return "npm" }

// npmOutdatedEntry mirrors a single value from `npm outdated --global --json`.
// The JSON object key is the package name; each value has these fields.
type npmOutdatedEntry struct {
	Current  string `json:"current"`
	Wanted   string `json:"wanted"`
	Latest   string `json:"latest"`
	Location string `json:"location,omitempty"`
	Type     string `json:"type,omitempty"`
}

// Check polls `npm outdated --global --json` and returns UpdateCheckResult.
//
// LookPath miss  → Available:false, nil Err, empty Updates.
// Exit 0         → Available:true, no updates (npm signals "nothing outdated" via exit 0).
// Exit 1 + JSON  → Available:true, Updates populated (npm exits 1 when outdated packages exist).
// Exit 1 + ERR!  → Available:true, Err set (real npm error in stderr).
// Exit 1 + empty → Available:true, no updates (ambiguous; treated as no-updates).
// Other exit     → Available:true, Err set.
//
// knownETags is ignored: npm has no ETag / conditional-fetch mechanism.
func (c *NpmUpdateChecker) Check(ctx context.Context, knownETags map[string]string) UpdateCheckResult {
	start := time.Now()

	if _, err := npmLookPath(npmBinary); err != nil {
		slog.Info("package.update.npm.unavailable", "reason", "npm not found")
		return UpdateCheckResult{Source: "npm", Available: false}
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, npmBinary, "outdated", "--global", "--json")
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			// Non-exit error: context cancel, binary gone post-LookPath, etc.
			return UpdateCheckResult{
				Source:    "npm",
				Available: true,
				Err:       fmt.Errorf("npm exec: %w", runErr),
			}
		}
		exitCode = ee.ExitCode()
	}

	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := stderr.String()
	hasNpmErr := strings.Contains(stderrStr, "npm ERR!")

	// Exit-code state machine per spec.
	switch {
	case exitCode == 0:
		// npm exits 0 when all global packages are up to date.
		return UpdateCheckResult{Source: "npm", Available: true}

	case exitCode == 1 && hasNpmErr:
		// Real npm error (ERESOLVE, network, permissions, …).
		return UpdateCheckResult{
			Source:    "npm",
			Available: true,
			Err:       fmt.Errorf("npm error: %s", truncateStderr(stderrStr, 500)),
		}

	case exitCode == 1 && stdoutStr == "" && stderrStr == "":
		// Ambiguous exit 1 with no output — treat as no-updates to avoid false positives.
		slog.Warn("package.update.npm.check", "ambiguous_exit_1", true)
		return UpdateCheckResult{Source: "npm", Available: true}

	case exitCode == 1 && stdoutStr != "" && stdoutStr != "{}":
		// Fall through to JSON parsing below.

	default:
		return UpdateCheckResult{
			Source:    "npm",
			Available: true,
			Err:       fmt.Errorf("npm outdated exit %d: %s", exitCode, truncateStderr(stderrStr, 500)),
		}
	}

	// Parse the JSON object: map[packageName]npmOutdatedEntry.
	var entries map[string]npmOutdatedEntry
	if err := json.Unmarshal([]byte(stdoutStr), &entries); err != nil {
		return UpdateCheckResult{
			Source:    "npm",
			Available: true,
			Err:       fmt.Errorf("npm outdated parse json: %w", err),
		}
	}

	infos := make([]UpdateInfo, 0, len(entries))
	skippedPre := 0
	for name, e := range entries {
		// Defensive: skip if current == latest (no actual change).
		if e.Current == e.Latest {
			continue
		}
		// H5 gate: stable current + pre-release latest → skip to avoid
		// unexpected upgrades to unstable channels.
		if IsNpmPreRelease(e.Latest) && !IsNpmPreRelease(e.Current) {
			slog.Debug("package.update.npm.skipped_prerelease",
				"name", name, "current", e.Current, "latest", e.Latest)
			skippedPre++
			continue
		}
		meta := map[string]any{"wanted": e.Wanted}
		if IsNpmPreRelease(e.Current) {
			meta["preRelease"] = true
		}
		infos = append(infos, UpdateInfo{
			Source:         "npm",
			Name:           name,
			CurrentVersion: e.Current,
			LatestVersion:  e.Latest,
			CheckedAt:      time.Now().UTC(),
			Meta:           meta,
		})
	}

	slog.Info("package.update.npm.check",
		"count", len(infos),
		"skipped_prerelease", skippedPre,
		"duration_ms", time.Since(start).Milliseconds())

	return UpdateCheckResult{Source: "npm", Available: true, Updates: infos}
}
