package skills

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUpdateCache_LoadMissing_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadUpdateCache(filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if c == nil || len(c.Updates) != 0 || c.GitHubETags == nil {
		t.Fatalf("expected empty cache, got %+v", c)
	}
}

func TestUpdateCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")
	now := time.Now().UTC().Truncate(time.Second)
	in := &UpdateCache{
		Updates: []UpdateInfo{{
			Source: "github", Name: "lazygit",
			CurrentVersion: "v0.42.0", LatestVersion: "v0.44.5",
			CheckedAt: now,
			Meta:      map[string]any{"repo": "jesseduffield/lazygit"},
		}},
		CheckedAt:   now,
		GitHubETags: map[string]string{"jesseduffield/lazygit": `W/"abc"`},
	}
	if err := SaveUpdateCache(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadUpdateCache(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Updates) != 1 || got.Updates[0].Name != "lazygit" {
		t.Fatalf("updates mismatch: %+v", got.Updates)
	}
	if got.GitHubETags["jesseduffield/lazygit"] != `W/"abc"` {
		t.Fatalf("etag mismatch: %+v", got.GitHubETags)
	}
	if !got.CheckedAt.Equal(now) {
		t.Fatalf("checkedAt drift: got %v want %v", got.CheckedAt, now)
	}
}

func TestUpdateCache_LoadCorrupt_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadUpdateCache(path)
	if !errors.Is(err, ErrUpdateCacheCorrupt) {
		t.Fatalf("expected ErrUpdateCacheCorrupt, got %v", err)
	}
	if c == nil || len(c.Updates) != 0 {
		t.Fatalf("expected empty cache on corrupt, got %+v", c)
	}
}

func TestUpdateCache_AtomicWrite_NoPartial(t *testing.T) {
	// Verify the tmp-rename pattern doesn't leave a .tmp file on success.
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.json")
	c := &UpdateCache{GitHubETags: make(map[string]string)}
	if err := SaveUpdateCache(path, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no .tmp file after save, got err=%v", err)
	}
}

func TestUpdateCache_MergeETagsConcurrent(t *testing.T) {
	c := &UpdateCache{GitHubETags: make(map[string]string)}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.MergeETags(map[string]string{
				"repo/" + string(rune('a'+i%26)): "etag",
			})
		}(i)
	}
	wg.Wait()
	// Ensure no panic + at least some entries present.
	if len(c.GitHubETags) == 0 {
		t.Fatal("expected entries after concurrent merge")
	}
}

func TestUpdateCache_RemoveUpdate(t *testing.T) {
	c := &UpdateCache{
		GitHubETags: make(map[string]string),
		Updates: []UpdateInfo{
			{Source: "github", Name: "lazygit"},
			{Source: "github", Name: "gh"},
		},
	}
	c.RemoveUpdate("github", "lazygit")
	if len(c.Updates) != 1 || c.Updates[0].Name != "gh" {
		t.Fatalf("remove failed: %+v", c.Updates)
	}
	// No-op on absent.
	c.RemoveUpdate("github", "doesnotexist")
	if len(c.Updates) != 1 {
		t.Fatalf("no-op broke state: %+v", c.Updates)
	}
}

func TestUpdateCache_Snapshot_IndependentFromCache(t *testing.T) {
	c := &UpdateCache{
		GitHubETags: make(map[string]string),
		Updates:     []UpdateInfo{{Source: "github", Name: "a"}},
	}
	snap, _ := c.Snapshot()
	// Mutating the snapshot should not affect the cache.
	snap[0].Name = "mutated"
	got, _ := c.Snapshot()
	if got[0].Name != "a" {
		t.Fatalf("snapshot mutation leaked into cache: %+v", got)
	}
}
