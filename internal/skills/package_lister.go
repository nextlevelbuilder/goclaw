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

var packageListCommandCombinedOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

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

// ListInstalledPackages queries system, pip3, and npm for installed packages.
// System packages are limited to packages installed through GoClaw.
func ListInstalledPackages(ctx context.Context) *InstalledPackages {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	result := &InstalledPackages{}
	result.System = listSystemUserPackages(ctx)
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

func listSystemUserPackages(ctx context.Context) []PackageInfo {
	if IsAlpineRuntime() {
		return listApkUserPackages(ctx)
	}
	return listDebianUserPackages(ctx)
}

// listApkUserPackages returns packages from the apk-packages persist file
// (user-installed on-demand packages only, not base Alpine).
func listApkUserPackages(ctx context.Context) []PackageInfo {
	listFile := filepath.Join(packageRuntimeDir(), "apk-packages")

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
	out, err := packageListCommandCombinedOutput(ctx, "apk", "list", "--installed", name)
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
	out, err := packageListCommandCombinedOutput(ctx, "pip3", "list", "--format", "json")
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
	ensureNpmGlobalEnv()
	cmd := exec.CommandContext(ctx, npmBinary, "list", "-g", "--json", "--depth=0")
	cmd.Env = npmCommandEnv()
	out, err := cmd.CombinedOutput()
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

func listDebianUserPackages(ctx context.Context) []PackageInfo {
	records, err := readSystemPackageRecords()
	if err != nil || len(records) == 0 {
		return nil
	}

	pkgs := make([]PackageInfo, 0, len(records))
	for _, record := range records {
		if record.Manager != "apt" || record.Package == "" {
			continue
		}
		version := getDebianPackageVersion(ctx, record.Package)
		if version == "" {
			continue
		}
		name := record.Name
		if name == "" {
			name = record.Package
		}
		pkgs = append(pkgs, PackageInfo{Name: name, Version: version})
	}
	return pkgs
}

func getDebianPackageVersion(ctx context.Context, name string) string {
	out, err := packageListCommandCombinedOutput(ctx, "dpkg-query", "-W", "-f=${Version}", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
