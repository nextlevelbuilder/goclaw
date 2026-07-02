package replycontext

import (
	"strings"
	"time"
)

func scopedKey(scope Scope, messageID string) cacheKey {
	return cacheKey{
		tenant:  strings.TrimSpace(scope.TenantID),
		channel: strings.TrimSpace(scope.ChannelID),
		thread:  strings.TrimSpace(scope.ThreadID),
		message: strings.TrimSpace(messageID),
	}
}

func (c *Cache) lookupLocked(scope Scope, ids []string) *node {
	for _, id := range ids {
		key := scopedKey(scope, id)
		if canonical, ok := c.aliases[key]; ok {
			return c.nodes[canonical]
		}
		if n := c.nodes[key]; n != nil {
			return n
		}
	}
	return nil
}

func (c *Cache) collectChainLocked(scope Scope, start *node) []renderMessage {
	visited := make(map[cacheKey]struct{}, c.opts.MaxDepth)
	reversed := make([]renderMessage, 0, c.opts.MaxDepth)
	for n := start; n != nil && len(reversed) < c.opts.MaxDepth; {
		if _, ok := visited[n.key]; ok {
			break
		}
		visited[n.key] = struct{}{}
		reversed = append(reversed, renderMessage{sender: n.sender, body: n.body})
		n = c.lookupLocked(scope, n.parentIDs)
	}

	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

func (c *Cache) purgeExpiredLocked(now time.Time) {
	if c.opts.TTL <= 0 {
		return
	}
	for key, n := range c.nodes {
		if now.Sub(n.createdAt) > c.opts.TTL {
			c.deleteNodeLocked(key, n)
		}
	}
}

func (c *Cache) evictLocked() {
	for len(c.nodes) > c.opts.MaxEntries {
		var oldestKey cacheKey
		var oldest *node
		for key, n := range c.nodes {
			if oldest == nil || n.createdAt.Before(oldest.createdAt) {
				oldestKey = key
				oldest = n
			}
		}
		if oldest == nil {
			return
		}
		c.deleteNodeLocked(oldestKey, oldest)
	}
}

func (c *Cache) deleteNodeLocked(key cacheKey, n *node) {
	delete(c.nodes, key)
	for _, id := range n.ids {
		aliasKey := cacheKey{tenant: key.tenant, channel: key.channel, thread: key.thread, message: id}
		if c.aliases[aliasKey] == key {
			delete(c.aliases, aliasKey)
		}
	}
}
