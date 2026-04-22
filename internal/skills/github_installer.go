package skills

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sentinel errors for the GitHub installer.
var (
	ErrInvalidGitHubSpec   = errors.New("github: invalid spec (expected github:owner/repo[@tag])")
	ErrGitHubOrgNotAllowed = errors.New("github: org not in allowlist")
	ErrNoMatchingAsset     = errors.New("github: no matching asset for runtime")
	ErrNotELF              = errors.New("github: not an ELF binary")
	ErrUnsupportedELFClass = errors.New("github: only 64-bit ELF supported")
	ErrELFArchMismatch     = errors.New("github: ELF architecture mismatch")
	ErrNoBinaryInArchive   = errors.New("github: no executable found in archive")
	ErrPackageNotInstalled = errors.New("github: package not installed")
	ErrUnsupportedOS       = errors.New("github: install only supported on Linux")
)

// GitHubSpec is the parsed form of a "github:owner/repo[@tag]" identifier.
type GitHubSpec struct {
	Owner string
	Repo  string
	Tag   string // empty string means "latest"
}

// gitHubSpecRE validates the supported identifier format.
// Owner: GitHub usernames are capped at 39 chars, alnum + hyphen, no leading/trailing hyphen.
// Repo: alnum + `.`/`_`/`-`.
// Tag: capped at 255 chars (git ref-name upper bound), excludes NUL/whitespace.
var gitHubSpecRE = regexp.MustCompile(`^github:([A-Za-z0-9](?:[A-Za-z0-9-]{0,37})?[A-Za-z0-9]|[A-Za-z0-9])/([A-Za-z0-9][A-Za-z0-9._-]*)(?:@([^\s\x00]{1,255}))?$`)

// ParseGitHubSpec parses an identifier string.
func ParseGitHubSpec(s string) (*GitHubSpec, error) {
	m := gitHubSpecRE.FindStringSubmatch(s)
	if m == nil {
		return nil, ErrInvalidGitHubSpec
	}
	return &GitHubSpec{Owner: m[1], Repo: m[2], Tag: m[3]}, nil
}

// GitHubPackagesConfig holds tunables for the installer.
// Token is sourced from env var only (never config.json plaintext).
type GitHubPackagesConfig struct {
	Token          string   // optional GitHub personal access token
	BinDir         string   // where to install binaries (default /app/data/.runtime/bin)
	ManifestPath   string   // manifest file path (default {BinDir}/../github-packages.json)
	AllowedOrgs    []string // lowercase list; empty = all allowed
	MaxAssetSizeMB int      // default 200
}

// Defaults fills in zero-valued fields.
func (c *GitHubPackagesConfig) Defaults() {
	if c.BinDir == "" {
		c.BinDir = "/app/data/.runtime/bin"
	}
	if c.ManifestPath == "" {
		c.ManifestPath = filepath.Join(filepath.Dir(c.BinDir), "github-packages.json")
	}
	if c.MaxAssetSizeMB <= 0 {
		c.MaxAssetSizeMB = 200
	}
	// Normalize allowed orgs to lowercase, trim whitespace, drop empties.
	out := c.AllowedOrgs[:0]
	for _, o := range c.AllowedOrgs {
		o = strings.ToLower(strings.TrimSpace(o))
		if o != "" {
			out = append(out, o)
		}
	}
	c.AllowedOrgs = out
}

// MaxAssetBytes returns MaxAssetSizeMB as a byte count.
func (c *GitHubPackagesConfig) MaxAssetBytes() int64 {
	return int64(c.MaxAssetSizeMB) * 1024 * 1024
}

// GitHubInstaller orchestrates end-to-end install + uninstall + list.
type GitHubInstaller struct {
	Client *GitHubClient
	Config *GitHubPackagesConfig

	mu sync.Mutex // serializes the final disk-write phase: bin dir writes + manifest mutation
	//             (download, extraction, and ELF validation intentionally run outside the lock)
}

// NewGitHubInstaller constructs an installer.
func NewGitHubInstaller(client *GitHubClient, cfg *GitHubPackagesConfig) *GitHubInstaller {
	if cfg == nil {
		cfg = &GitHubPackagesConfig{}
	}
	cfg.Defaults()
	return &GitHubInstaller{Client: client, Config: cfg}
}

// AllowedOrg returns true if owner passes allowlist (empty slice = all allowed).
func (i *GitHubInstaller) AllowedOrg(owner string) bool {
	if len(i.Config.AllowedOrgs) == 0 {
		return true
	}
	owner = strings.ToLower(owner)
	for _, a := range i.Config.AllowedOrgs {
		if a == owner {
			return true
		}
	}
	return false
}

// -------- Asset selection --------

var (
	excludeSuffixRE = regexp.MustCompile(`(?i)\.(sha256|sig|asc|minisig|pem|pub|cert|crt)$`)
	excludeNameRE   = regexp.MustCompile(`(?i)(source[\s_-]?code|source\.tar\.gz|source\.zip)`)
	linuxRE         = regexp.MustCompile(`(?i)linux`)
	amd64RE         = regexp.MustCompile(`(?i)(amd64|x86[-_]?64|x64)`)
	arm64RE         = regexp.MustCompile(`(?i)(arm64|aarch64)`)
)

// SelectAsset picks the best asset for target OS + arch.
// Heuristic (in order):
//  1. exclude checksum/signature suffix files
//  2. exclude "source code" archives
//  3. filter by OS ("linux")
//  4. filter by arch
//  5. prefer .tar.gz/.tgz > .zip > raw
//  6. tiebreak by shortest name
func SelectAsset(assets []GitHubAsset, goos, goarch string) (*GitHubAsset, error) {
	candidates := make([]GitHubAsset, 0, len(assets))
	for _, a := range assets {
		if excludeSuffixRE.MatchString(a.Name) {
			continue
		}
		if excludeNameRE.MatchString(a.Name) {
			continue
		}
		candidates = append(candidates, a)
	}

	if goos == "linux" {
		candidates = filterAssets(candidates, linuxRE)
	}
	switch goarch {
	case "amd64":
		candidates = filterAssets(candidates, amd64RE)
	case "arm64":
		candidates = filterAssets(candidates, arm64RE)
	}

	if len(candidates) == 0 {
		return nil, enrichNoMatch(assets, goos, goarch)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		pi, pj := extPriority(candidates[i].Name), extPriority(candidates[j].Name)
		if pi != pj {
			return pi < pj
		}
		return len(candidates[i].Name) < len(candidates[j].Name)
	})
	pick := candidates[0]
	return &pick, nil
}

func filterAssets(in []GitHubAsset, re *regexp.Regexp) []GitHubAsset {
	out := in[:0:0]
	for _, a := range in {
		if re.MatchString(a.Name) {
			out = append(out, a)
		}
	}
	return out
}

// extPriority gives lower numbers to preferred archive formats.
func extPriority(name string) int {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".tar.gz"), strings.HasSuffix(n, ".tgz"):
		return 0
	case strings.HasSuffix(n, ".zip"):
		return 1
	default:
		return 2
	}
}

func enrichNoMatch(all []GitHubAsset, goos, goarch string) error {
	names := make([]string, 0, len(all))
	for _, a := range all {
		names = append(names, a.Name)
	}
	return fmt.Errorf("%w (os=%s, arch=%s); available: %s",
		ErrNoMatchingAsset, goos, goarch, strings.Join(names, ", "))
}

// -------- Manifest --------

// GitHubPackageEntry records metadata about an installed package.
//
// Note: there is no InstalledBy field — the install call doesn't currently
// thread a user ID through, and an always-empty audit field is more misleading
// than useful. Add it back if/when the request context is plumbed in.
type GitHubPackageEntry struct {
	Name           string    `json:"name"`
	Repo           string    `json:"repo"`
	Tag            string    `json:"tag"`
	Binaries       []string  `json:"binaries"`
	SHA256         string    `json:"sha256"`
	AssetURL       string    `json:"asset_url"`
	AssetName      string    `json:"asset_name"`
	AssetSizeBytes int64     `json:"asset_size_bytes"`
	InstalledAt    time.Time `json:"installed_at"`
}

// GitHubManifest is the persisted state for installed GitHub packages.
type GitHubManifest struct {
	Version  int                  `json:"version"`
	Packages []GitHubPackageEntry `json:"packages"`
}

// loadManifest returns an empty manifest if file missing.
func (i *GitHubInstaller) loadManifest() (*GitHubManifest, error) {
	b, err := os.ReadFile(i.Config.ManifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &GitHubManifest{Version: 1}, nil
		}
		return nil, err
	}
	var m GitHubManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return &m, nil
}

// saveManifest writes atomically via temp + fsync + rename + dir fsync.
// The two fsyncs ensure durability across crashes/power-loss: without
// file fsync the rename commits a possibly-empty inode; without dir fsync
// the rename itself may be reordered on XFS/ext4 with journal-async.
func (i *GitHubInstaller) saveManifest(m *GitHubManifest) error {
	dir := filepath.Dir(i.Config.ManifestPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := i.Config.ManifestPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, i.Config.ManifestPath); err != nil {
		os.Remove(tmp)
		return err
	}
	// Best-effort dir fsync — opening a dir as O_RDONLY works on Linux/macOS
	// but some filesystems (e.g. Windows via WSL paths) may reject it. We
	// log and proceed — the rename has already happened.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// List returns all installed packages from manifest.
func (i *GitHubInstaller) List() ([]GitHubPackageEntry, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	m, err := i.loadManifest()
	if err != nil {
		return nil, err
	}
	return m.Packages, nil
}

// -------- ELF validation --------

// validateELF checks magic bytes, 64-bit class, and machine matches runtime.
func validateELF(content []byte) error {
	if len(content) < 4 {
		return ErrNotELF
	}
	if !bytes.Equal(content[:4], []byte{0x7f, 0x45, 0x4c, 0x46}) {
		return ErrNotELF
	}
	f, err := elf.NewFile(bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("invalid ELF: %w", err)
	}
	defer f.Close()

	if f.Class != elf.ELFCLASS64 {
		return ErrUnsupportedELFClass
	}
	wantMachine := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		wantMachine = elf.EM_AARCH64
	}
	if f.Machine != wantMachine {
		return fmt.Errorf("%w (binary=%v, runtime=%v)", ErrELFArchMismatch, f.Machine, wantMachine)
	}
	return nil
}

// -------- Binary picker --------

var nonBinaryPathRE = regexp.MustCompile(`(?i)(^|/)(man|docs?|contrib|completions|examples?|tests?|licenses?)/`)

// pickBinaries selects executable entries from an extracted archive.
// Preference order:
//  1. entries with basename == repo name (common case: `lazygit` binary in lazygit archive)
//  2. all executable entries whose path doesn't match nonBinaryPathRE
//     (man/docs/contrib/completions/examples/tests/licenses are excluded)
//  3. any ELF-magic entry under a non-excluded path
func pickBinaries(files []ArchiveFile, repoName string) []ArchiveFile {
	// Filter out clearly-not-binary paths first.
	var candidates []ArchiveFile
	for _, f := range files {
		if nonBinaryPathRE.MatchString(f.Name) {
			continue
		}
		candidates = append(candidates, f)
	}

	// Try exact basename match to repo name.
	var named []ArchiveFile
	for _, f := range candidates {
		base := filepath.Base(f.Name)
		if base == repoName {
			named = append(named, f)
		}
	}
	if len(named) > 0 {
		return named
	}

	// Otherwise: any executable-looking entry that survived the nonBinaryPathRE
	// filter above. Depth is not enforced here — ELF validation in the caller
	// is the final gate before chmod +x.
	var execs []ArchiveFile
	for _, f := range candidates {
		if isLikelyExecutable(f) {
			execs = append(execs, f)
		}
	}
	return execs
}

func isLikelyExecutable(f ArchiveFile) bool {
	// Executable bit set OR ELF magic present.
	if f.Mode&0o111 != 0 {
		return true
	}
	if len(f.Content) >= 4 && bytes.Equal(f.Content[:4], []byte{0x7f, 0x45, 0x4c, 0x46}) {
		return true
	}
	return false
}

// -------- Install + Uninstall --------

// Install runs the full pipeline: parse → check org → fetch release → select asset →
// download → verify → extract → validate ELF → write to bin dir → update manifest.
func (i *GitHubInstaller) Install(ctx context.Context, spec string) (*GitHubPackageEntry, error) {
	// The installer ships only Linux ELF asset selection + validation. Guard
	// non-Linux callers (Windows/macOS desktop host) up front so we don't
	// waste bandwidth fetching a Linux asset that's going to be rejected at
	// the ELF-machine check later.
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("%w (got %s)", ErrUnsupportedOS, runtime.GOOS)
	}
	parsed, err := ParseGitHubSpec(spec)
	if err != nil {
		return nil, err
	}
	if !i.AllowedOrg(parsed.Owner) {
		return nil, fmt.Errorf("%w: %s", ErrGitHubOrgNotAllowed, parsed.Owner)
	}

	release, err := i.Client.GetRelease(ctx, parsed.Owner, parsed.Repo, parsed.Tag)
	if err != nil {
		return nil, err
	}
	asset, err := SelectAsset(release.Assets, "linux", runtime.GOARCH)
	if err != nil {
		return nil, err
	}

	maxBytes := i.Config.MaxAssetBytes()
	tmpPath, sha, err := i.Client.DownloadAsset(ctx, asset.DownloadURL, maxBytes)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpPath)

	// Checksum verification (if publisher provides it).
	// Failure modes that silently proceed are noisy-logged so an operator can
	// detect a modified checksum file being served alongside a tampered asset.
	if ca := FindChecksumAsset(release, asset.Name); ca != nil {
		checksumPath, _, cerr := i.Client.DownloadAsset(ctx, ca.DownloadURL, 1<<20) // 1 MiB cap
		if cerr == nil {
			defer os.Remove(checksumPath)
			data, rerr := os.ReadFile(checksumPath)
			if rerr != nil {
				slog.Warn("github.installer: read checksum file failed",
					"checksum_asset", ca.Name, "error", rerr)
			} else {
				sums, perr := ParseChecksums(data)
				if perr != nil {
					slog.Warn("github.installer: parse checksum file failed",
						"checksum_asset", ca.Name, "error", perr)
				} else if expected, ok := sums[asset.Name]; ok {
					if verr := VerifyChecksum(expected, sha); verr != nil {
						return nil, verr
					}
				} else {
					// Publisher ships a checksum file that omits this asset.
					// Could be benign (asset added later, different file set)
					// or a MITM replacing the checksum file with an entry-free
					// one. We warn loudly; ELF validation remains the final gate.
					slog.Warn("github.installer: asset not listed in checksum file",
						"asset", asset.Name, "checksum_asset", ca.Name)
				}
			}
		} else {
			slog.Warn("github.installer: failed to fetch checksum file", "error", cerr)
		}
	} else {
		// Not a problem with the install — many upstream publishers simply
		// don't ship checksum files (jq, fzf, older ripgrep, etc.). Downgraded
		// from Warn so the suspicious cases (read/parse/asset-not-listed
		// errors, which stay at Warn above) stand out cleanly.
		slog.Info("github.installer: no checksum asset available", "asset", asset.Name)
	}

	// Pass the repo name as the fallback logical name so raw (non-archive)
	// ELF assets don't end up recorded under the temp filename
	// "goclaw-gh-asset-XXXX.bin".
	files, err := ExtractArchiveAs(tmpPath, parsed.Repo, 2*maxBytes)
	if err != nil {
		return nil, err
	}

	binaries := pickBinaries(files, parsed.Repo)
	if len(binaries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoBinaryInArchive, asset.Name)
	}
	for idx := range binaries {
		if err := validateELF(binaries[idx].Content); err != nil {
			return nil, err
		}
	}

	// Commit to disk under lock.
	i.mu.Lock()
	defer i.mu.Unlock()

	if err := os.MkdirAll(i.Config.BinDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bin dir: %w", err)
	}

	// Load manifest first so we can detect basename collisions with other
	// installed packages. Current policy is still last-writer-wins (changing
	// that would break re-install), but an operator needs to know when two
	// packages are fighting over the same binary name.
	m, err := i.loadManifest()
	if err != nil {
		return nil, err
	}
	entryRepo := parsed.Owner + "/" + parsed.Repo

	binNames := make([]string, 0, len(binaries))
	for _, b := range binaries {
		name := filepath.Base(b.Name)
		// Warn if another package in the manifest already owns this binary name.
		for _, p := range m.Packages {
			if !strings.EqualFold(p.Repo, entryRepo) {
				for _, pb := range p.Binaries {
					if pb == name {
						slog.Warn("github.installer: binary name collision — overwriting",
							"binary", name, "existing_package", p.Name,
							"existing_repo", p.Repo, "new_repo", entryRepo)
					}
				}
			}
		}
		dst := filepath.Join(i.Config.BinDir, name)
		if err := os.WriteFile(dst, b.Content, 0o755); err != nil {
			return nil, fmt.Errorf("write binary %s: %w", name, err)
		}
		binNames = append(binNames, name)
	}

	entry := GitHubPackageEntry{
		Name:           canonicalPackageName(parsed, binNames),
		Repo:           entryRepo,
		Tag:            release.TagName,
		Binaries:       binNames,
		SHA256:         sha,
		AssetURL:       asset.DownloadURL,
		AssetName:      asset.Name,
		AssetSizeBytes: asset.SizeBytes,
		InstalledAt:    time.Now().UTC(),
	}

	// Replace existing entry with same name.
	found := false
	for idx := range m.Packages {
		if m.Packages[idx].Name == entry.Name {
			m.Packages[idx] = entry
			found = true
			break
		}
	}
	if !found {
		m.Packages = append(m.Packages, entry)
	}
	if err := i.saveManifest(m); err != nil {
		return nil, err
	}
	return &entry, nil
}

// canonicalPackageName uses repo name unless a single binary has a different name.
func canonicalPackageName(spec *GitHubSpec, binNames []string) string {
	if len(binNames) == 1 && binNames[0] != spec.Repo {
		return binNames[0]
	}
	return spec.Repo
}

// Uninstall removes installed binaries + manifest entry.
// name matches GitHubPackageEntry.Name.
func (i *GitHubInstaller) Uninstall(ctx context.Context, name string) error {
	_ = ctx
	i.mu.Lock()
	defer i.mu.Unlock()

	m, err := i.loadManifest()
	if err != nil {
		return err
	}
	idx := -1
	for k, p := range m.Packages {
		if p.Name == name {
			idx = k
			break
		}
	}
	if idx < 0 {
		return ErrPackageNotInstalled
	}
	binaries := m.Packages[idx].Binaries
	// Persist the manifest BEFORE touching the files on disk. If saveManifest
	// fails we bail out without orphaning binaries on a manifest that still
	// claims them as installed (a retried Uninstall would otherwise hit
	// ErrPackageNotInstalled after the first attempt wiped the files).
	m.Packages = append(m.Packages[:idx], m.Packages[idx+1:]...)
	if err := i.saveManifest(m); err != nil {
		return err
	}
	// Remove-after-save is best-effort: a missing file is fine (idempotent),
	// any other error is warned and the manifest still reflects the truth
	// that the entry is no longer tracked.
	for _, b := range binaries {
		// Only remove files within our configured bin dir (defense in depth).
		path := filepath.Join(i.Config.BinDir, filepath.Base(b))
		if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
			slog.Warn("github.installer: remove binary failed", "path", path, "error", rerr)
		}
	}
	return nil
}
