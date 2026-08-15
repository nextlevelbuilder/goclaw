package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/version"
)

const (
	githubRepo         = "nextlevelbuilder/goclaw"
	liteTagPrefix      = "lite-v"
	updateCheckInterval = 1 * time.Hour
	maxResponseBody    = 2 << 20 // 2 MB
)

// UpdateInfo holds the latest release information from GitHub.
type UpdateInfo struct {
	LatestVersion   string `json:"latestVersion"`
	UpdateURL       string `json:"updateUrl"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseNotes    string `json:"releaseNotes,omitempty"`
}

// UpdateChecker periodically checks GitHub for new releases.
type UpdateChecker struct {
	currentVersion string
	mu             sync.RWMutex
	info           *UpdateInfo
	etag           string // ETag for conditional requests
}

// NewUpdateChecker creates an UpdateChecker for the given current version.
func NewUpdateChecker(currentVersion string) *UpdateChecker {
	return &UpdateChecker{currentVersion: currentVersion}
}

// Start begins periodic update checking. Call with a cancellable context.
func (uc *UpdateChecker) Start(ctx context.Context) {
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

// githubRelease is a minimal GitHub Release API response.
type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (uc *UpdateChecker) check() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use releases list API (not /latest) to filter out lite-v* tags.
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases", githubRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("update check: failed to create request", "error", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "goclaw/"+uc.currentVersion)

	// ETag conditional request to reduce API rate limit usage.
	if uc.etag != "" {
		req.Header.Set("If-None-Match", uc.etag)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("update check: request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	// 304 Not Modified — cached info is still valid.
	if resp.StatusCode == http.StatusNotModified {
		return
	}
	if resp.StatusCode != http.StatusOK {
		slog.Warn("update check: unexpected status", "status", resp.StatusCode)
		return
	}

	// Cache ETag.
	if etag := resp.Header.Get("ETag"); etag != "" {
		uc.etag = etag
	}

	var releases []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&releases); err != nil {
		slog.Warn("update check: failed to decode response", "error", err)
		return
	}

	// Find the latest non-draft, non-prerelease, non-lite server release.
	for _, rel := range releases {
		if rel.Draft || rel.Prerelease {
			continue
		}
		if strings.HasPrefix(rel.TagName, liteTagPrefix) {
			continue
		}
		if !strings.HasPrefix(rel.TagName, "v") {
			continue
		}

		info := &UpdateInfo{
			LatestVersion:   rel.TagName,
			UpdateURL:       rel.HTMLURL,
			UpdateAvailable: version.IsNewer(rel.TagName, uc.currentVersion),
			ReleaseNotes:    rel.Body,
		}

		uc.mu.Lock()
		uc.info = info
		uc.mu.Unlock()

		if info.UpdateAvailable {
			slog.Info("new version available", "current", uc.currentVersion, "latest", rel.TagName)
		}
		return
	}
}
