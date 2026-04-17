package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// preReleaseRE matches common pre-release suffixes in tag names.
// Case-insensitive. Precedes golang.org/x/mod/semver.Prerelease which only
// recognises strict semver (v prefix + dash-separated ids).
var preReleaseRE = regexp.MustCompile(`(?i)-(alpha|beta|rc|pre|preview|dev|nightly|snapshot)`)

// isPreReleaseTag returns true when the tag likely denotes a pre-release.
// Double-gate: the caller combines this with GitHubRelease.Prerelease so a
// release later re-flagged at the API level is still treated correctly.
func isPreReleaseTag(tag string) bool {
	return preReleaseRE.MatchString(tag)
}

// GitHubUpdateChecker implements UpdateChecker for "github" source.
// Holds a weak reference to the installer for manifest access and to the
// shared GitHubClient for HTTP + ETag-aware fetches.
type GitHubUpdateChecker struct {
	Installer *GitHubInstaller
}

// NewGitHubUpdateChecker wires the checker to an existing installer.
func NewGitHubUpdateChecker(installer *GitHubInstaller) *GitHubUpdateChecker {
	return &GitHubUpdateChecker{Installer: installer}
}

// Source returns "github".
func (c *GitHubUpdateChecker) Source() string { return "github" }

// Check iterates the GitHub manifest, polls each repo (ETag-aware) and returns
// a list of UpdateInfo for entries with a newer release available.
//
// Per red-team fixes:
//   - C2: returns its own ETag map; registry merges under lock.
//   - H3: non-semver fallback uses strings.Compare > 0 to prevent silent
//     downgrade.
//   - H4: distinct ETag keys for /releases/latest vs /releases?per_page (list).
//   - M1: secondary rate-limit (403 Retry-After) aborts the remaining repos
//     with a warning log; per-repo ctx-cancel aborts gracefully.
func (c *GitHubUpdateChecker) Check(ctx context.Context, knownETags map[string]string) UpdateCheckResult {
	out := UpdateCheckResult{
		Source: c.Source(),
		ETags:  make(map[string]string),
	}
	if c.Installer == nil || c.Installer.Client == nil {
		out.Err = errors.New("github update checker: installer not configured")
		return out
	}
	m, err := c.Installer.loadManifest()
	if err != nil {
		out.Err = fmt.Errorf("load manifest: %w", err)
		return out
	}

	for idx := range m.Packages {
		if ctx.Err() != nil {
			out.Err = ctx.Err()
			return out
		}
		entry := m.Packages[idx]
		info, etags, err := c.checkEntry(ctx, entry, knownETags)
		// Propagate etags even on per-entry errors (304 may still populate).
		for k, v := range etags {
			out.ETags[k] = v
		}
		if err != nil {
			// Secondary rate limit aborts the whole sweep; other errors are
			// per-repo and isolated.
			if errors.Is(err, ErrGitHubSecondaryRateLimit) {
				slog.Warn("security.github.secondary_ratelimit",
					"repo", entry.Repo, "error", err)
				out.Err = err
				return out
			}
			slog.Warn("skills.update.github: check entry failed",
				"name", entry.Name, "repo", entry.Repo, "error", err)
			continue
		}
		if info != nil {
			out.Updates = append(out.Updates, *info)
		}
	}
	return out
}

// checkEntry performs the conditional fetch + candidate selection for a
// single manifest entry. Returns (update, newETags, err).
// update==nil means "no update available" (may still populate etags from 304).
func (c *GitHubUpdateChecker) checkEntry(ctx context.Context, entry GitHubPackageEntry, known map[string]string) (*UpdateInfo, map[string]string, error) {
	etags := make(map[string]string)
	owner, repo, ok := splitOwnerRepo(entry.Repo)
	if !ok {
		return nil, etags, fmt.Errorf("invalid manifest entry repo: %q", entry.Repo)
	}

	latestKey := entry.Repo                  // "owner/repo"
	listKey := entry.Repo + ":list"          // distinct keyspace (H4)

	// Always query /releases/latest (stable).
	latest, newETag, notMod, err := c.Installer.Client.CondGetRelease(ctx, owner, repo, "", known[latestKey])
	if err != nil && !errors.Is(err, ErrGitHubNotFound) {
		return nil, etags, err
	}
	if newETag != "" {
		etags[latestKey] = newETag
	}
	// 304 means cache still valid; still may have an older UpdateInfo carried
	// forward — Phase 1 does not persist per-entry UpdateInfo across checks, so
	// we skip silently (not a "new" update).
	if notMod {
		latest = nil
	}

	// If current is pre-release, also query the recent-releases list to find
	// the newest candidate that may itself be pre-release.
	var candidates []GitHubRelease
	if latest != nil && !latest.Draft {
		candidates = append(candidates, *latest)
	}
	currentIsPre := isPreReleaseTag(entry.Tag)
	if currentIsPre {
		list, listETag, listNotMod, lerr := c.Installer.Client.CondListReleases(ctx, owner, repo, 5, known[listKey])
		if lerr != nil && !errors.Is(lerr, ErrGitHubNotFound) {
			// Treat list failure as non-fatal — /latest result may suffice.
			slog.Warn("skills.update.github: list releases failed",
				"repo", entry.Repo, "error", lerr)
		} else {
			if listETag != "" {
				etags[listKey] = listETag
			}
			if !listNotMod {
				for _, rel := range list {
					if rel.Draft {
						continue
					}
					candidates = append(candidates, rel)
				}
			}
		}
	}

	if len(candidates) == 0 {
		return nil, etags, nil
	}

	// Pick the newest candidate with a DIFFERENT tag than current.
	best := pickNewestRelease(entry.Tag, candidates)
	if best == nil || best.TagName == entry.Tag {
		return nil, etags, nil
	}

	// Resolve the matching asset for current runtime OS+arch so the executor
	// can apply without a second fetch. If asset pick fails, skip but log —
	// don't surface as "update available" when we can't apply it.
	asset, aerr := SelectAsset(best.Assets, "linux", runtime.GOARCH)
	if aerr != nil {
		slog.Info("skills.update.github: update found but no compatible asset",
			"repo", entry.Repo, "latest", best.TagName, "error", aerr)
		return nil, etags, nil
	}

	// Opportunistically fetch the checksum map so the executor can verify
	// without refetching. If absent, leave sha256 empty — executor falls back
	// to its own publisher-checksum lookup (or warns).
	assetSHA := findAssetSHA256(ctx, c.Installer.Client, best, asset.Name)

	info := UpdateInfo{
		Source:         "github",
		Name:           entry.Name,
		CurrentVersion: entry.Tag,
		LatestVersion:  best.TagName,
		CheckedAt:      time.Now().UTC(),
		Meta: map[string]any{
			"repo":           entry.Repo,
			"assetName":      asset.Name,
			"assetURL":       asset.DownloadURL,
			"assetSizeBytes": asset.SizeBytes,
			"assetSHA256":    assetSHA, // may be empty
			"prerelease":     best.Prerelease,
		},
	}
	return &info, etags, nil
}

// findAssetSHA256 returns the publisher-provided SHA256 for the asset, or
// empty if no checksum file is present. Errors are logged and swallowed —
// the executor still verifies via its own download hash.
func findAssetSHA256(ctx context.Context, client *GitHubClient, rel *GitHubRelease, assetName string) string {
	ca := FindChecksumAsset(rel, assetName)
	if ca == nil {
		return ""
	}
	path, _, err := client.DownloadAsset(ctx, ca.DownloadURL, 1<<20)
	if err != nil {
		return ""
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sums, err := ParseChecksums(data)
	if err != nil {
		return ""
	}
	return sums[assetName]
}

// pickNewestRelease returns the release with the highest version compared to
// `current`. Uses semver when possible (v-prefixed). Non-semver tags fall back
// to `strings.Compare(tag, current) > 0` to avoid silent downgrades (H3).
//
// Returns nil if no candidate is strictly greater than current.
func pickNewestRelease(current string, candidates []GitHubRelease) *GitHubRelease {
	var best *GitHubRelease
	currentSemver := ensureV(current)
	currentIsValid := semver.IsValid(currentSemver)

	for i := range candidates {
		cand := &candidates[i]
		if cand.TagName == current {
			continue
		}
		if best == nil {
			if isCandidateNewer(current, currentSemver, currentIsValid, cand.TagName) {
				best = cand
			}
			continue
		}
		// Compare current best vs new candidate.
		if isCandidateNewer(best.TagName, ensureV(best.TagName), semver.IsValid(ensureV(best.TagName)), cand.TagName) {
			best = cand
		}
	}
	return best
}

// isCandidateNewer returns true when candidate is strictly newer than current.
// Both-semver: semver.Compare.
// Both-non-semver: strings.Compare > 0 (lex).
// Mixed: valid-semver wins only if it orders > current interpreted as non-semver.
// On ambiguity, return false to prevent downgrades.
func isCandidateNewer(currentRaw, currentSemver string, currentIsValid bool, candidateRaw string) bool {
	candSemver := ensureV(candidateRaw)
	candValid := semver.IsValid(candSemver)
	switch {
	case currentIsValid && candValid:
		return semver.Compare(candSemver, currentSemver) > 0
	case !currentIsValid && !candValid:
		return strings.Compare(candidateRaw, currentRaw) > 0
	default:
		// Mixed forms: flag but don't downgrade.
		slog.Debug("skills.update.github: mixed-form tag comparison skipped",
			"current", currentRaw, "candidate", candidateRaw)
		return false
	}
}

// ensureV returns tag with a "v" prefix if missing so semver.IsValid accepts
// forms like "1.2.3". Leaves non-numeric tags alone.
func ensureV(tag string) string {
	if tag == "" {
		return tag
	}
	if tag[0] == 'v' || tag[0] == 'V' {
		return tag
	}
	// Quick numeric check: if first rune is a digit, add v.
	if tag[0] >= '0' && tag[0] <= '9' {
		return "v" + tag
	}
	return tag
}

// splitOwnerRepo splits "owner/repo" safely.
func splitOwnerRepo(s string) (string, string, bool) {
	i := strings.IndexByte(s, '/')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
