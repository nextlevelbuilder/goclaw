//go:build integration

package tracing

// RetryQueueLen returns the current length of the retry queue.
// Used only in integration tests to verify failed updates are enqueued.
func (c *Collector) RetryQueueLen() int {
	return len(c.retryCh)
}

// RecoverStaleNow manually triggers the stale recovery process once.
// Used in integration tests to verify stale trace recovery without waiting
// for the periodic 30s ticker.
func (c *Collector) RecoverStaleNow() {
	c.recoverStaleOnce()
}
