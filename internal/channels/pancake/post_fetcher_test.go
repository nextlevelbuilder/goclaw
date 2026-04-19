package pancake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func makePostFetcherClient(t *testing.T, srv *httptest.Server) *APIClient {
	t.Helper()
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.pageV2BaseURL = srv.URL
	client.httpClient = srv.Client()
	return client
}

func TestNewPostFetcher_DefaultTTL(t *testing.T) {
	client := NewAPIClient("u", "p", "page-1")

	pf1 := NewPostFetcher(client, "")
	if pf1.cacheTTL != defaultPostCacheTTL {
		t.Errorf("empty string: cacheTTL = %v, want %v", pf1.cacheTTL, defaultPostCacheTTL)
	}

	pf2 := NewPostFetcher(client, "bogus")
	if pf2.cacheTTL != defaultPostCacheTTL {
		t.Errorf("bogus: cacheTTL = %v, want %v", pf2.cacheTTL, defaultPostCacheTTL)
	}

	pf3 := NewPostFetcher(client, "30m")
	if pf3.cacheTTL != 30*time.Minute {
		t.Errorf("30m: cacheTTL = %v, want 30m", pf3.cacheTTL)
	}
}

func TestPostFetcher_GetPost_EmptyPageIDReturnsNil(t *testing.T) {
	client := NewAPIClient("u", "p", "page-1")
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	post, err := pf.GetPost(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Errorf("expected nil post for empty postID, got %+v", post)
	}
}

func TestPostFetcher_GetPost_CacheHit(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"data": []PancakePost{}})
	}))
	defer srv.Close()

	client := makePostFetcherClient(t, srv)
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	// Pre-populate cache.
	pf.cache.Store("post-1", &postCacheEntry{
		post:      &PancakePost{ID: "post-1", Message: "cached"},
		expiresAt: time.Now().Add(time.Hour),
	})

	post, err := pf.GetPost(context.Background(), "post-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post == nil || post.Message != "cached" {
		t.Errorf("expected cached post, got %+v", post)
	}
	if callCount.Load() != 0 {
		t.Errorf("expected 0 API calls, got %d", callCount.Load())
	}
}

func TestPostFetcher_GetPost_CacheMiss(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"data": []PancakePost{{ID: "post-1", Message: "fresh"}},
		})
	}))
	defer srv.Close()

	client := makePostFetcherClient(t, srv)
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	post, err := pf.GetPost(context.Background(), "post-1")
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	if post == nil || post.Message != "fresh" {
		t.Errorf("expected fresh post, got %+v", post)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 API call, got %d", callCount.Load())
	}

	// Second call should be a cache hit — no additional API calls.
	_, err = pf.GetPost(context.Background(), "post-1")
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected still 1 API call after cache hit, got %d", callCount.Load())
	}
}

func TestPostFetcher_GetPost_CacheExpiry(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"data": []PancakePost{{ID: "post-1", Message: "refetched"}},
		})
	}))
	defer srv.Close()

	client := makePostFetcherClient(t, srv)
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	// Pre-populate with expired entry.
	pf.cache.Store("post-1", &postCacheEntry{
		post:      &PancakePost{ID: "post-1", Message: "stale"},
		expiresAt: time.Now().Add(-time.Minute), // already expired
	})

	post, err := pf.GetPost(context.Background(), "post-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post == nil || post.Message != "refetched" {
		t.Errorf("expected refetched post, got %+v", post)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 API call after cache expiry, got %d", callCount.Load())
	}
}

func TestPostFetcher_GetPost_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := makePostFetcherClient(t, srv)
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	post, err := pf.GetPost(context.Background(), "post-1")
	if err == nil {
		t.Fatal("expected error from API 500, got nil")
	}
	if post != nil {
		t.Errorf("expected nil post on error, got %+v", post)
	}
}

func TestPostFetcher_GetPost_PostNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []PancakePost{}})
	}))
	defer srv.Close()

	client := makePostFetcherClient(t, srv)
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	post, err := pf.GetPost(context.Background(), "missing-post")
	if err != nil {
		t.Fatalf("expected nil error for missing post (graceful degradation), got %v", err)
	}
	if post != nil {
		t.Errorf("expected nil post for missing post, got %+v", post)
	}
}

func TestPostFetcher_GetPost_SingleflightCoalescing(t *testing.T) {
	var callCount atomic.Int32
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-ready // block until all goroutines have started
		callCount.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"data": []PancakePost{{ID: "post-1", Message: "coalesced"}},
		})
	}))
	defer srv.Close()

	client := makePostFetcherClient(t, srv)
	pf := NewPostFetcher(client, "")
	pf.stopCtx = context.Background()

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			pf.GetPost(context.Background(), "post-1") //nolint:errcheck
		}()
	}

	// Give goroutines time to pile up in singleflight, then unblock.
	time.Sleep(20 * time.Millisecond)
	close(ready)
	wg.Wait()

	if callCount.Load() != 1 {
		t.Errorf("singleflight: expected 1 API call, got %d", callCount.Load())
	}
}
