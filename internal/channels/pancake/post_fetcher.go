package pancake

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const defaultPostCacheTTL = 15 * time.Minute

type postCacheEntry struct {
	post      *PancakePost // nil = negative cache (post not found)
	expiresAt time.Time
}

// PostFetcher caches page post content for comment context enrichment.
// singleflight prevents redundant concurrent Pancake API calls for the same page.
// Unlike Facebook (per-post Graph API), Pancake requires listing all posts then finding by ID,
// so the singleflight key is "posts" (one call fetches all, results cached individually).
type PostFetcher struct {
	apiClient *APIClient
	cacheTTL  time.Duration
	cache     sync.Map           // postID(string) -> *postCacheEntry
	sfGroup   singleflight.Group
	stopCtx   context.Context // channel-lifetime context — used inside singleflight to avoid per-request cancellation
}

// NewPostFetcher creates a PostFetcher. cacheTTLStr is parsed as time.Duration;
// empty or invalid values fall back to defaultPostCacheTTL.
func NewPostFetcher(client *APIClient, cacheTTLStr string) *PostFetcher {
	ttl := defaultPostCacheTTL
	if cacheTTLStr != "" {
		if d, err := time.ParseDuration(cacheTTLStr); err == nil && d > 0 {
			ttl = d
		}
	}
	return &PostFetcher{
		apiClient: client,
		cacheTTL:  ttl,
	}
}

// GetPost returns a cached post by ID, or fetches all recent posts and caches them.
// Returns (nil, nil) if postID is empty or post not found — graceful degradation.
// Uses stopCtx inside singleflight to avoid per-request cancellation killing shared fetches.
// Stores negative cache entries for missing posts to prevent repeated API stampedes.
func (pf *PostFetcher) GetPost(ctx context.Context, postID string) (*PancakePost, error) {
	if postID == "" {
		return nil, nil
	}

	// Check cache.
	if v, ok := pf.cache.Load(postID); ok {
		entry := v.(*postCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.post, nil // may be nil (negative cache)
		}
		pf.cache.Delete(postID)
	}

	// Coalesce concurrent fetches via singleflight keyed on "posts".
	// All posts are fetched in one API call and cached individually.
	fetchCtx := pf.stopCtx
	if fetchCtx == nil {
		fetchCtx = ctx
	}
	_, err, _ := pf.sfGroup.Do("posts", func() (any, error) {
		posts, err := pf.apiClient.GetPosts(fetchCtx, 50)
		if err != nil {
			return nil, err
		}
		expires := time.Now().Add(pf.cacheTTL)
		for i := range posts {
			pf.cache.Store(posts[i].ID, &postCacheEntry{
				post:      &posts[i],
				expiresAt: expires,
			})
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}

	// Re-check cache after fetch.
	if v, ok := pf.cache.Load(postID); ok {
		return v.(*postCacheEntry).post, nil
	}
	// Store negative cache entry — prevents re-fetching for old/missing posts.
	pf.cache.Store(postID, &postCacheEntry{post: nil, expiresAt: time.Now().Add(pf.cacheTTL)})
	return nil, nil
}
