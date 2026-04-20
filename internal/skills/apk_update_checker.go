package skills

// apk_update_checker.go — ApkUpdateChecker polls apk for available package
// updates by invoking the pkg-helper Unix socket (actions: update-index,
// list-outdated). All apk invocations run via the privileged helper because the
// gateway runs unprivileged as `goclaw`. No direct exec.Command("apk", ...) here.
//
// Availability semantics:
//   - Helper socket unreachable (dial fail) → Available:false, nil Err.
//   - Helper reachable but action fails    → Available:true, Err set.
//   - Two round-trips per Check(): (1) update-index ~60s, (2) list-outdated ~30s.

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

const (
	// apkCheckerUpdateIndexTimeout is the per-call budget for refreshing the
	// remote index (network-bound: fetches index from Alpine mirrors).
	apkCheckerUpdateIndexTimeout = 60 * time.Second

	// apkCheckerListTimeout is the per-call budget for reading the outdated
	// package list (local-only: reads cached index, no network).
	apkCheckerListTimeout = 30 * time.Second
)

// apkNameVerBoundary matches a hyphen immediately followed by a digit.
// Used to locate the rightmost name/version boundary in Alpine package strings
// of the form "<name>-<ver>", where name itself may contain hyphens (e.g. py3-pip).
var apkNameVerBoundary = regexp.MustCompile(`-\d`)

// ApkUpdateChecker implements UpdateChecker for the "apk" source.
// It calls the pkg-helper Unix socket to refresh the Alpine index and enumerate
// outdated packages. Thread-safe: no mutable state; apkHelperCallFunc hook MUST
// only be mutated from single-goroutine test setup.
type ApkUpdateChecker struct{}

// NewApkUpdateChecker returns an ApkUpdateChecker ready for use.
func NewApkUpdateChecker() *ApkUpdateChecker { return &ApkUpdateChecker{} }

// Source returns "apk".
func (c *ApkUpdateChecker) Source() string { return "apk" }

// Check polls apk for outdated packages and returns UpdateCheckResult.
//
// Not on Alpine (IsAlpineRuntime=false) → Available:false, nil Err.
// Socket dial fail                       → Available:false, nil Err.
// update-index helper error              → Available:true, Err set.
// list-outdated helper error             → Available:true, Err set.
// Success                                → Available:true, Updates populated.
//
// knownETags is ignored: apk has no ETag / conditional-fetch mechanism.
func (c *ApkUpdateChecker) Check(ctx context.Context, _ map[string]string) UpdateCheckResult {
	start := time.Now()

	// Fast-fail: we are not on Alpine Linux.
	if !IsAlpineRuntime() {
		slog.Info("package.update.apk.unavailable", "reason", "not alpine")
		return UpdateCheckResult{Source: "apk", Available: false}
	}

	// Round-trip 1: refresh the remote index (network-bound, 60s).
	upCtx, upCancel := context.WithTimeout(ctx, apkCheckerUpdateIndexTimeout)
	ok, code, _, errMsg := apkHelperCallFunc(upCtx, "update-index", "")
	upCancel()

	if !ok {
		if code == "helper_unavailable" {
			slog.Info("package.update.apk.unavailable", "reason", errMsg)
			return UpdateCheckResult{Source: "apk", Available: false}
		}
		slog.Warn("package.update.apk.check",
			"stage", "update-index", "code", code, "error", errMsg)
		return UpdateCheckResult{
			Source:    "apk",
			Available: true,
			Err:       fmt.Errorf("apk update-index: %s (code=%s)", errMsg, code),
		}
	}

	// Round-trip 2: read outdated packages from the refreshed local index (30s).
	lsCtx, lsCancel := context.WithTimeout(ctx, apkCheckerListTimeout)
	ok, code, data, errMsg := apkHelperCallFunc(lsCtx, "list-outdated", "")
	lsCancel()

	if !ok {
		slog.Warn("package.update.apk.check",
			"stage", "list-outdated", "code", code, "error", errMsg)
		return UpdateCheckResult{
			Source:    "apk",
			Available: true,
			Err:       fmt.Errorf("apk list-outdated: %s (code=%s)", errMsg, code),
		}
	}

	entries := parseApkOutdated(data)
	infos := make([]UpdateInfo, 0, len(entries))
	now := time.Now().UTC()
	for _, e := range entries {
		infos = append(infos, UpdateInfo{
			Source:         "apk",
			Name:           e.Name,
			CurrentVersion: e.Version,
			LatestVersion:  e.Latest,
			CheckedAt:      now,
			Meta:           map[string]any{"source": "apk"},
		})
	}

	slog.Info("package.update.apk.check",
		"count", len(infos),
		"duration_ms", time.Since(start).Milliseconds())

	return UpdateCheckResult{Source: "apk", Available: true, Updates: infos}
}

// apkOutdatedEntry holds a single parsed result from `apk version -l '<'` output.
type apkOutdatedEntry struct {
	Name    string
	Version string
	Latest  string
}

// parseApkOutdated parses `apk version -l '<'` text output into a slice of
// apkOutdatedEntry. Each line has the form:
//
//	<name>-<installed_ver> < <available_ver>
//
// The name/version boundary is the rightmost "-<digit>" in the left-hand token,
// which correctly handles packages whose names contain hyphens (e.g. py3-pip).
// Malformed lines are skipped with slog.Warn; the caller receives whatever
// well-formed entries were parsed.
func parseApkOutdated(raw string) []apkOutdatedEntry {
	lines := strings.Split(raw, "\n")
	out := make([]apkOutdatedEntry, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Expect exactly one " < " separator (three bytes with surrounding spaces).
		parts := strings.SplitN(line, " < ", 2)
		if len(parts) != 2 {
			slog.Warn("apk checker: malformed line", "line", line)
			continue
		}

		lhs := strings.TrimSpace(parts[0])
		latest := strings.TrimSpace(parts[1])

		if lhs == "" || latest == "" {
			slog.Warn("apk checker: malformed line", "line", line)
			continue
		}

		// Find the rightmost "-<digit>" boundary in lhs to split name from version.
		// FindAllStringIndex returns all match positions; we want the last one.
		matches := apkNameVerBoundary.FindAllStringIndex(lhs, -1)
		if len(matches) == 0 {
			slog.Warn("apk checker: malformed line", "line", line)
			continue
		}

		// The rightmost match gives us the split point: index of the '-'.
		splitIdx := matches[len(matches)-1][0]
		name := lhs[:splitIdx]
		version := lhs[splitIdx+1:] // skip the '-' itself

		if name == "" || version == "" {
			slog.Warn("apk checker: malformed line", "line", line)
			continue
		}

		out = append(out, apkOutdatedEntry{
			Name:    name,
			Version: version,
			Latest:  latest,
		})
	}

	return out
}
