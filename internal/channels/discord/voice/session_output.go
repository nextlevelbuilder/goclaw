package voice

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Discord thread defaults we don't need to expose.
const (
	// 1440 = 24h, the only value that works for non-Boosted guilds without a
	// Community feature. Sessions rarely span a day; this is the "leave it
	// alone" setting.
	threadAutoArchiveMinutes = 1440

	// Discord enforces ~1000 messages per thread. We fall back to posting in
	// the parent transcript channel after this, so late-session utterances
	// still reach the operator, just louder.
	threadMessageCap = 1000

	// Cap on REST calls we make for summary edits. Discord's per-channel
	// message-edit rate limit is ~5/5s; discordgo's retry logic handles
	// short bursts, but we still cap outgoing calls at ~1 edit per 4s so a
	// noisy call (many speakers joining in quick succession) doesn't eat
	// the per-channel budget. A trailing edit on Close always fires regardless
	// of the last throttle window.
	summaryEditMinInterval = 4 * time.Second
)

// sessionOutput owns the per-voice-session Discord artefacts: one summary
// message in the transcript text channel and an attached thread where raw
// per-utterance transcripts go. One instance per voice-join; discarded on
// voice-leave. Safe for concurrent use — PostLine fires from the
// transcriber worker goroutine, NoteSpeaker fires from the transcriber
// worker goroutine (after resolveDisplayName), and Close fires from the
// supervisor's teardown goroutine. All writes go through mu.
//
// Graceful degradation: if the initial summary post or thread creation
// fails (permissions, rate limit, network), the sessionOutput is still
// returned in a usable but reduced-function state — PostLine falls back to
// the parent transcript channel, NoteSpeaker / Close become no-ops. The
// operator still gets transcripts; they just see old-style line-per-message
// output until the next session.
type sessionOutput struct {
	session             discordSession
	transcriptChannelID string
	voiceChannelID      string
	voiceChannelName    string // fallback "<id>" if Channel lookup failed
	log                 *slog.Logger

	mu              sync.Mutex
	summaryMsgID    string            // "" if initial post failed
	threadChannelID string            // "" if thread create failed
	speakers        map[string]string // userID -> display name (ordered via sort for summary rendering)
	speakerOrder    []string          // first-seen order, rendered in summary
	startedAt       time.Time         // session start (for final-summary duration)
	utteranceCount  int               // total PostLine calls that succeeded
	droppedOnCap    int               // posts that spilled to the parent channel after hitting threadMessageCap
	lastEditAt      time.Time         // last summary-edit request start; used for rate-throttle
	closed          bool
}

// newSessionOutput posts the initial summary message and starts the thread.
// REST failures are logged + recorded in the returned object's state so the
// caller can proceed even on a degraded setup. Never returns an error;
// sessionOutput is always usable (falls back to parent-channel posts).
//
// Caller's ctx governs the two REST calls; budget ~5s total.
func newSessionOutput(ctx context.Context, session discordSession, transcriptChID, voiceChID string, log *slog.Logger) *sessionOutput {
	out := &sessionOutput{
		session:             session,
		transcriptChannelID: transcriptChID,
		voiceChannelID:      voiceChID,
		log:                 log,
		speakers:            make(map[string]string),
		startedAt:           time.Now(),
	}

	// Best-effort channel-name lookup for readable summary + thread name.
	// Failure is fine — we use the raw ID as a fallback in both.
	if ch, err := session.Channel(voiceChID, discordgo.WithContext(ctx)); err == nil && ch != nil && ch.Name != "" {
		out.voiceChannelName = ch.Name
	} else if err != nil {
		log.Debug("voice: channel-name lookup failed; using ID", "err", err, "voice_channel_id", voiceChID)
	}

	// Post the initial summary message.
	msg, err := session.ChannelMessageSend(transcriptChID, out.initialSummaryText(), discordgo.WithContext(ctx))
	if err != nil || msg == nil {
		log.Warn("voice: initial summary post failed; falling back to parent-channel per-line transcripts",
			"err", err, "transcript_channel_id", transcriptChID)
		return out
	}
	out.summaryMsgID = msg.ID

	// Start a thread anchored to the summary message.
	thread, err := session.MessageThreadStart(transcriptChID, msg.ID, out.threadName(), threadAutoArchiveMinutes, discordgo.WithContext(ctx))
	if err != nil || thread == nil {
		log.Warn("voice: thread start failed; per-line transcripts will post in the parent channel",
			"err", err, "transcript_channel_id", transcriptChID, "summary_msg_id", msg.ID)
		return out
	}
	out.threadChannelID = thread.ID
	log.Info("voice: session output ready",
		"summary_msg_id", out.summaryMsgID,
		"thread_channel_id", out.threadChannelID,
		"voice_channel_name", out.voiceChannelName,
	)
	return out
}

// PostLine posts a transcript line to the thread (or the parent transcript
// channel if the thread is unavailable). Counts toward the per-thread cap
// and increments utteranceCount. Always honours the caller's ctx for the
// REST timeout.
func (o *sessionOutput) PostLine(ctx context.Context, displayName, text string) {
	if o == nil {
		return
	}
	line := fmt.Sprintf("%s: %s", channels.SanitizeDisplayName(displayName), strings.TrimSpace(text))
	if len(line) > 1900 {
		line = line[:1897] + "..."
	}

	o.mu.Lock()
	target := o.threadChannelID
	hitCap := o.utteranceCount >= threadMessageCap
	if target == "" || hitCap {
		if hitCap && o.droppedOnCap == 0 {
			o.log.Warn("voice: thread hit 1000-message cap; spilling to parent transcript channel",
				"thread_channel_id", o.threadChannelID, "transcript_channel_id", o.transcriptChannelID)
		}
		if hitCap {
			o.droppedOnCap++
		}
		target = o.transcriptChannelID
	}
	o.mu.Unlock()

	if target == "" {
		// Nothing usable — neither thread nor parent channel. Drop with warn.
		o.log.Warn("voice: no usable output channel for transcript line", "line", line)
		return
	}

	if _, err := o.session.ChannelMessageSend(target, line, discordgo.WithContext(ctx)); err != nil {
		o.log.Warn("voice: transcript line post failed",
			"err", err, "target_channel_id", target)
		return
	}
	o.mu.Lock()
	o.utteranceCount++
	o.mu.Unlock()
}

// NoteSpeaker records a speaker for the session and refreshes the summary
// message's speaker list. No-op if we don't have a summary message to edit.
// Rate-throttled to at most one edit per summaryEditMinInterval so rapid
// speaker arrivals don't eat Discord's per-channel edit budget; the final
// edit on Close is never throttled and always reflects the full list.
func (o *sessionOutput) NoteSpeaker(ctx context.Context, userID, displayName string) {
	if o == nil || userID == "" {
		return
	}
	o.mu.Lock()
	if o.closed || o.summaryMsgID == "" {
		o.mu.Unlock()
		return
	}
	if existing, ok := o.speakers[userID]; ok && existing == displayName {
		// Already tracked with the same name; no-op.
		o.mu.Unlock()
		return
	}
	isNew := false
	if _, ok := o.speakers[userID]; !ok {
		o.speakerOrder = append(o.speakerOrder, userID)
		isNew = true
	}
	o.speakers[userID] = displayName
	// Rate-throttle: skip the edit if we just did one, unless this is a brand
	// new speaker (the list changed materially).
	if !isNew {
		o.mu.Unlock()
		return
	}
	if time.Since(o.lastEditAt) < summaryEditMinInterval {
		o.mu.Unlock()
		return
	}
	o.lastEditAt = time.Now()
	text := o.runningSummaryTextLocked()
	msgID := o.summaryMsgID
	o.mu.Unlock()

	if _, err := o.session.ChannelMessageEdit(o.transcriptChannelID, msgID, text, discordgo.WithContext(ctx)); err != nil {
		o.log.Debug("voice: summary edit failed (non-fatal)", "err", err)
	}
}

// Close writes the final summary and marks the output closed. Subsequent
// calls are no-ops. Always attempts the final edit even if intermediate
// edits were throttled, so the last-seen state reflects the full session.
func (o *sessionOutput) Close(ctx context.Context, duration time.Duration) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	msgID := o.summaryMsgID
	speakerCount := len(o.speakers)
	utterances := o.utteranceCount
	text := o.finalSummaryTextLocked(duration, speakerCount, utterances)
	o.mu.Unlock()

	if msgID == "" {
		// Initial post failed; nothing to close.
		return
	}
	if _, err := o.session.ChannelMessageEdit(o.transcriptChannelID, msgID, text, discordgo.WithContext(ctx)); err != nil {
		o.log.Warn("voice: final summary edit failed", "err", err)
	}
}

// ----- helpers -----

func (o *sessionOutput) channelLabel() string {
	if o.voiceChannelName != "" {
		return "#" + o.voiceChannelName
	}
	return "voice channel " + o.voiceChannelID
}

func (o *sessionOutput) initialSummaryText() string {
	return fmt.Sprintf("🎤 Voice session started in %s", o.channelLabel())
}

func (o *sessionOutput) threadName() string {
	label := o.voiceChannelName
	if label == "" {
		label = o.voiceChannelID
	}
	name := fmt.Sprintf("#%s voice · %s", label, o.startedAt.UTC().Format("Jan 2 15:04"))
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// runningSummaryTextLocked renders the "session in progress" line with the
// current speaker list. Caller must hold mu.
func (o *sessionOutput) runningSummaryTextLocked() string {
	names := make([]string, 0, len(o.speakerOrder))
	for _, uid := range o.speakerOrder {
		if n, ok := o.speakers[uid]; ok && n != "" {
			names = append(names, channels.SanitizeDisplayName(n))
		}
	}
	// Keep first-seen order; only sort ties (empty-name fallbacks) for stability.
	sort.SliceStable(names, func(i, j int) bool { return false }) // no-op, retains order
	label := o.channelLabel()
	switch len(names) {
	case 0:
		return fmt.Sprintf("🎤 Voice session in %s", label)
	case 1:
		return fmt.Sprintf("🎤 Voice session in %s — 1 speaker: %s", label, names[0])
	default:
		return fmt.Sprintf("🎤 Voice session in %s — %d speakers: %s", label, len(names), strings.Join(names, ", "))
	}
}

// finalSummaryTextLocked renders the "session ended" line with stats.
// Caller must hold mu.
func (o *sessionOutput) finalSummaryTextLocked(duration time.Duration, speakerCount, utterances int) string {
	return fmt.Sprintf(
		"✅ Voice session ended in %s — %s · %s · %s",
		o.channelLabel(),
		formatDurationMin(duration),
		pluralize(speakerCount, "speaker", "speakers"),
		pluralize(utterances, "utterance", "utterances"),
	)
}

func formatDurationMin(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	return fmt.Sprintf("%dm", m)
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
