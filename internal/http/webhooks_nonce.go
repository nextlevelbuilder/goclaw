package http

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// webhookNonceTTL is the replay-protection window.
	// Must exceed webhookHMACSkewSeconds (300s) so that a signature first seen at
	// the edge of the skew window remains cached until the skew window closes.
	// 320s = 300s skew + 20s slack. Note: a replay attempted after TTL expiry
	// is also rejected by the timestamp skew check independently, so the nonce
	// cache and skew check form complementary (not overlapping) defenses.
	webhookNonceTTL = 320 * time.Second

	// webhookNonceSweepInterval controls how often expired entries are evicted.
	webhookNonceSweepInterval = 60 * time.Second

	// webhookNonceMaxEntries is a defensive ceiling — if exceeded the sweep runs
	// immediately to bound memory growth under DoS conditions.
	webhookNonceMaxEntries = 100_000
)

// webhookNonceEntry holds the expiry timestamp for a cached nonce.
type webhookNonceEntry struct {
	expiresAt int64 // Unix nanoseconds
}

// webhookNonceCache is a per-process, in-memory replay-protection store for
// HMAC-signed webhook requests. It caches sha256(tenantID|"|"|signatureHex)
// for webhookNonceTTL after first use. Subsequent requests with the same
// signature within the TTL are rejected as replays.
//
// Single-node caveat: this cache is not distributed. In a multi-node deployment
// a replay may succeed on a different node. Acceptable for current architecture
// (single-process gateway). Document in docs/webhooks.md.
//
// Thread-safe: uses sync.Map for concurrent access.
type webhookNonceCache struct {
	entries sync.Map
	count   atomic.Int64
	ttl     time.Duration
	stopCh  chan struct{}
}

// newWebhookNonceCache creates a cache with TTL sweep goroutine.
// Caller must call Stop() when done (typically at process shutdown).
func newWebhookNonceCache() *webhookNonceCache {
	c := &webhookNonceCache{
		ttl:    webhookNonceTTL,
		stopCh: make(chan struct{}),
	}
	go c.sweepLoop()
	return c
}

// nonceKey builds a cache key from tenantID and the hex-encoded HMAC signature.
// Using sha256 to bound key size regardless of input length.
func nonceKey(tenantID, signatureHex string) string {
	h := sha256.Sum256([]byte(tenantID + "|" + signatureHex))
	return fmt.Sprintf("%x", h)
}

// Seen returns true if this nonce was already seen within the TTL window,
// indicating a replay attempt. Returns false on first observation and records
// the nonce for future replay detection.
//
// Atomicity note: sync.Map.LoadOrStore provides the compare-and-swap semantics
// needed here — only one goroutine wins the "insert" race.
func (c *webhookNonceCache) Seen(key string) bool {
	entry := webhookNonceEntry{
		expiresAt: time.Now().Add(c.ttl).UnixNano(),
	}
	_, loaded := c.entries.LoadOrStore(key, entry)
	if !loaded {
		// First time seen — we inserted it.
		n := c.count.Add(1)
		if n >= webhookNonceMaxEntries {
			// Defensive: sweep immediately under potential DoS load.
			go c.sweep()
		}
	}
	// loaded=true → key was already present → replay.
	return loaded
}

// Stop halts the background sweep goroutine.
func (c *webhookNonceCache) Stop() {
	close(c.stopCh)
}

// sweepLoop runs periodic expired-entry eviction.
func (c *webhookNonceCache) sweepLoop() {
	ticker := time.NewTicker(webhookNonceSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.sweep()
		case <-c.stopCh:
			return
		}
	}
}

// sweep evicts all expired entries from the map.
func (c *webhookNonceCache) sweep() {
	now := time.Now().UnixNano()
	c.entries.Range(func(k, v any) bool {
		entry, ok := v.(webhookNonceEntry)
		if !ok || now > entry.expiresAt {
			c.entries.Delete(k)
			c.count.Add(-1)
		}
		return true
	})
}
