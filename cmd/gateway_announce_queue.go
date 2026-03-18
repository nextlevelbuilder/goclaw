package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// announceEntry holds one teammate completion result waiting to be announced.
type announceEntry struct {
	MemberAgent        string // agent key (e.g. "researcher")
	MemberDisplayName  string // display name (e.g. "Nhà Nghiên Cứu"), empty if not set
	Content            string
	Media              []agent.MediaResult
}

// announceQueueState tracks the per-session announce queue.
// Producer-consumer: multiple goroutines add entries, one loops to drain+announce.
type announceQueueState struct {
	mu      sync.Mutex
	running bool
	entries []announceEntry
}

// announceQueues maps leadSessionKey → queue. Cleaned up when queue finishes.
var announceQueues sync.Map

func getOrCreateAnnounceQueue(key string) *announceQueueState {
	v, _ := announceQueues.LoadOrStore(key, &announceQueueState{})
	return v.(*announceQueueState)
}

// enqueueAnnounce adds a result to the queue. Returns (queue, isProcessor).
// If isProcessor=true, the caller must run processAnnounceLoop.
func enqueueAnnounce(key string, entry announceEntry) (*announceQueueState, bool) {
	q := getOrCreateAnnounceQueue(key)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = append(q.entries, entry)
	if q.running {
		return q, false
	}
	q.running = true
	return q, true
}

func (q *announceQueueState) drain() []announceEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.entries
	q.entries = nil
	return out
}

// tryFinish atomically checks for pending entries and marks the queue idle.
// Returns true if the processor should exit (no pending entries).
// Prevents TOCTOU race between hasPending() and finish().
func (q *announceQueueState) tryFinish(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) > 0 {
		return false // more work arrived — keep processing
	}
	q.running = false
	announceQueues.Delete(key)
	return true
}

// announceRouting holds the shared routing info captured by the first goroutine.
type announceRouting struct {
	LeadAgent        string
	LeadSessionKey   string
	OrigChannel      string
	OrigChatID       string
	OrigPeerKind     string
	OrigLocalKey     string
	OriginUserID     string
	TeamID           string
	TeamWorkspace    string
	OriginTraceID    string
	ParentTraceID    uuid.UUID
	ParentRootSpanID uuid.UUID
	OutMeta          map[string]string
}

// processAnnounceLoop drains entries, builds merged announce, schedules to leader.
// Loops until queue is empty.
func processAnnounceLoop(
	ctx context.Context,
	q *announceQueueState,
	r announceRouting,
	sched *scheduler.Scheduler,
	msgBus *bus.MessageBus,
	teamStore store.TeamStore,
	postTurn tools.PostTurnProcessor,
	cfg *config.Config,
) {
	for {
		entries := q.drain()
		if len(entries) == 0 {
			if q.tryFinish(r.LeadSessionKey) {
				return
			}
			continue // entries arrived between drain and tryFinish
		}

		// Build task board snapshot (latest state, after all completions in this batch).
		snapshot := ""
		if r.TeamID != "" && r.OriginTraceID != "" {
			if teamUUID, err := uuid.Parse(r.TeamID); err == nil {
				snapshot = buildTaskBoardSnapshot(ctx, teamStore, teamUUID, r.OrigChatID, r.OriginTraceID)
			}
		}

		content := buildMergedAnnounceContent(entries, snapshot, r.TeamWorkspace)

		req := agent.RunRequest{
			SessionKey:       r.LeadSessionKey,
			Message:          content,
			Channel:          r.OrigChannel,
			ChatID:           r.OrigChatID,
			PeerKind:         r.OrigPeerKind,
			LocalKey:         r.OrigLocalKey,
			UserID:           r.OriginUserID,
			RunID:            fmt.Sprintf("teammate-announce-%s-%d", r.LeadAgent, len(entries)),
			RunKind:          "announce",
			HideInput:        true,
			Stream:           false,
			TeamID:           r.TeamID,
			ParentTraceID:    r.ParentTraceID,
			ParentRootSpanID: r.ParentRootSpanID,
		}
		// Collect all media from entries.
		for _, e := range entries {
			for _, mr := range e.Media {
				req.ForwardMedia = append(req.ForwardMedia, bus.MediaFile{
					Path:     mr.Path,
					MimeType: mr.ContentType,
				})
			}
		}
		// WS channel has no outbound media handler — deliver via ContentSuffix.
		if r.OrigChannel == "ws" && len(req.ForwardMedia) > 0 {
			req.ContentSuffix = mediaToMarkdownFromPaths(req.ForwardMedia, cfg)
			req.ForwardMedia = nil
		}

		// Inject post-turn tracker (leader may create new tasks during announce).
		ptd := tools.NewPendingTeamDispatch()
		defer ptd.ReleaseTeamLock() // ensure lock released even on panic
		schedCtx := tools.WithPendingTeamDispatch(ctx, ptd)
		outCh := sched.Schedule(schedCtx, scheduler.LaneSubagent, req)
		outcome := <-outCh

		ptd.ReleaseTeamLock()
		if postTurn != nil {
			for tid, tIDs := range ptd.Drain() {
				if err := postTurn.ProcessPendingTasks(ctx, tid, tIDs); err != nil {
					slog.Warn("post_turn(announce): failed", "team_id", tid, "error", err)
				}
			}
		}

		if outcome.Err != nil {
			slog.Error("teammate announce: lead run failed", "error", outcome.Err, "batch_size", len(entries))
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

		slog.Info("teammate announce: batch processed",
			"batch_size", len(entries), "session", r.LeadSessionKey)

		// Loop back — tryFinish at top will exit when queue is truly empty.
	}
}

// memberLabel returns a display-friendly name for announce messages.
// Uses "DisplayName (agent_key)" if display name is set, otherwise just agent_key.
func memberLabel(e announceEntry) string {
	if e.MemberDisplayName != "" {
		return fmt.Sprintf("%s (%s)", e.MemberDisplayName, e.MemberAgent)
	}
	return e.MemberAgent
}

// buildMergedAnnounceContent creates the announce message for one or more completed/failed tasks.
func buildMergedAnnounceContent(entries []announceEntry, taskBoardSnapshot, teamWorkspace string) string {
	var sb strings.Builder

	if len(entries) == 1 {
		e := entries[0]
		label := memberLabel(e)
		if strings.HasPrefix(e.Content, "[FAILED]") {
			fmt.Fprintf(&sb, "[System Message] Team member %q failed to complete task.\n\nError: %s", label, strings.TrimPrefix(e.Content, "[FAILED] "))
			sb.WriteString("\n\nInform the user about the failure. You may suggest retrying with team_tasks(action=\"retry\", task_id=\"...\") if appropriate.")
		} else {
			fmt.Fprintf(&sb, "[System Message] Team member %q completed task.\n\nResult:\n%s", label, e.Content)
		}
	} else {
		// Count successes vs failures for header.
		var failed, succeeded int
		for _, e := range entries {
			if strings.HasPrefix(e.Content, "[FAILED]") {
				failed++
			} else {
				succeeded++
			}
		}
		if failed > 0 && succeeded > 0 {
			fmt.Fprintf(&sb, "[System Message] %d task(s) completed, %d task(s) failed.\n", succeeded, failed)
		} else if failed > 0 {
			fmt.Fprintf(&sb, "[System Message] %d task(s) failed.\n", failed)
		} else {
			fmt.Fprintf(&sb, "[System Message] %d team tasks completed.\n", succeeded)
		}
		for _, e := range entries {
			label := memberLabel(e)
			if strings.HasPrefix(e.Content, "[FAILED]") {
				fmt.Fprintf(&sb, "\n--- FAILED: %q ---\nError: %s\n", label, strings.TrimPrefix(e.Content, "[FAILED] "))
			} else {
				fmt.Fprintf(&sb, "\n--- Result from %q ---\n%s\n", label, e.Content)
			}
		}
		if failed > 0 {
			sb.WriteString("\nFor failed tasks, you may suggest retrying with team_tasks(action=\"retry\", task_id=\"...\").")
		}
	}

	if taskBoardSnapshot != "" {
		sb.WriteString("\n\n")
		sb.WriteString(taskBoardSnapshot)
	}

	// Batch-aware prompting: guide leader to summarize vs acknowledge.
	allDone := strings.Contains(taskBoardSnapshot, "All ") && strings.Contains(taskBoardSnapshot, " completed")
	if allDone {
		sb.WriteString("\n\nAll tasks in this batch are completed. Present a comprehensive summary of ALL results to the user.")
	} else if taskBoardSnapshot != "" {
		sb.WriteString("\n\nSome tasks are still in progress. Briefly acknowledge this result (1-2 sentences). A full summary will come when all tasks complete.")
	} else {
		sb.WriteString("\n\nPresent this result to the user.")
	}

	sb.WriteString(" Any media files are forwarded automatically. Do NOT search for files — the results above contain all relevant information.")

	if teamWorkspace != "" {
		fmt.Fprintf(&sb, "\n[Team workspace: %s — use read_file/list_files to access shared files]", teamWorkspace)
	}

	return sb.String()
}
