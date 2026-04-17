package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrUpdateCacheCorrupt signals that a cache file was present but unparseable.
// The loader still returns an empty cache so callers can proceed; this sentinel
// is exposed for tests and runbook tooling.
var ErrUpdateCacheCorrupt = errors.New("skills: update cache file corrupt")

// UpdateInfo describes a single available update detected by a checker.
//
// Meta holds source-specific fields without polluting the struct. For GitHub
// binaries it contains:
//
//	repo           string  — "owner/repo"
//	assetName      string
//	assetURL       string  — may be stale; re-verify host-allowlist before download
//	assetSHA256    string  — empty if publisher ships no checksum file
//	assetSizeBytes int64
type UpdateInfo struct {
	Source         string         `json:"source"`                // "github" (Phase 1)
	Name           string         `json:"name"`                  // matches GitHubPackageEntry.Name
	CurrentVersion string         `json:"currentVersion"`        // manifest.Tag at check time
	LatestVersion  string         `json:"latestVersion"`         // candidate.tag_name
	CheckedAt      time.Time      `json:"checkedAt"`
	Meta           map[string]any `json:"meta,omitempty"`
}

// UpdateCache is the on-disk aggregate of all known updates + ETag state.
// Access via LoadUpdateCache / SaveUpdateCache + the Setter/Getter methods
// which serialize through mu. Callers must NOT mutate Updates or GitHubETags
// directly under concurrent use.
type UpdateCache struct {
	Updates     []UpdateInfo      `json:"updates"`
	CheckedAt   time.Time         `json:"checkedAt"`
	GitHubETags map[string]string `json:"githubETags"`

	mu sync.Mutex `json:"-"`
}

// LoadUpdateCache reads the cache from disk. Missing file returns an empty
// cache and no error; parse failure returns an empty cache and ErrUpdateCacheCorrupt
// so the caller can decide whether to log and trigger a full refresh.
func LoadUpdateCache(path string) (*UpdateCache, error) {
	c := &UpdateCache{GitHubETags: make(map[string]string)}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return &UpdateCache{GitHubETags: make(map[string]string)}, fmt.Errorf("%w: %v", ErrUpdateCacheCorrupt, err)
	}
	if c.GitHubETags == nil {
		c.GitHubETags = make(map[string]string)
	}
	return c, nil
}

// SaveUpdateCache atomically writes the cache to disk via tmp+fsync+rename.
// Pattern matches GitHubInstaller.saveManifest (file fsync for inode durability,
// rename for commit, best-effort dir fsync for ordering on ext4/XFS with
// journal-async). Callers should hold the cache mu during serialization.
func SaveUpdateCache(path string, c *UpdateCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
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
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// SetETag stores the ETag for a cache key (typically "owner/repo" or
// "owner/repo:list"). Safe for concurrent use.
func (c *UpdateCache) SetETag(key, etag string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.GitHubETags == nil {
		c.GitHubETags = make(map[string]string)
	}
	c.GitHubETags[key] = etag
}

// GetETag returns the stored ETag for a cache key, or empty if absent.
// Safe for concurrent use.
func (c *UpdateCache) GetETag(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.GitHubETags[key]
}

// MergeETags applies a batch of (key, etag) pairs atomically. Used by the
// registry to merge a checker's local ETag map back into the shared cache
// after parallel checkers return (red-team fix C2 — avoids concurrent map
// writes across checker goroutines).
func (c *UpdateCache) MergeETags(batch map[string]string) {
	if len(batch) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.GitHubETags == nil {
		c.GitHubETags = make(map[string]string)
	}
	for k, v := range batch {
		c.GitHubETags[k] = v
	}
}

// ReplaceUpdates atomically swaps the Updates slice and sets CheckedAt.
// Used by the registry after all checkers return; the passed slice is
// adopted (no copy) so callers must not retain a reference.
func (c *UpdateCache) ReplaceUpdates(updates []UpdateInfo, checkedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Updates = updates
	c.CheckedAt = checkedAt
}

// Snapshot returns a shallow copy of Updates + CheckedAt. Suitable for
// read-only consumers (HTTP handler serialization).
func (c *UpdateCache) Snapshot() (updates []UpdateInfo, checkedAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]UpdateInfo, len(c.Updates))
	copy(out, c.Updates)
	return out, c.CheckedAt
}

// RemoveUpdate drops the (source, name) pair from Updates. No-op if absent.
// Called after a successful single-package update so the UI immediately
// reflects the applied state without waiting for the next refresh.
func (c *UpdateCache) RemoveUpdate(source, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.Updates[:0]
	for _, u := range c.Updates {
		if u.Source == source && u.Name == name {
			continue
		}
		out = append(out, u)
	}
	c.Updates = out
}
