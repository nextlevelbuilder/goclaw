package skills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestCheckAll_10Repos_FastPath validates that CheckAll correctly discovers
// and caches updates for 10 packages in a single pass, then uses ETags on
// the second pass (fast path).
func TestCheckAll_10Repos_FastPath(t *testing.T) {
	// Spin up a mock GitHub API server that counts requests and respects ETags.
	hitCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		if r.Header.Get("If-None-Match") != "" {
			// Second+ pass with ETag: return 304 Not Modified.
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// First pass: return a newer release with ETag.
		w.Header().Set("ETag", `W/"etag-1"`)
		w.Header().Set("Content-Type", "application/json")
		// Extract the repo name from the request path to return a unique tag.
		repo := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/releases/latest"), "/repos/")
		newTag := "v2.0.0-" + strings.ReplaceAll(repo, "/", "-")
		_ = json.NewEncoder(w).Encode(GitHubRelease{
			TagName:     newTag,
			PublishedAt: time.Now().UTC().Add(-24 * time.Hour),
			Assets: []GitHubAsset{
				// Use darwin/linux compatible asset names to avoid filtering.
				{Name: "binary_2.0.0_linux_x86_64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
				{Name: "binary_2.0.0_linux_arm64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
				{Name: "binary_2.0.0_darwin_x86_64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
				{Name: "binary_2.0.0_darwin_arm64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
			},
		})
	}))
	defer srv.Close()

	// Create 10 GitHub package entries with unique repos, all at v1.0.0.
	entries := make([]GitHubPackageEntry, 10)
	for i := 0; i < 10; i++ {
		entries[i] = GitHubPackageEntry{
			Name:     "package" + string(rune('0'+i)),
			Repo:     "user" + string(rune('0'+i)) + "/repo" + string(rune('0'+i)),
			Tag:      "v1.0.0",
			Binaries: []string{"binary"},
		}
	}

	// Build installer pointing at our mock server.
	inst := newTestInstaller(t, srv.URL, entries)
	checker := NewGitHubUpdateChecker(inst)

	// First check: discovers all 10 updates.
	result1 := checker.Check(context.Background(), map[string]string{})
	if result1.Err != nil {
		t.Fatalf("check 1: %v", result1.Err)
	}
	if len(result1.Updates) != 10 {
		t.Fatalf("expected 10 updates, got %d: %+v", len(result1.Updates), result1.Updates)
	}
	if len(result1.ETags) != 10 {
		t.Fatalf("expected 10 ETags, got %d", len(result1.ETags))
	}

	// Second check: with ETags, should get 304 for all (fast path).
	hitCountBefore := hitCount
	result2 := checker.Check(context.Background(), result1.ETags)
	if result2.Err != nil {
		t.Fatalf("check 2: %v", result2.Err)
	}
	if len(result2.Updates) != 0 {
		t.Fatalf("expected 0 updates on fast path, got %d", len(result2.Updates))
	}
	hitCountAfter := hitCount

	// Verify that we made exactly 10 hits in the second pass (one per repo).
	hitsInCheck2 := hitCountAfter - hitCountBefore
	if hitsInCheck2 != 10 {
		t.Errorf("expected 10 hits in check 2 (ETag cache reuse), got %d", hitsInCheck2)
	}
}

// BenchmarkCheckAll10Packages measures the performance of CheckAll with 10
// GitHub package entries. First iteration is cold (no ETags), second is warm
// (with ETags; should be faster due to 304 responses).
func BenchmarkCheckAll10Packages(b *testing.B) {
	// Spin up a mock GitHub API server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Respect If-None-Match for ETag caching.
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// First request: return a newer release with ETag.
		w.Header().Set("ETag", `W/"bench-etag-1"`)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GitHubRelease{
			TagName:     "v2.0.0",
			PublishedAt: time.Now().UTC().Add(-24 * time.Hour),
			Assets: []GitHubAsset{
				// Use multi-platform asset names to avoid filtering.
				{Name: "binary_2.0.0_linux_x86_64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
				{Name: "binary_2.0.0_linux_arm64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
				{Name: "binary_2.0.0_darwin_x86_64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
				{Name: "binary_2.0.0_darwin_arm64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
			},
		})
	}))
	defer srv.Close()

	// Create 10 GitHub package entries.
	entries := make([]GitHubPackageEntry, 10)
	for i := 0; i < 10; i++ {
		entries[i] = GitHubPackageEntry{
			Name:     "bench-pkg-" + string(rune('0'+i)),
			Repo:     "user" + string(rune('0'+i)) + "/repo" + string(rune('0'+i)),
			Tag:      "v1.0.0",
			Binaries: []string{"binary"},
		}
	}

	// Create installer manually (can't use newTestInstaller on *testing.B).
	dir := b.TempDir()
	cfg := &GitHubPackagesConfig{BinDir: dir + "/bin", ManifestPath: dir + "/manifest.json"}
	cfg.Defaults()
	client := NewGitHubClient("")
	client.BaseURL = srv.URL
	inst := NewGitHubInstaller(client, cfg)
	m := &GitHubManifest{Version: 1, Packages: entries}
	if err := inst.saveManifest(m); err != nil {
		b.Fatal(err)
	}

	checker := NewGitHubUpdateChecker(inst)

	// Warm up: execute one check to populate ETags.
	warmupResult := checker.Check(context.Background(), map[string]string{})
	if warmupResult.Err != nil {
		b.Fatalf("warmup check failed: %v", warmupResult.Err)
	}

	b.ResetTimer()
	b.SetBytes(10 * 100) // Rough estimate: 10 packages × ~100 bytes of metadata per check

	// Run the benchmark: measure CheckAll with cached ETags (fast path).
	for i := 0; i < b.N; i++ {
		result := checker.Check(context.Background(), warmupResult.ETags)
		if result.Err != nil {
			b.Fatalf("iteration %d: %v", i, result.Err)
		}
	}
}
