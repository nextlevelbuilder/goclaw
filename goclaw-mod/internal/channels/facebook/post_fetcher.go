package facebook

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const defaultPostCacheTTL = 15 * time.Minute

// postCacheEntry holds a cached post with its expiry time.
type postCacheEntry struct {
	post      *GraphPost
	expiresAt time.Time
}

// PostFetcher fetches and caches Facebook post content and comment threads.
// singleflight prevents redundant concurrent Graph API calls for the same post
// (cache stampede on viral posts receiving many comments simultaneously).
type PostFetcher struct {
	graphClient *GraphClient
	cacheTTL    time.Duration
	cache       sync.Map          // postID(string) → *postCacheEntry
	sfGroup     singleflight.Group // coalesces concurrent fetches for the same postID
}

// NewPostFetcher creates a PostFetcher with the given cache TTL string (e.g. "15m").
// Falls back to defaultPostCacheTTL if the string is empty or unparseable.
func NewPostFetcher(client *GraphClient, cacheTTLStr string) *PostFetcher {
	ttl := defaultPostCacheTTL
	if cacheTTLStr != "" {
		if d, err := time.ParseDuration(cacheTTLStr); err == nil && d > 0 {
			ttl = d
		}
	}
	return &PostFetcher{
		graphClient: client,
		cacheTTL:    ttl,
	}
}

// GetPost returns the post for postID, using cache when fresh.
// Concurrent callers for the same postID share one inflight request.
func (pf *PostFetcher) GetPost(ctx context.Context, postID string) (*GraphPost, error) {
	if postID == "" {
		return nil, nil
	}

	// Check cache first.
	if v, ok := pf.cache.Load(postID); ok {
		entry := v.(*postCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.post, nil
		}
		pf.cache.Delete(postID)
	}

	// Coalesce concurrent fetches for the same postID.
	val, err, _ := pf.sfGroup.Do(postID, func() (any, error) {
		// Re-check cache inside the singleflight call (another goroutine may have populated it).
		if v, ok := pf.cache.Load(postID); ok {
			entry := v.(*postCacheEntry)
			if time.Now().Before(entry.expiresAt) {
				return entry.post, nil
			}
		}
		post, err := pf.graphClient.GetPost(ctx, postID)
		if err != nil {
			return nil, err
		}
		pf.cache.Store(postID, &postCacheEntry{
			post:      post,
			expiresAt: time.Now().Add(pf.cacheTTL),
		})
		return post, nil
	})
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil
	}
	return val.(*GraphPost), nil
}

// GetCommentThread fetches up to depth comments under parentCommentID.
// Not cached — thread content changes frequently as replies arrive.
func (pf *PostFetcher) GetCommentThread(ctx context.Context, parentCommentID string, depth int) ([]GraphComment, error) {
	if parentCommentID == "" {
		return nil, nil
	}
	return pf.graphClient.GetCommentThread(ctx, parentCommentID, depth)
}
