package cmd

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// initGitHubInstaller constructs the process-wide GitHub Releases installer
// from environment variables and registers it via skills.SetDefaultGitHubInstaller.
//
// Config comes ONLY from env vars (token is a secret — never from config.json):
//
//	GOCLAW_PACKAGES_GITHUB_TOKEN          optional PAT (boosts rate limit, enables private repos)
//	GOCLAW_PACKAGES_MAX_ASSET_SIZE_MB     default 200
//	GOCLAW_PACKAGES_GITHUB_ALLOWED_ORGS   comma-separated allowlist (empty = all allowed)
//	GOCLAW_PACKAGES_GITHUB_BIN_DIR        default /app/data/.runtime/bin
//	GOCLAW_PACKAGES_GITHUB_MANIFEST       default {BIN_DIR}/../github-packages.json
func initGitHubInstaller() {
	cfg := &skills.GitHubPackagesConfig{
		Token:        os.Getenv("GOCLAW_PACKAGES_GITHUB_TOKEN"),
		BinDir:       os.Getenv("GOCLAW_PACKAGES_GITHUB_BIN_DIR"),
		ManifestPath: os.Getenv("GOCLAW_PACKAGES_GITHUB_MANIFEST"),
	}
	if v := os.Getenv("GOCLAW_PACKAGES_MAX_ASSET_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxAssetSizeMB = n
		}
	}
	if v := os.Getenv("GOCLAW_PACKAGES_GITHUB_ALLOWED_ORGS"); v != "" {
		for o := range strings.SplitSeq(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.AllowedOrgs = append(cfg.AllowedOrgs, o)
			}
		}
	}

	// NewGitHubInstaller calls cfg.Defaults() — no need to invoke it here.
	client := skills.NewGitHubClient(cfg.Token)
	installer := skills.NewGitHubInstaller(client, cfg)
	skills.SetDefaultGitHubInstaller(installer)

	// Best-effort ensure bin dir + manifest dir exist (entrypoint may run as root
	// while Go process runs as goclaw — respect pre-existing permissions).
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		slog.Warn("github.installer: mkdir bin dir failed", "path", cfg.BinDir, "error", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.ManifestPath), 0o755); err != nil {
		slog.Warn("github.installer: mkdir manifest dir failed", "path", cfg.ManifestPath, "error", err)
	}

	slog.Info("packages: github installer enabled",
		"bin_dir", cfg.BinDir,
		"manifest", cfg.ManifestPath,
		"allowed_orgs", cfg.AllowedOrgs,
		"max_asset_mb", cfg.MaxAssetSizeMB,
		"token_set", cfg.Token != "",
	)
}
