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

func TestIsPreReleaseTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"v1.0.0", false},
		{"v1.0.0-beta", true},
		{"v1.0.0-beta.1", true},
		{"v1.0.0-rc.1", true},
		{"v1.0.0-alpha", true},
		{"v1.0.0-ALPHA", true},
		{"v0.1.0-pre", true},
		{"v0.1.0-preview", true},
		{"v0.1.0-dev", true},
		{"v1.0.0-nightly", true},
		{"v2024-01-15", false}, // date tags not considered pre-release
		{"release-42", false},
	}
	for _, tc := range cases {
		if got := isPreReleaseTag(tc.tag); got != tc.want {
			t.Errorf("isPreReleaseTag(%q) = %v, want %v", tc.tag, got, tc.want)
		}
	}
}

func TestEnsureV(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
		{"V1.2.3", "V1.2.3"},
		{"release-42", "release-42"},
	}
	for _, tc := range cases {
		if got := ensureV(tc.in); got != tc.want {
			t.Errorf("ensureV(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPickNewestRelease_SemverOrdering(t *testing.T) {
	// Current is v1.0.0 stable; candidates include v1.0.1 and v1.1.0.
	candidates := []GitHubRelease{
		{TagName: "v1.0.0"}, // same as current → skipped
		{TagName: "v1.0.1"},
		{TagName: "v1.1.0"},
	}
	best := pickNewestRelease("v1.0.0", candidates)
	if best == nil || best.TagName != "v1.1.0" {
		t.Fatalf("expected v1.1.0, got %+v", best)
	}
}

func TestPickNewestRelease_PreToStableTransition(t *testing.T) {
	// Red-team research: user on v1.0.0-rc.1, stable v1.0.0 released.
	// Both are semver-valid; semver.Compare treats stable > any prerelease.
	candidates := []GitHubRelease{
		{TagName: "v1.0.0-rc.2", Prerelease: true},
		{TagName: "v1.0.0"},
	}
	best := pickNewestRelease("v1.0.0-rc.1", candidates)
	if best == nil || best.TagName != "v1.0.0" {
		t.Fatalf("expected v1.0.0 stable, got %+v", best)
	}
}

func TestPickNewestRelease_NonSemverDowngrade_Protected(t *testing.T) {
	// Red-team H3: non-semver tags must never trigger downgrade.
	// Current 2024-01-15, candidate 2023-12-01 (older) → must NOT select.
	candidates := []GitHubRelease{
		{TagName: "2023-12-01"},
	}
	best := pickNewestRelease("2024-01-15", candidates)
	if best != nil {
		t.Fatalf("expected nil (no downgrade), got %+v", best)
	}

	// Reverse: candidate is newer by string order → select.
	candidates = []GitHubRelease{
		{TagName: "2024-05-20"},
	}
	best = pickNewestRelease("2024-01-15", candidates)
	if best == nil || best.TagName != "2024-05-20" {
		t.Fatalf("expected 2024-05-20, got %+v", best)
	}
}

func TestPickNewestRelease_MixedFormSkipped(t *testing.T) {
	// Current is semver, candidate is non-semver → skip (ambiguous).
	candidates := []GitHubRelease{
		{TagName: "release-99"},
	}
	best := pickNewestRelease("v1.0.0", candidates)
	if best != nil {
		t.Fatalf("expected nil (ambiguous), got %+v", best)
	}
}

func TestGitHubUpdateChecker_Check_HappyPath(t *testing.T) {
	server := mockReleasesServer(t)
	defer server.Close()

	inst := newTestInstaller(t, server.URL, []GitHubPackageEntry{
		{Name: "lazygit", Repo: "jesseduffield/lazygit", Tag: "v0.42.0", Binaries: []string{"lazygit"}},
	})
	checker := NewGitHubUpdateChecker(inst)
	result := checker.Check(context.Background(), map[string]string{})
	if result.Err != nil {
		t.Fatalf("check error: %v", result.Err)
	}
	if len(result.Updates) != 1 {
		t.Fatalf("expected 1 update, got %+v", result.Updates)
	}
	u := result.Updates[0]
	if u.CurrentVersion != "v0.42.0" || u.LatestVersion != "v0.44.5" {
		t.Errorf("version mismatch: %+v", u)
	}
	if u.Meta["assetName"] == "" {
		t.Errorf("asset not resolved: %+v", u.Meta)
	}
	if _, ok := result.ETags["jesseduffield/lazygit"]; !ok {
		t.Errorf("etag missing: %+v", result.ETags)
	}
}

func TestGitHubUpdateChecker_Check_NoChange(t *testing.T) {
	server := mockReleasesServer(t)
	defer server.Close()
	inst := newTestInstaller(t, server.URL, []GitHubPackageEntry{
		// Current tag matches latest — no update should surface.
		{Name: "lazygit", Repo: "jesseduffield/lazygit", Tag: "v0.44.5", Binaries: []string{"lazygit"}},
	})
	checker := NewGitHubUpdateChecker(inst)
	result := checker.Check(context.Background(), map[string]string{})
	if result.Err != nil {
		t.Fatalf("check error: %v", result.Err)
	}
	if len(result.Updates) != 0 {
		t.Fatalf("expected 0 updates, got %+v", result.Updates)
	}
}

func TestGitHubUpdateChecker_Check_ETag304(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("If-None-Match") == `W/"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `W/"abc"`)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GitHubRelease{
			TagName: "v0.44.5",
			Assets: []GitHubAsset{
				{Name: "lazygit_0.44.5_linux_x86_64.tar.gz", DownloadURL: "https://github.com/...", SizeBytes: 1},
			},
		})
	}))
	defer srv.Close()

	inst := newTestInstaller(t, srv.URL, []GitHubPackageEntry{
		{Name: "lazygit", Repo: "jesseduffield/lazygit", Tag: "v0.44.5"},
	})
	checker := NewGitHubUpdateChecker(inst)
	// First call: populates ETag.
	result := checker.Check(context.Background(), map[string]string{})
	if result.Err != nil {
		t.Fatalf("check 1: %v", result.Err)
	}
	if len(result.Updates) != 0 {
		t.Fatalf("expected no updates, got %+v", result.Updates)
	}
	// Second call with known ETag must return 304 → no new data fetched.
	result = checker.Check(context.Background(), result.ETags)
	if result.Err != nil {
		t.Fatalf("check 2: %v", result.Err)
	}
	if hits != 2 {
		t.Errorf("expected 2 hits, got %d", hits)
	}
}

// mockReleasesServer returns an httptest server answering /releases/latest
// with a canned newer release.
func mockReleasesServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Header().Set("ETag", `W/"latest-1"`)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(GitHubRelease{
				TagName:     "v0.44.5",
				PublishedAt: time.Now().UTC().Add(-24 * time.Hour),
				Assets: []GitHubAsset{
					{Name: "lazygit_0.44.5_linux_x86_64.tar.gz", DownloadURL: "https://github.com/x.tar.gz", SizeBytes: 100},
					{Name: "lazygit_0.44.5_linux_arm64.tar.gz", DownloadURL: "https://github.com/y.tar.gz", SizeBytes: 100},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
}

// newTestInstaller builds an installer pointing at a fake GitHub API server
// with a pre-seeded manifest on a temp bin dir.
func newTestInstaller(t *testing.T, baseURL string, entries []GitHubPackageEntry) *GitHubInstaller {
	t.Helper()
	dir := t.TempDir()
	cfg := &GitHubPackagesConfig{BinDir: dir + "/bin", ManifestPath: dir + "/manifest.json"}
	cfg.Defaults()
	client := NewGitHubClient("")
	client.BaseURL = baseURL
	inst := NewGitHubInstaller(client, cfg)
	m := &GitHubManifest{Version: 1, Packages: entries}
	if err := inst.saveManifest(m); err != nil {
		t.Fatal(err)
	}
	return inst
}
