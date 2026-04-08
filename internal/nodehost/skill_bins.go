package nodehost

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SkillBinsCache provides thread-safe access to trusted skill binary entries with a TTL.
type SkillBinsCache struct {
	mu          sync.RWMutex
	bins        []SkillBinTrustEntry
	lastRefresh time.Time
	ttl         time.Duration
	fetch       func(ctx context.Context) ([]string, error)
	pathEnv     string
}

// NewSkillBinsCache creates a new cache with a 90s TTL.
func NewSkillBinsCache(fetch func(ctx context.Context) ([]string, error), pathEnv string) *SkillBinsCache {
	return &SkillBinsCache{
		ttl:     90 * time.Second,
		fetch:   fetch,
		pathEnv: pathEnv,
	}
}

// Current returns cached entries, refreshing if TTL expired or force is true.
func (c *SkillBinsCache) Current(force bool) ([]SkillBinTrustEntry, error) {
	c.mu.RLock()
	if !force && time.Since(c.lastRefresh) < c.ttl {
		bins := c.bins
		c.mu.RUnlock()
		return bins, nil
	}
	c.mu.RUnlock()

	c.doRefresh(force)

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bins, nil
}

func (c *SkillBinsCache) doRefresh(force bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock (skip if forced).
	if !force && time.Since(c.lastRefresh) < c.ttl {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	names, err := c.fetch(ctx)
	if err != nil {
		if c.lastRefresh.IsZero() {
			c.bins = nil
		}
		return
	}
	c.bins = resolveSkillBinTrustEntries(names, c.pathEnv)
	c.lastRefresh = time.Now()
}

// resolveSkillBinTrustEntries resolves binary names to path entries, deduplicating and sorting.
func resolveSkillBinTrustEntries(names []string, pathEnv string) []SkillBinTrustEntry {
	var entries []SkillBinTrustEntry
	seen := make(map[string]bool)
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		resolved := resolveExecutableFromPathEnv(name, pathEnv)
		if resolved == "" {
			continue
		}
		key := name + "\x00" + resolved
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, SkillBinTrustEntry{Name: name, ResolvedPath: resolved})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].ResolvedPath < entries[j].ResolvedPath
	})
	return entries
}

// resolveExecutableFromPathEnv resolves a binary name to its absolute path using PATH.
func resolveExecutableFromPathEnv(bin, pathEnv string) string {
	if strings.ContainsAny(bin, "/\\") {
		return ""
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, bin)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		// Check executable bit.
		if info.Mode()&0o111 == 0 {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err == nil {
			return abs
		}
	}
	return ""
}
