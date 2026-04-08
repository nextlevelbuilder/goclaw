package orchestration

import "sync"

// BatchQueue is a generic producer-consumer queue keyed by string.
// Multiple goroutines enqueue entries; one processor drains and processes batches.
// Pattern: Enqueue returns isProcessor=true for the first enqueue (that goroutine
// must run the processing loop). Subsequent enqueues return false.
type BatchQueue[T any] struct {
	queues sync.Map // key -> *batchQueueState[T]
}

type batchQueueState[T any] struct {
	mu      sync.Mutex
	running bool
	entries []T
}

// Enqueue adds an entry to the queue for the given key.
// Returns isProcessor=true if the caller is the first goroutine and must
// run the processing loop (Drain → process → TryFinish).
func (bq *BatchQueue[T]) Enqueue(key string, entry T) bool {
	v, _ := bq.queues.LoadOrStore(key, &batchQueueState[T]{})
	q := v.(*batchQueueState[T])
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = append(q.entries, entry)
	if q.running {
		return false
	}
	q.running = true
	return true
}

// Drain atomically takes all pending entries from the queue.
// Returns nil if no entries are pending.
func (bq *BatchQueue[T]) Drain(key string) []T {
	v, ok := bq.queues.Load(key)
	if !ok {
		return nil
	}
	q := v.(*batchQueueState[T])
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.entries
	q.entries = nil
	return out
}

// TryFinish atomically checks for pending entries and marks the queue idle.
// Returns true if the processor should exit (no pending entries).
// Prevents TOCTOU race between checking and finishing.
func (bq *BatchQueue[T]) TryFinish(key string) bool {
	v, ok := bq.queues.Load(key)
	if !ok {
		return true
	}
	q := v.(*batchQueueState[T])
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) > 0 {
		return false // more work arrived
	}
	q.running = false
	bq.queues.Delete(key)
	return true
}
