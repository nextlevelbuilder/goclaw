package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// subagentAnnounceEntry holds one subagent completion result waiting to be announced.
type subagentAnnounceEntry struct {
	Label        string
	Status       string // "completed", "failed", "cancelled"
	Content      string
	Media        []bus.MediaFile
	InputTokens  int64
	OutputTokens int64
	Runtime      time.Duration
	Iterations   int
}

// subagentAnnounceRouting holds shared routing info captured by the first enqueue.
type subagentAnnounceRouting struct {
	QueueKey         string    // tenant-scoped key for sync.Map (tenantID:sessionKey)
	SessionKey       string    // original session key (no tenant prefix) for RunRequest
	TenantID         uuid.UUID // preserved for tenant-scoped scheduling
	OrigChannel      string
	OrigChannelType  string
	OrigChatID       string
	OrigPeerKind     string
	OrigLocalKey     string
	UserID           string
	ParentAgent      string
	ParentTraceID    uuid.UUID
	ParentRootSpanID uuid.UUID
	OutMeta          map[string]string
}

// subagentAnnounceQueue is a producer-consumer queue per parent session.
// Multiple subagent goroutines enqueue entries; one processor drains and merges.
type subagentAnnounceQueue struct {
	mu      sync.Mutex
	running bool
	entries []subagentAnnounceEntry
}

// subagentAnnounceQueues maps sessionKey → queue. Cleaned up when queue finishes.
var subagentAnnounceQueues sync.Map

func getOrCreateSubagentAnnounceQueue(key string) *subagentAnnounceQueue {
	v, _ := subagentAnnounceQueues.LoadOrStore(key, &subagentAnnounceQueue{})
	return v.(*subagentAnnounceQueue)
}

// enqueueSubagentAnnounce adds a result to the queue. Returns (queue, isProcessor).
// If isProcessor=true, the caller must run processSubagentAnnounceLoop.
func enqueueSubagentAnnounce(key string, entry subagentAnnounceEntry) (*subagentAnnounceQueue, bool) {
	q := getOrCreateSubagentAnnounceQueue(key)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = append(q.entries, entry)
	if q.running {
		return q, false
	}
	q.running = true
	return q, true
}

func (q *subagentAnnounceQueue) drain() []subagentAnnounceEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.entries
	q.entries = nil
	return out
}

// tryFinish atomically checks for pending entries and marks the queue idle.
func (q *subagentAnnounceQueue) tryFinish(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) > 0 {
		return false
	}
	q.running = false
	subagentAnnounceQueues.Delete(key)
	return true
}

// processSubagentAnnounceLoop drains entries, builds merged announce, schedules to parent.
func processSubagentAnnounceLoop(
	ctx context.Context,
	q *subagentAnnounceQueue,
	r subagentAnnounceRouting,
	roster tools.SubagentRoster,
	subagentMgr *tools.SubagentManager,
	sched *scheduler.Scheduler,
	msgBus *bus.MessageBus,
	cfg *config.Config,
) {
	// Ensure tenant scope is always set for the scheduler.
	if r.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, r.TenantID)
	}

	for {
		select {
		case <-ctx.Done():
			q.tryFinish(r.QueueKey)
			return
		default:
		}

		entries := q.drain()
		if len(entries) == 0 {
			if q.tryFinish(r.QueueKey) {
				return
			}
			// Brief sleep to avoid tight spin when entries arrive between drain and tryFinish.
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Refresh roster each iteration for up-to-date task statuses.
		roster = subagentMgr.RosterForParent(r.ParentAgent)
		content := buildMergedSubagentAnnounce(entries, roster)

		// Collect media from all entries.
		var fwdMedia []bus.MediaFile
		for _, e := range entries {
			fwdMedia = append(fwdMedia, e.Media...)
		}
		contentSuffix := ""
		if r.OrigChannel == "ws" && len(fwdMedia) > 0 {
			contentSuffix = mediaToMarkdownFromPaths(fwdMedia, cfg)
			fwdMedia = nil
		}

		req := agent.RunRequest{
			SessionKey:       r.SessionKey,
			Message:          content,
			ForwardMedia:     fwdMedia,
			ContentSuffix:    contentSuffix,
			Channel:          r.OrigChannel,
			ChannelType:      r.OrigChannelType,
			ChatID:           r.OrigChatID,
			PeerKind:         r.OrigPeerKind,
			LocalKey:         r.OrigLocalKey,
			UserID:           r.UserID,
			RunID:            fmt.Sprintf("subagent-announce-%s-%d", r.ParentAgent, len(entries)),
			RunKind:          "announce",
			HideInput:        true,
			Stream:           false,
			ParentTraceID:    r.ParentTraceID,
			ParentRootSpanID: r.ParentRootSpanID,
		}

		outCh := sched.Schedule(ctx, scheduler.LaneSubagent, req)
		outcome := <-outCh

		if outcome.Err != nil {
			if !errors.Is(outcome.Err, context.Canceled) {
				slog.Error("subagent announce: lead run failed", "error", outcome.Err, "batch_size", len(entries))
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  r.OrigChannel,
					ChatID:   r.OrigChatID,
					Content:  formatAgentError(outcome.Err),
					Metadata: r.OutMeta,
				})
			}
		} else {
			isSilent := outcome.Result.Content == "" || agent.IsSilentReply(outcome.Result.Content)
			if !(isSilent && len(outcome.Result.Media) == 0) {
				out := outcome.Result.Content
				if isSilent {
					out = ""
				}
				outMsg := bus.OutboundMessage{
					Channel:  r.OrigChannel,
					ChatID:   r.OrigChatID,
					Content:  out,
					Metadata: r.OutMeta,
				}
				appendMediaToOutbound(&outMsg, outcome.Result.Media)
				msgBus.PublishOutbound(outMsg)
			}
		}

		slog.Info("subagent announce: batch processed",
			"batch_size", len(entries), "session", r.SessionKey)
	}
}
