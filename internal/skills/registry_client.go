package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultRegistryURL = "https://raw.githubusercontent.com/goclaw-hub/registry/main/index.json"
	registryCacheTTL   = 1 * time.Hour
	registryFetchTimeout = 10 * time.Second
	maxIndexSize       = 1 << 20 // 1 MB
	cacheFileName      = "registry-index.json"
)

// RegistryEntry represents one skill in the registry index.
type RegistryEntry struct {
	Slug        string   `json:"slug"`
	Repo        string   `json:"repo"`        // "owner/repo"
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

// RegistryIndex is the top-level JSON structure of the registry.
type RegistryIndex struct {
	Skills []RegistryEntry `json:"skills"`
}

// RegistryClient fetches and caches the skill registry index from GitHub.
type RegistryClient struct {
	cacheDir string
	indexURL string
	cacheTTL time.Duration
}

// NewRegistryClient creates a registry client with local cache directory.
// Uses GOCLAW_REGISTRY_URL env var if set, otherwise the default GitHub URL.
func NewRegistryClient(cacheDir string) *RegistryClient {
	url := os.Getenv("GOCLAW_REGISTRY_URL")
	if url == "" {
		url = defaultRegistryURL
	}
	// Enforce HTTPS to prevent MITM.
	if !strings.HasPrefix(url, "https://") {
		slog.Warn("registry: non-HTTPS URL rejected, using default", "url", url)
		url = defaultRegistryURL
	}
	return &RegistryClient{
		cacheDir: cacheDir,
		indexURL: url,
		cacheTTL: registryCacheTTL,
	}
}

// Resolve maps a slug to an owner/repo pair via the registry index.
func (c *RegistryClient) Resolve(ctx context.Context, slug string) (owner, repo string, err error) {
	index, err := c.fetchIndex(ctx)
	if err != nil {
		return "", "", err
	}
	for _, entry := range index.Skills {
		if entry.Slug == slug {
			parts := strings.SplitN(entry.Repo, "/", 2)
			if len(parts) != 2 {
				return "", "", fmt.Errorf("registry: invalid repo format for %q: %s", slug, entry.Repo)
			}
			return parts[0], parts[1], nil
		}
	}
	return "", "", fmt.Errorf("skill %q not found in registry. Try: goclaw skills install github.com/owner/repo", slug)
}

// Search returns registry entries matching a keyword query across slug, description, and tags.
func (c *RegistryClient) Search(ctx context.Context, query string) ([]RegistryEntry, error) {
	index, err := c.fetchIndex(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var results []RegistryEntry
	for _, entry := range index.Skills {
		if matchesQuery(entry, q) {
			results = append(results, entry)
		}
	}
	return results, nil
}

// List returns all skills in the registry.
func (c *RegistryClient) List(ctx context.Context) ([]RegistryEntry, error) {
	index, err := c.fetchIndex(ctx)
	if err != nil {
		return nil, err
	}
	return index.Skills, nil
}

// Refresh forces a fresh fetch of the registry index by removing the cache.
func (c *RegistryClient) Refresh(ctx context.Context) error {
	cachePath := filepath.Join(c.cacheDir, cacheFileName)
	os.Remove(cachePath)
	_, err := c.fetchIndex(ctx)
	return err
}

// fetchIndex returns the registry index, using local cache if fresh enough.
// Falls back to stale cache if the network fetch fails.
func (c *RegistryClient) fetchIndex(ctx context.Context) (*RegistryIndex, error) {
	cachePath := filepath.Join(c.cacheDir, cacheFileName)

	// Try cache first.
	if info, err := os.Stat(cachePath); err == nil {
		if time.Since(info.ModTime()) < c.cacheTTL {
			if idx, err := c.readCache(cachePath); err == nil {
				return idx, nil
			}
		}
	}

	// Fetch from network.
	idx, fetchErr := c.fetchFromNetwork(ctx)
	if fetchErr == nil {
		// Write cache atomically (temp file + rename).
		c.writeCache(cachePath, idx)
		return idx, nil
	}

	// Network failed — try stale cache.
	if idx, err := c.readCache(cachePath); err == nil {
		slog.Warn("registry: using stale cache", "error", fetchErr)
		return idx, nil
	}

	return nil, fmt.Errorf("registry unavailable: %w", fetchErr)
}

// fetchFromNetwork downloads and parses the registry index.
func (c *RegistryClient) fetchFromNetwork(ctx context.Context) (*RegistryIndex, error) {
	ctx, cancel := context.WithTimeout(ctx, registryFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.indexURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goclaw/skill-hub")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxIndexSize {
		return nil, fmt.Errorf("registry index too large (max %d bytes)", maxIndexSize)
	}

	var idx RegistryIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("registry: invalid JSON: %w", err)
	}
	return &idx, nil
}

// readCache reads and parses the cached registry index from disk.
func (c *RegistryClient) readCache(path string) (*RegistryIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx RegistryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// writeCache atomically writes the registry index to the cache file.
func (c *RegistryClient) writeCache(path string, idx *RegistryIndex) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

// matchesQuery checks if a registry entry matches a search query (case-insensitive).
func matchesQuery(entry RegistryEntry, query string) bool {
	if strings.Contains(strings.ToLower(entry.Slug), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Description), query) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}
