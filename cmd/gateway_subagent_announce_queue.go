package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	orch "github.com/nextlevelbuilder/goclaw/internal/orchestration"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// makeDelegateAnnounceCallback returns the batch callback used by tools.NewAnnounceQueue.
// Extracted from runGateway to keep the main function concise.
func makeDelegateAnnounceCallback(
	subagentMgr *tools.SubagentManager,
	msgBus *bus.MessageBus,
) func(sessionKey string, items []tools.AnnounceQueueItem, meta tools.AnnounceMetadata) {
	return func(sessionKey string, items []tools.AnnounceQueueItem, meta tools.AnnounceMetadata) {
		roster := subagentMgr.RosterForParent(meta.ParentAgent)
		content := tools.FormatBatchedAnnounce(items, roster)
		senderID := fmt.Sprintf("subagent:batch-%d", len(items))
		label := items[0].Label
		if len(items) > 1 {
			label = fmt.Sprintf("%d tasks", len(items))
		}
		batchMeta := map[string]string{
			tools.MetaOriginChannel:    meta.OriginChannel,
			tools.MetaOriginPeerKind:   meta.OriginPeerKind,
			tools.MetaParentAgent:      meta.ParentAgent,
			tools.MetaSubagentLabel:    label,
			tools.MetaOriginTraceID:    meta.OriginTraceID,
			tools.MetaOriginRootSpanID: meta.OriginRootSpanID,
		}
		if meta.OriginLocalKey != "" {
			batchMeta[tools.MetaOriginLocalKey] = meta.OriginLocalKey
		}
		if meta.OriginSessionKey != "" {
			batchMeta[tools.MetaOriginSessionKey] = meta.OriginSessionKey
		}
		// Collect media from all items in the batch.
		var batchMedia []bus.MediaFile
		for _, item := range items {
			batchMedia = append(batchMedia, item.Media...)
		}
		// Notify clients that leader is processing team results
		// (bridges UI gap between last task.completed and announce run.started).
		bus.BroadcastForTenant(msgBus, protocol.EventTeamLeaderProcessing, meta.OriginTenantID, map[string]any{
			"agentId": meta.ParentAgent,
			"tasks":   len(items),
		})

		msgBus.PublishInbound(bus.InboundMessage{
			Channel:  "system",
			SenderID: senderID,
			ChatID:   meta.OriginChatID,
			Content:  content,
			UserID:   meta.OriginUserID,
			TenantID: meta.OriginTenantID,
			Metadata: batchMeta,
			Media:    batchMedia,
		})
	}
}

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

// subagentAnnounceQueue uses BatchQueue for producer-consumer synchronization.
var subagentAnnounceQueue orch.BatchQueue[subagentAnnounceEntry]

// enqueueSubagentAnnounce adds a result to the queue. Returns isProcessor.
func enqueueSubagentAnnounce(key string, entry subagentAnnounceEntry) bool {
	return subagentAnnounceQueue.Enqueue(key, entry)
}

// processSubagentAnnounceLoop drains entries, builds merged announce, schedules to parent.
func processSubagentAnnounceLoop(
	ctx context.Context,
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
			subagentAnnounceQueue.TryFinish(r.QueueKey)
			return
		default:
		}

		entries := subagentAnnounceQueue.Drain(r.QueueKey)
		if len(entries) == 0 {
			if subagentAnnounceQueue.TryFinish(r.QueueKey) {
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
