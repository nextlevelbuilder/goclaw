package scheduler

import "errors"

var (
	// ErrQueueFull is returned when a message is rejected because the session queue is full (drop=new policy).
	ErrQueueFull = errors.New("session queue is full")

	// ErrQueueDropped is returned when a queued message is evicted to make room (drop=old policy).
	ErrQueueDropped = errors.New("message dropped from queue")

	// ErrMessageStale is returned when a queued message is skipped because it was
	// enqueued before an abort (/stopall) and is no longer relevant.
	ErrMessageStale = errors.New("message stale: enqueued before abort")

	// ErrGatewayDraining is returned when the gateway is shutting down and
	// new requests cannot be accepted.
	ErrGatewayDraining = errors.New("gateway is shutting down, please retry shortly")

	// ErrLaneCleared is returned when a session queue is reset (e.g. during restart)
	// and all pending requests are drained.
	ErrLaneCleared = errors.New("session queue cleared during reset")
)
