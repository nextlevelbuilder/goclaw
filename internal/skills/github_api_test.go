package skills

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubClient_GetRelease(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer testtoken" {
			t.Errorf("expected bearer token, got %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/repos/cli/cli/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"tag_name": "v2.45.0",
				"name": "v2.45.0",
				"published_at": "2025-01-01T00:00:00Z",
				"prerelease": false,
				"assets": [
					{"name": "gh_linux_amd64.tar.gz", "browser_download_url": "https://github.com/cli/cli/releases/download/v2.45.0/gh.tar.gz", "size": 1024}
				]
			}`))
		case "/repos/missing/repo/releases/latest":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewGitHubClient("testtoken")
	c.BaseURL = srv.URL

	rel, err := c.GetRelease(context.Background(), "cli", "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v2.45.0" || len(rel.Assets) != 1 {
		t.Errorf("unexpected release %+v", rel)
	}

	// Cache hit shouldn't increment hits.
	first := hits
	rel2, err := c.GetRelease(context.Background(), "cli", "cli", "")
	if err != nil {
		t.Fatal(err)
	}
	if rel2.TagName != rel.TagName {
		t.Error("cache returned different release")
	}
	if hits != first {
		t.Errorf("expected cache hit, got %d → %d", first, hits)
	}

	_, err = c.GetRelease(context.Background(), "missing", "repo", "")
	if !errors.Is(err, ErrGitHubNotFound) {
		t.Errorf("want ErrGitHubNotFound, got %v", err)
	}
}

func TestGitHubClient_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "9999999999")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := NewGitHubClient("")
	c.BaseURL = srv.URL
	_, err := c.GetRelease(context.Background(), "x", "y", "v1")
	if !errors.Is(err, ErrGitHubRateLimited) {
		t.Errorf("want ErrGitHubRateLimited, got %v", err)
	}
}
