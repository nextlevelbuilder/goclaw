package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubRepo         = "nextlevelbuilder/goclaw"
	updateCheckInterval = 1 * time.Hour
)

// UpdateInfo holds the latest release information from GitHub.
type UpdateInfo struct {
	LatestVersion  string `json:"latestVersion"`
	UpdateURL      string `json:"updateUrl"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// UpdateChecker periodically checks GitHub for new releases.
type UpdateChecker struct {
	currentVersion string
	mu             sync.RWMutex
	info           *UpdateInfo
}

// NewUpdateChecker creates an UpdateChecker for the given current version.
func NewUpdateChecker(currentVersion string) *UpdateChecker {
	return &UpdateChecker{currentVersion: currentVersion}
}

// Start begins periodic update checking. Call with a cancellable context.
func (uc *UpdateChecker) Start(ctx context.Context) {
	// Initial check after short delay (don't block startup)
	go func() {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}
		uc.check()

		ticker := time.NewTicker(updateCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				uc.check()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Info returns the cached update info, or nil if not yet checked.
func (uc *UpdateChecker) Info() *UpdateInfo {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return uc.info
}

func (uc *UpdateChecker) check() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("update check: failed to create request", "error", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "goclaw/"+uc.currentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("update check: request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("update check: unexpected status", "status", resp.StatusCode)
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		slog.Warn("update check: failed to decode response", "error", err)
		return
	}

	if release.TagName == "" {
		return
	}

	info := &UpdateInfo{
		LatestVersion:   release.TagName,
		UpdateURL:       release.HTMLURL,
		UpdateAvailable: isNewer(uc.currentVersion, release.TagName),
	}

	uc.mu.Lock()
	uc.info = info
	uc.mu.Unlock()

	if info.UpdateAvailable {
		slog.Info("new version available", "current", uc.currentVersion, "latest", release.TagName)
	}
}

// isNewer returns true if latest is a newer version than current.
// Compares by stripping "v" prefix and doing simple string comparison.
// Returns false if current is "dev" (development build).
func isNewer(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	if current == "dev" || current == "" || latest == "" {
		return false
	}
	if current == latest {
		return false
	}

	// Semantic version comparison: split by "." and compare numerically
	return compareSemver(latest, current) > 0
}

// compareSemver compares two semver strings (without "v" prefix).
// Returns >0 if a > b, <0 if a < b, 0 if equal.
// Handles pre-release suffixes by stripping them (e.g., "1.2.3-5-gabcdef").
func compareSemver(a, b string) int {
	partsA := parseSemver(a)
	partsB := parseSemver(b)

	for i := range 3 {
		va, vb := 0, 0
		if i < len(partsA) {
			va = partsA[i]
		}
		if i < len(partsB) {
			vb = partsB[i]
		}
		if va != vb {
			return va - vb
		}
	}
	return 0
}

func parseSemver(s string) []int {
	// Strip pre-release suffix: "1.2.3-5-gabcdef" → "1.2.3"
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		nums = append(nums, n)
	}
	return nums
}
