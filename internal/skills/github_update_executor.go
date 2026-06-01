package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Sentinel errors for the update executor.
var (
	ErrUpdateChecksumMismatch = errors.New("github.update: asset checksum mismatch")
	ErrUpdateSwapFailed       = errors.New("github.update: atomic swap failed (previous version restored)")
	ErrUpdateManifestDesync   = errors.New("github.update: binary swapped but manifest save failed (manual recovery required)")
)

// GitHubUpdateExecutor implements UpdateExecutor for "github" source.
// Shares the installer's config and client; executor itself is lock-free
// (caller uses PackageLocker). Red-team fixes applied:
//   - C1: two-phase swap — all olds → .bak BEFORE any new → dest.
//   - C3: re-verifies asset via meta SHA256 when present; refuses staged
//     URL whose host is not in allowedDownloadHosts.
//   - C4: saveManifest retries up to 3× before declaring desync.
//   - H6: explicit ScratchDir (no "../tmp" symlink hazard).
//   - L4: file written with 0755 during extraction, not chmod post-rename.
type GitHubUpdateExecutor struct {
	Installer  *GitHubInstaller
	ScratchDir string // explicit; defaults to filepath.Join(BinDir, "..", "tmp") if empty
}

// NewGitHubUpdateExecutor wires the executor. Call SetScratchDir to override
// the default tmp path.
func NewGitHubUpdateExecutor(installer *GitHubInstaller) *GitHubUpdateExecutor {
	return &GitHubUpdateExecutor{Installer: installer}
}

// Source returns "github".
func (e *GitHubUpdateExecutor) Source() string { return "github" }

// scratchDir returns the resolved scratch directory.
func (e *GitHubUpdateExecutor) scratchDir() string {
	if e.ScratchDir != "" {
		return e.ScratchDir
	}
	return filepath.Join(filepath.Dir(e.Installer.Config.BinDir), "tmp")
}

// Update applies the target version. The caller holds PackageLocker for
// (source, name). See package doc for red-team fixes applied in-situ.
func (e *GitHubUpdateExecutor) Update(ctx context.Context, name, toVersion string, meta map[string]any) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%w (got %s)", ErrUnsupportedOS, runtime.GOOS)
	}
	if e.Installer == nil || e.Installer.Client == nil {
		return errors.New("github update executor: installer not configured")
	}

	// Load manifest; locate entry by name.
	m, err := e.Installer.loadManifest()
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	idx := findEntryByName(m, name)
	if idx < 0 {
		return fmt.Errorf("%w: %s", ErrPackageNotInstalled, name)
	}
	entry := m.Packages[idx]

	owner, repo, ok := splitOwnerRepo(entry.Repo)
	if !ok {
		return fmt.Errorf("manifest entry has invalid repo: %q", entry.Repo)
	}

	// Resolve target tag: explicit toVersion OR fall back to meta LatestVersion.
	target := toVersion
	if target == "" {
		if v, ok := metaString(meta, "latestVersion"); ok {
			target = v
		}
	}
	if target == "" {
		return errors.New("github update executor: toVersion required (no meta)")
	}
	if target == entry.Tag {
		// No-op — caller should have filtered, but handle gracefully.
		return nil
	}

	// Resolve asset. Try meta first (fast path from check); verify host; refetch
	// if stale or missing. C3 fix — cached asset URL is a hint, not a trust anchor.
	assetURL, _ := metaString(meta, "assetURL")
	assetName, _ := metaString(meta, "assetName")
	assetSHA, _ := metaString(meta, "assetSHA256")

	needRefetch := assetURL == "" || assetName == "" || assetSHA == ""
	if !needRefetch {
		if verr := validateDownloadURL(assetURL); verr != nil {
			slog.Warn("github.update: cached assetURL rejected; refetching",
				"name", name, "error", verr)
			needRefetch = true
		}
	}
	if needRefetch {
		rel, _, _, ferr := e.Installer.Client.CondGetRelease(ctx, owner, repo, target, "")
		if ferr != nil {
			return fmt.Errorf("fetch release %s: %w", target, ferr)
		}
		if rel == nil {
			return fmt.Errorf("%w: %s", ErrGitHubNotFound, target)
		}
		asset, aerr := SelectAsset(rel.Assets, "linux", runtime.GOARCH)
		if aerr != nil {
			return aerr
		}
		assetURL = asset.DownloadURL
		assetName = asset.Name
		// Opportunistically reload checksum from the release.
		if assetSHA == "" {
			assetSHA = findAssetSHA256(ctx, e.Installer.Client, rel, asset.Name)
		}
		// Final host validation (redirect case).
		if verr := validateDownloadURL(assetURL); verr != nil {
			return verr
		}
	}

	// Prepare scratch dir — isolated per-update.
	scratch := filepath.Join(e.scratchDir(),
		fmt.Sprintf("%s-%s-%d", name, sanitizeTag(target), time.Now().UnixNano()))
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return fmt.Errorf("create scratch dir: %w", err)
	}
	defer os.RemoveAll(scratch)

	// Download.
	tmpArchive, sha, derr := e.Installer.Client.DownloadAsset(ctx, assetURL, e.Installer.Config.MaxAssetBytes())
	if derr != nil {
		return fmt.Errorf("download asset: %w", derr)
	}
	// Move archive into scratch so the defer cleans it up uniformly.
	scratchArchive := filepath.Join(scratch, filepath.Base(tmpArchive))
	if rerr := os.Rename(tmpArchive, scratchArchive); rerr != nil {
		// Cross-device rename may fail — fall back to just using tmpArchive
		// directly and remove it after.
		scratchArchive = tmpArchive
		defer os.Remove(tmpArchive)
	}

	// Verify SHA256 (constant-time) when publisher provides one.
	if assetSHA != "" {
		if verr := VerifyChecksum(assetSHA, sha); verr != nil {
			return fmt.Errorf("%w: %v", ErrUpdateChecksumMismatch, verr)
		}
	} else {
		slog.Info("github.update: no checksum available; proceeding without verification",
			"asset", assetName)
	}

	// Extract.
	files, eerr := ExtractArchiveAs(scratchArchive, repo, 2*e.Installer.Config.MaxAssetBytes())
	if eerr != nil {
		return fmt.Errorf("extract: %w", eerr)
	}
	binaries := pickBinaries(files, repo)
	if len(binaries) == 0 {
		return fmt.Errorf("%w: %s", ErrNoBinaryInArchive, assetName)
	}

	// ELF validate EVERY binary before swap.
	for i := range binaries {
		if verr := validateELF(binaries[i].Content); verr != nil {
			return verr
		}
	}

	// Stage all new binaries in scratch first with 0755 permissions (L4 —
	// chmod BEFORE move, not after, to eliminate the exec-bit race).
	staged := make(map[string]string, len(binaries)) // dest → stagedPath
	binDir := e.Installer.Config.BinDir
	for i := range binaries {
		b := binaries[i]
		base := filepath.Base(b.Name)
		stagedPath := filepath.Join(scratch, "staged-"+base)
		if werr := os.WriteFile(stagedPath, b.Content, 0o755); werr != nil {
			return fmt.Errorf("stage %s: %w", base, werr)
		}
		staged[filepath.Join(binDir, base)] = stagedPath
	}

	// Acquire the installer's disk mutex for the swap + manifest save, since
	// install/uninstall share the same bin dir.
	e.Installer.mu.Lock()
	defer e.Installer.mu.Unlock()

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	// ---- Two-phase atomic swap (C1) ----
	//
	// Phase A: rename ALL existing olds → .bak. If any fails, rollback all
	// prior .bak renames and abort.
	// Phase B: rename ALL news → dest. If any fails, restore all .bak files
	// AND move any already-placed new into .failed-<ns> for forensics.
	// On success: delete all .bak files.

	type swapTarget struct {
		dest      string
		backup    string
		newSrc    string
		hadBackup bool // review CRIT-3: distinguish real .bak from fresh-install sentinel
	}
	now := time.Now().UnixNano()
	targets := make([]swapTarget, 0, len(staged))
	for dest, src := range staged {
		targets = append(targets, swapTarget{
			dest:   dest,
			backup: fmt.Sprintf("%s.bak.%d", dest, now),
			newSrc: src,
		})
	}

	// Phase A — old → .bak
	renamedA := make([]swapTarget, 0, len(targets))
	rollbackA := func() {
		// Only restore entries where we actually created a backup (CRIT-3);
		// skipping the rest avoids spurious security.update.rollback_failed
		// ENOENT alarms on fresh-install targets.
		for _, t := range renamedA {
			if !t.hadBackup {
				continue
			}
			if rerr := os.Rename(t.backup, t.dest); rerr != nil {
				slog.Error("security.update.rollback_failed",
					"source", "github", "name", name,
					"dest", t.dest, "backup", t.backup, "error", rerr)
			}
		}
	}
	for _, t := range targets {
		if _, serr := os.Stat(t.dest); os.IsNotExist(serr) {
			// Fresh install — no prior file. Mark hadBackup=false so rollback skips.
			renamedA = append(renamedA, t)
			continue
		} else if serr != nil {
			rollbackA()
			return fmt.Errorf("%w: stat %s: %v", ErrUpdateSwapFailed, t.dest, serr)
		}
		if rerr := os.Rename(t.dest, t.backup); rerr != nil {
			rollbackA()
			return fmt.Errorf("%w: rename old→bak %s: %v", ErrUpdateSwapFailed, t.dest, rerr)
		}
		t.hadBackup = true
		renamedA = append(renamedA, t)
	}

	// Phase B — new → dest
	installedB := make([]swapTarget, 0, len(targets))
	rollbackB := func() {
		// Remove any successfully-placed new binaries (move to .failed-<ns>).
		for _, t := range installedB {
			failed := fmt.Sprintf("%s.failed-%d", t.dest, now)
			if rerr := os.Rename(t.dest, failed); rerr != nil {
				slog.Error("security.update.quarantine_failed",
					"dest", t.dest, "target", failed, "error", rerr)
			}
		}
		// Restore all .bak files.
		rollbackA()
	}
	for _, t := range renamedA {
		if rerr := os.Rename(t.newSrc, t.dest); rerr != nil {
			rollbackB()
			return fmt.Errorf("%w: rename new→dest %s: %v", ErrUpdateSwapFailed, t.dest, rerr)
		}
		installedB = append(installedB, t)
	}

	// Success — delete .bak files.
	for _, t := range renamedA {
		if _, serr := os.Stat(t.backup); serr == nil {
			_ = os.Remove(t.backup)
		}
	}

	// Update manifest entry in place.
	entry.Tag = target
	entry.SHA256 = sha
	entry.AssetURL = assetURL
	entry.AssetName = assetName
	entry.InstalledAt = time.Now().UTC()
	// Binaries list unchanged: we only re-install the same binary set the
	// installer originally resolved. (Phase 2 pip/npm may change this.)
	m.Packages[idx] = entry

	// C4 — manifest save retry.
	if err := e.saveManifestWithRetry(m); err != nil {
		slog.Error("security.manifest.desync",
			"source", "github", "name", name, "from", entry.Tag, "to", target, "error", err)
		return fmt.Errorf("%w: %v", ErrUpdateManifestDesync, err)
	}
	return nil
}

// saveManifestWithRetry attempts 3 atomic writes with backoff.
func (e *GitHubUpdateExecutor) saveManifestWithRetry(m *GitHubManifest) error {
	var lastErr error
	backoffs := []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second}
	for _, b := range backoffs {
		if err := e.Installer.saveManifest(m); err == nil {
			return nil
		} else {
			lastErr = err
			time.Sleep(b)
		}
	}
	return lastErr
}

// findEntryByName returns index of the entry with matching Name, or -1.
func findEntryByName(m *GitHubManifest, name string) int {
	for i := range m.Packages {
		if m.Packages[i].Name == name {
			return i
		}
	}
	return -1
}

// metaString extracts a string value from meta, returning (value, present).
// Missing or wrong type returns ("", false) — never panics (C6 nil-safe).
func metaString(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// sanitizeTag makes a tag string safe for use in filesystem paths.
// Replaces any non-alphanumeric/dot/underscore/hyphen with '-'.
func sanitizeTag(tag string) string {
	var b strings.Builder
	b.Grow(len(tag))
	for _, r := range tag {
		switch {
		case r >= '0' && r <= '9',
			r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
