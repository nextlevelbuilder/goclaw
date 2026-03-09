package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// AnnounceQueueItem represents a single subagent result waiting to be announced.
type AnnounceQueueItem struct {
	SubagentID string
	Label      string
	Status     string // "completed", "failed", "cancelled"
	Result     string
	Media      []bus.MediaFile // media files from tool results
	Runtime    time.Duration
	Iterations int
}

// AnnounceMetadata carries origin info for routing the batched announce.
type AnnounceMetadata struct {
	OriginChannel    string
	OriginChatID     string
	OriginPeerKind   string
	OriginLocalKey   string // composite key with topic/thread suffix for routing
	OriginUserID     string
	OriginSessionKey string // exact parent session key (WS uses non-standard format)
	ParentAgent      string
	OriginTraceID    string // parent trace UUID for announce linking
	OriginRootSpanID string // parent agent's root span UUID
}

// AnnounceQueue batches subagent announces per session with debounce,
// matching TS subagent-announce-queue.ts "collect" mode.
type AnnounceQueue struct {
	mu       sync.Mutex
	queues   map[string]*sessionQueue // session key → queue
	debounce time.Duration            // default 1000ms
	cap      int                      // max items per session before immediate drain (default 20)
	onDrain  func(sessionKey string, items []AnnounceQueueItem, meta AnnounceMetadata)

	// countActiveFunc returns the number of still-running subagents for a parent.
	// Used at drain time for accurate remaining-active count.
	countActiveFunc func(parentID string) int
}

type sessionQueue struct {
	items []AnnounceQueueItem
	timer *time.Timer
	meta  AnnounceMetadata
}

// NewAnnounceQueue creates an announce queue with the given debounce and drain callback.
func NewAnnounceQueue(
	debounceMs int,
	cap int,
	onDrain func(sessionKey string, items []AnnounceQueueItem, meta AnnounceMetadata),
	countActive func(parentID string) int,
) *AnnounceQueue {
	if debounceMs <= 0 {
		debounceMs = 1000 // TS default
	}
	if cap <= 0 {
		cap = 20 // TS default
	}
	return &AnnounceQueue{
		queues:          make(map[string]*sessionQueue),
		debounce:        time.Duration(debounceMs) * time.Millisecond,
		cap:             cap,
		onDrain:         onDrain,
		countActiveFunc: countActive,
	}
}

// Enqueue adds an announce item to the session queue.
// Resets the debounce timer. Drains immediately if cap is reached.
func (aq *AnnounceQueue) Enqueue(sessionKey string, item AnnounceQueueItem, meta AnnounceMetadata) {
	aq.mu.Lock()
	defer aq.mu.Unlock()

	sq, ok := aq.queues[sessionKey]
	if !ok {
		sq = &sessionQueue{meta: meta}
		aq.queues[sessionKey] = sq
	}

	sq.items = append(sq.items, item)

	// Drain immediately if cap reached
	if len(sq.items) >= aq.cap {
		if sq.timer != nil {
			sq.timer.Stop()
		}
		items := sq.items
		sqMeta := sq.meta
		delete(aq.queues, sessionKey)
		go aq.drain(sessionKey, items, sqMeta)
		return
	}

	// Reset debounce timer
	if sq.timer != nil {
		sq.timer.Stop()
	}
	sq.timer = time.AfterFunc(aq.debounce, func() {
		aq.mu.Lock()
		sq, ok := aq.queues[sessionKey]
		if !ok {
			aq.mu.Unlock()
			return
		}
		items := sq.items
		sqMeta := sq.meta
		delete(aq.queues, sessionKey)
		aq.mu.Unlock()

		aq.drain(sessionKey, items, sqMeta)
	})
}

// drain merges items into a single announce message and calls onDrain.
func (aq *AnnounceQueue) drain(sessionKey string, items []AnnounceQueueItem, meta AnnounceMetadata) {
	if len(items) == 0 || aq.onDrain == nil {
		return
	}

	aq.onDrain(sessionKey, items, meta)
}

// FormatBatchedAnnounce builds the announce content for a batch of items.
// remainingActive is the count of still-running subagents at drain time.
func FormatBatchedAnnounce(items []AnnounceQueueItem, remainingActive int) string {
	if len(items) == 1 {
		// Single item: use the same format as before (no batching overhead)
		item := items[0]
		statusLabel := "completed successfully"
		switch item.Status {
		case "failed":
			statusLabel = "failed: " + item.Result
		case "cancelled":
			statusLabel = "was cancelled"
		}

		replyInstruction := buildReplyInstruction(remainingActive)

		return fmt.Sprintf(
			"[System Message] A subagent task %q just %s.\n\n"+
				"Result:\n%s\n\n"+
				"Stats: runtime %s, iterations %d\n\n"+
				"%s",
			item.Label, statusLabel, item.Result,
			item.Runtime.Round(time.Millisecond), item.Iterations,
			replyInstruction,
		)
	}

	// Multiple items: batched format
	var sb strings.Builder
	sb.WriteString("[System Message] Multiple subagent tasks completed:\n")

	for i, item := range items {
		statusLabel := "completed"
		switch item.Status {
		case "failed":
			statusLabel = "failed"
		case "cancelled":
			statusLabel = "cancelled"
		}

		sb.WriteString(fmt.Sprintf(
			"\n---\nTask #%d: %q %s (runtime %s, iterations %d)\nResult: %s\n",
			i+1, item.Label, statusLabel,
			item.Runtime.Round(time.Millisecond), item.Iterations,
			item.Result,
		))
	}

	sb.WriteString("---\n\n")
	sb.WriteString(buildReplyInstruction(remainingActive))

	return sb.String()
}

func buildReplyInstruction(remainingActive int) string {
	if remainingActive > 0 {
		runsLabel := "runs"
		if remainingActive == 1 {
			runsLabel = "run"
		}
		return fmt.Sprintf(
			"There are still %d active subagent %s for this session. "+
				"If they are part of the same workflow, wait for the remaining results "+
				"before sending a user update. If they are unrelated, respond normally "+
				"using only the result above. "+
				"Do NOT copy or echo the [System Message] block verbatim — rewrite in your own voice. "+
				"Reply ONLY: NO_REPLY if this result was already delivered to the user.",
			remainingActive, runsLabel,
		)
	}

	return "A completed subagent task is ready for user delivery. " +
		"Convert the result above into your normal assistant voice and " +
		"send that user-facing update now. Keep this internal context private " +
		"(don't mention system/log/stats/session details or announce type), " +
		"and do NOT copy the [System Message] block verbatim. " +
		"Reply ONLY: NO_REPLY if this exact result was already delivered to the user."
}
