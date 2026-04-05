package browser

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// StorageManager manages browser profile directories on the filesystem.
type StorageManager struct {
	workspace string
	logger    *slog.Logger
}

// ProfileInfo describes a stored browser profile.
type ProfileInfo struct {
	Name         string    `json:"name"`
	SizeBytes    int64     `json:"sizeBytes"`
	Size         string    `json:"size"` // human-readable
	LastModified time.Time `json:"lastModified"`
}

// NewStorageManager creates a StorageManager rooted at workspace.
func NewStorageManager(workspace string, logger *slog.Logger) *StorageManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &StorageManager{workspace: workspace, logger: logger}
}

var validProfileName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ListProfiles returns all profiles for a tenant.
func (sm *StorageManager) ListProfiles(tenantID string) ([]ProfileInfo, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	dir := filepath.Join(sm.workspace, "browser", "profiles", tenantID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ProfileInfo{}, nil
		}
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	var profiles []ProfileInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		size := dirSize(filepath.Join(dir, entry.Name()))
		profiles = append(profiles, ProfileInfo{
			Name:         entry.Name(),
			SizeBytes:    size,
			Size:         humanSize(size),
			LastModified: info.ModTime(),
		})
	}
	return profiles, nil
}

// DeleteProfile removes a profile directory for a tenant.
func (sm *StorageManager) DeleteProfile(tenantID, profileName string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	if !validProfileName.MatchString(profileName) {
		return fmt.Errorf("invalid profile name: %q", profileName)
	}

	dir, err := sm.ResolveProfileDir(tenantID, profileName)
	if err != nil {
		return err
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("profile not found: %s", profileName)
	}

	sm.logger.Info("deleting browser profile", "tenant", tenantID, "profile", profileName, "dir", dir)
	return os.RemoveAll(dir)
}

// ResolveProfileDir returns the absolute path for a tenant's profile, with path traversal prevention.
func (sm *StorageManager) ResolveProfileDir(tenantID, profileName string) (string, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	if !validProfileName.MatchString(profileName) {
		return "", fmt.Errorf("invalid profile name: %q", profileName)
	}
	if !validProfileName.MatchString(tenantID) && tenantID != "default" {
		// Allow UUID-format tenant IDs
		if len(tenantID) > 100 || strings.Contains(tenantID, "..") || strings.Contains(tenantID, "/") {
			return "", fmt.Errorf("invalid tenant ID: %q", tenantID)
		}
	}

	base := filepath.Join(sm.workspace, "browser", "profiles", tenantID)
	resolved := filepath.Join(base, profileName)

	// Path traversal check: resolved must be under base
	absBase, _ := filepath.Abs(base)
	absResolved, _ := filepath.Abs(resolved)
	if !strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) && absResolved != absBase {
		return "", fmt.Errorf("security: path traversal detected for profile %q", profileName)
	}

	return resolved, nil
}

// GetUsage returns total disk usage of all profiles for a tenant.
func (sm *StorageManager) GetUsage(tenantID string) (int64, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	dir := filepath.Join(sm.workspace, "browser", "profiles", tenantID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, nil
	}
	return dirSize(dir), nil
}

// PurgeSession removes all data for a specific profile.
func (sm *StorageManager) PurgeSession(tenantID, profileName string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	dir, err := sm.ResolveProfileDir(tenantID, profileName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil // nothing to purge
	}
	sm.logger.Info("purging browser session", "tenant", tenantID, "profile", profileName)
	return os.RemoveAll(dir)
}

// ClearProfileData removes cache/cookies subdirectories but keeps the profile dir.
func (sm *StorageManager) ClearProfileData(tenantID, profileName string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	dir, err := sm.ResolveProfileDir(tenantID, profileName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	// Remove transient Chrome data subdirectories
	for _, sub := range []string{"Cache", "Code Cache", "GPUCache", "Service Worker", "Cookies", "Cookies-journal"} {
		target := filepath.Join(dir, sub)
		if _, err := os.Stat(target); err == nil {
			os.RemoveAll(target)
		}
	}
	sm.logger.Info("cleared browser profile data", "tenant", tenantID, "profile", profileName)
	return nil
}

// Cleanup removes profiles older than maxAge for a tenant. Returns count of removed profiles.
func (sm *StorageManager) Cleanup(tenantID string, maxAge time.Duration) (int, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	dir := filepath.Join(sm.workspace, "browser", "profiles", tenantID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			target := filepath.Join(dir, entry.Name())
			if err := os.RemoveAll(target); err != nil {
				sm.logger.Warn("failed to remove old profile", "profile", entry.Name(), "error", err)
				continue
			}
			removed++
			sm.logger.Info("removed old browser profile", "tenant", tenantID, "profile", entry.Name())
		}
	}
	return removed, nil
}

// dirSize recursively calculates directory size.
func dirSize(path string) int64 {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// humanSize formats bytes into a human-readable string.
func humanSize(bytes int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
