package skills

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PackageInfo describes a single installed package.
type PackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// GitHubPackageListEntry is a viewer-safe projection of GitHubPackageEntry.
// Deliberately omits AssetURL / SHA256 / AssetName so viewer-level callers of
// GET /v1/packages don't receive CDN download URLs or checksum metadata —
// those are install-time details the UI never renders. Mirrors the same
// narrowing applied to the release-picker endpoint (`assetPreview`).
type GitHubPackageListEntry struct {
	Name        string    `json:"name"`
	Repo        string    `json:"repo"`
	Tag         string    `json:"tag"`
	Binaries    []string  `json:"binaries"`
	InstalledAt time.Time `json:"installed_at"`
}

// InstalledPackages groups installed packages by manager.
type InstalledPackages struct {
	System []PackageInfo            `json:"system"`
	Pip    []PackageInfo            `json:"pip"`
	Npm    []PackageInfo            `json:"npm"`
	GitHub []GitHubPackageListEntry `json:"github,omitempty"`
}

const listTimeout = 15 * time.Second

// ListInstalledPackages queries apk, pip3, and npm for installed packages.
// Only returns user-installed packages (filters out base Alpine packages for system).
func ListInstalledPackages(ctx context.Context) *InstalledPackages {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	result := &InstalledPackages{}
	result.System = listApkUserPackages(ctx)
	result.Pip = listPipPackages(ctx)
	result.Npm = listNpmPackages(ctx)
	if gh := DefaultGitHubInstaller(); gh != nil {
		if entries, err := gh.List(); err == nil {
			result.GitHub = make([]GitHubPackageListEntry, 0, len(entries))
			for _, e := range entries {
				result.GitHub = append(result.GitHub, GitHubPackageListEntry{
					Name:        e.Name,
					Repo:        e.Repo,
					Tag:         e.Tag,
					Binaries:    e.Binaries,
					InstalledAt: e.InstalledAt,
				})
			}
		}
	}
	return result
}

// listApkUserPackages returns packages from the apk-packages persist file
// (user-installed on-demand packages only, not base Alpine).
func listApkUserPackages(ctx context.Context) []PackageInfo {
	runtimeDir := os.Getenv("RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/app/data/.runtime"
	}
	listFile := filepath.Join(runtimeDir, "apk-packages")

	f, err := os.Open(listFile)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Read unique package names from persist file.
	seen := make(map[string]bool)
	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return nil
	}

	// Get versions for persisted packages via apk info.
	var pkgs []PackageInfo
	for _, name := range names {
		version := getApkVersion(ctx, name)
		pkgs = append(pkgs, PackageInfo{Name: name, Version: version})
	}
	return pkgs
}

// getApkVersion returns the installed version of an apk package, or empty string.
// Uses "apk list --installed" which works without root and gives versioned output.
func getApkVersion(ctx context.Context, name string) string {
	// Output format: "github-cli-2.72.0-r6 aarch64 {github-cli} (MIT) [installed]"
	out, err := exec.CommandContext(ctx, "apk", "list", "--installed", name).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract version: everything between "name-" and the first space.
		if strings.HasPrefix(line, name+"-") {
			rest := strings.TrimPrefix(line, name+"-")
			if idx := strings.IndexByte(rest, ' '); idx > 0 {
				return rest[:idx]
			}
			return rest
		}
	}
	return ""
}

// listPipPackages returns pip3-installed packages via JSON output.
func listPipPackages(ctx context.Context) []PackageInfo {
	out, err := exec.CommandContext(ctx, "pip3", "list", "--format", "json").CombinedOutput()
	if err != nil {
		return nil
	}

	var raw []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	pkgs := make([]PackageInfo, 0, len(raw))
	for _, r := range raw {
		pkgs = append(pkgs, PackageInfo{Name: r.Name, Version: r.Version})
	}
	return pkgs
}

// listNpmPackages returns globally installed npm packages.
func listNpmPackages(ctx context.Context) []PackageInfo {
	out, err := exec.CommandContext(ctx, "npm", "list", "-g", "--json", "--depth=0").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil
	}

	var raw struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	pkgs := make([]PackageInfo, 0, len(raw.Dependencies))
	for name, info := range raw.Dependencies {
		pkgs = append(pkgs, PackageInfo{Name: name, Version: info.Version})
	}
	return pkgs
}
