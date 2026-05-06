package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/cartridge-gg/discordgo"

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

	// On process restart we do not have in-memory sessionOutput state. Before
	// creating a fresh summary/thread, look back through recent transcript
	// channel messages for an un-ended voice summary for the same voice channel.
	sessionRecoveryMessageScanLimit = 25
	sessionRecoveryThreadLineLimit  = 100
	summaryMessageMaxLen            = 1900

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
	summarizer          TranscriptSummarizer // optional; nil → keep legacy stats line on Close

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
	recovered       bool // true when this output reattached to a pre-restart summary/thread

	// transcriptLines accumulates "<DisplayName>: <text>" entries in
	// post order so Close can hand the full session to the summarizer.
	// Capped at transcriptCaptureMax so a runaway speaker can't OOM the
	// supervisor; older entries get dropped silently (the summary is
	// best-effort and a 30-min, 3000-utterance session is well over what
	// any LLM context window can hold anyway).
	transcriptLines []string
}

// transcriptCaptureMax bounds the in-memory transcript we retain for
// the close-time summarization. ~3000 lines × ~200 bytes ≈ 600KB; any
// LLM-friendly summary input is well under that.
const transcriptCaptureMax = 3000

// newSessionOutput posts the initial summary message and starts the thread.
// REST failures are logged + recorded in the returned object's state so the
// caller can proceed even on a degraded setup. Never returns an error;
// sessionOutput is always usable (falls back to parent-channel posts).
//
// summarizer is optional; if nil, Close keeps the legacy stats-line summary.
//
// Caller's ctx governs the two REST calls; budget ~5s total.
func newSessionOutput(ctx context.Context, session discordSession, transcriptChID, voiceChID string, log *slog.Logger, summarizer TranscriptSummarizer) *sessionOutput {
	out := &sessionOutput{
		session:             session,
		transcriptChannelID: transcriptChID,
		voiceChannelID:      voiceChID,
		log:                 log,
		summarizer:          summarizer,
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

	if out.recoverActive(ctx) {
		return out
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

// recoverActive reattaches to the most recent un-ended voice summary for this
// channel. This covers pod restarts while humans are still in voice: the new
// process rejoins but continues the existing transcript thread instead of
// creating a duplicate summary message.
func (o *sessionOutput) recoverActive(ctx context.Context) bool {
	msgs, err := o.session.ChannelMessages(o.transcriptChannelID, sessionRecoveryMessageScanLimit, "", "", "", discordgo.WithContext(ctx))
	if err != nil {
		o.log.Debug("voice: active session recovery scan failed", "err", err, "transcript_channel_id", o.transcriptChannelID)
		return false
	}
	for _, msg := range msgs {
		if msg == nil || !o.isRecoverableSummary(msg.Content) {
			continue
		}
		threadID := msg.ID
		if msg.Thread != nil && msg.Thread.ID != "" {
			threadID = msg.Thread.ID
		}
		o.summaryMsgID = msg.ID
		o.threadChannelID = threadID
		o.startedAt = msg.Timestamp
		if o.startedAt.IsZero() {
			o.startedAt = time.Now()
		}
		o.recovered = true
		o.loadRecoveredTranscript(ctx)
		o.log.Info("voice: recovered active session output",
			"summary_msg_id", o.summaryMsgID,
			"thread_channel_id", o.threadChannelID,
			"recovered_lines", len(o.transcriptLines),
		)
		return true
	}
	return false
}

func (o *sessionOutput) isRecoverableSummary(content string) bool {
	label := o.channelLabel()
	if strings.Contains(content, fmt.Sprintf("✅ Voice session ended in %s", label)) {
		return false
	}
	return strings.Contains(content, fmt.Sprintf("🎤 Voice session started in %s", label)) ||
		strings.Contains(content, fmt.Sprintf("🎤 Voice session in %s", label))
}

func (o *sessionOutput) loadRecoveredTranscript(ctx context.Context) {
	if o.threadChannelID == "" {
		return
	}
	msgs, err := o.session.ChannelMessages(o.threadChannelID, sessionRecoveryThreadLineLimit, "", "", "", discordgo.WithContext(ctx))
	if err != nil {
		o.log.Debug("voice: recovered thread transcript scan failed", "err", err, "thread_channel_id", o.threadChannelID)
		return
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg == nil {
			continue
		}
		line := strings.TrimSpace(msg.Content)
		if !looksLikeTranscriptLine(line) {
			continue
		}
		if len(o.transcriptLines) >= transcriptCaptureMax {
			break
		}
		o.transcriptLines = append(o.transcriptLines, line)
		o.utteranceCount++
		if name := speakerNameFromLine(line); name != "" {
			o.noteSpeakerLocked("recovered:"+name, name)
		}
	}
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
	if len(o.transcriptLines) < transcriptCaptureMax {
		o.transcriptLines = append(o.transcriptLines, line)
	}
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
		if idx, ok := o.recoveredSpeakerIndexLocked(displayName); ok {
			oldID := o.speakerOrder[idx]
			delete(o.speakers, oldID)
			o.speakerOrder[idx] = userID
		} else {
			o.speakerOrder = append(o.speakerOrder, userID)
		}
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

// Close finalizes the session. Subsequent calls are no-ops.
//
// Two finishing modes:
//
//  1. Empty session (utteranceCount == 0): the bot joined but no human
//     speech reached STT. The summary message + thread are pure noise
//     for the channel — delete both. Best-effort: a delete failure is
//     logged at warn but doesn't propagate.
//
//  2. Non-empty session (utteranceCount > 0): if a TranscriptSummarizer
//     is wired, run it over the captured "<DisplayName>: <text>" lines
//     and use the result as the summary message body (with the stats
//     line appended). If no summarizer or the call fails, fall back to
//     the legacy stats-only line so the summary message still reflects
//     the final state.
//
// In both modes the operation is wrapped in the caller's ctx — Close
// runs from the supervisor's teardown goroutine which gives us a 30s
// budget (long enough for an LLM call but bounded so a wedged provider
// doesn't pin the supervisor).
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
	threadID := o.threadChannelID
	speakerCount := len(o.speakers)
	utterances := o.utteranceCount
	transcriptCopy := append([]string(nil), o.transcriptLines...)
	o.mu.Unlock()

	if msgID == "" {
		// Initial post failed; nothing to clean up. (No thread either —
		// MessageThreadStart only runs after a successful summary post.)
		return
	}

	// Empty brand-new session → delete the summary + thread artefacts. A
	// recovered session may legitimately have no hydrated lines (thread lookup
	// can fail), so preserve it rather than deleting pre-restart artefacts.
	if utterances == 0 && !o.recovered {
		o.cleanupEmpty(ctx, msgID, threadID)
		return
	}

	// Non-empty session → write a real summary if we have a summarizer,
	// otherwise keep the legacy stats line.
	finalText := o.finalSummaryTextLocked(duration, speakerCount, utterances)
	if o.summarizer != nil && len(transcriptCopy) > 0 {
		summary, err := o.summarizer(ctx, strings.Join(transcriptCopy, "\n"))
		switch {
		case err != nil:
			o.log.Warn("voice: transcript summarizer failed; falling back to stats line",
				"err", err, "lines", len(transcriptCopy))
		case strings.TrimSpace(summary) == "":
			// Info, not Debug: an empty summary is invisible-by-default
			// in prod (the user just sees the stats line and assumes the
			// LLM didn't run). Surfacing it at Info makes the failure
			// mode obvious in logs without needing a debugger or local
			// repro. Common cause: reasoning models consuming the entire
			// max_completion_tokens budget on reasoning tokens.
			o.log.Info("voice: transcript summarizer returned empty; falling back to stats line",
				"lines", len(transcriptCopy))
		default:
			finalText = combineSummaryAndStats(strings.TrimSpace(summary), finalText)
		}
	}

	if _, err := o.session.ChannelMessageEdit(o.transcriptChannelID, msgID, finalText, discordgo.WithContext(ctx)); err != nil {
		o.log.Warn("voice: final summary edit failed", "err", err)
	}
}

// cleanupEmpty deletes the parent summary message and the attached
// thread (if any). Best-effort: each failure is logged but doesn't
// propagate. Order matters: delete the thread first so a successful
// summary delete doesn't orphan a dangling thread reference (Discord
// auto-archives orphan threads but the channel still shows them
// briefly).
func (o *sessionOutput) cleanupEmpty(ctx context.Context, summaryMsgID, threadChannelID string) {
	if threadChannelID != "" {
		if _, err := o.session.ChannelDelete(threadChannelID, discordgo.WithContext(ctx)); err != nil {
			o.log.Warn("voice: empty-session thread delete failed",
				"err", err, "thread_channel_id", threadChannelID)
		}
	}
	if err := o.session.ChannelMessageDelete(o.transcriptChannelID, summaryMsgID, discordgo.WithContext(ctx)); err != nil {
		o.log.Warn("voice: empty-session summary message delete failed",
			"err", err, "transcript_channel_id", o.transcriptChannelID, "summary_msg_id", summaryMsgID)
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

func (o *sessionOutput) recoveredSpeakerIndexLocked(displayName string) (int, bool) {
	for i, uid := range o.speakerOrder {
		if !strings.HasPrefix(uid, "recovered:") {
			continue
		}
		if o.speakers[uid] == displayName {
			return i, true
		}
	}
	return 0, false
}

func (o *sessionOutput) noteSpeakerLocked(userID, displayName string) {
	if userID == "" || displayName == "" {
		return
	}
	if _, ok := o.speakers[userID]; !ok {
		o.speakerOrder = append(o.speakerOrder, userID)
	}
	o.speakers[userID] = displayName
}

func looksLikeTranscriptLine(line string) bool {
	idx := strings.Index(line, ":")
	if idx <= 0 || idx > 80 {
		return false
	}
	return strings.TrimSpace(line[idx+1:]) != ""
}

func speakerNameFromLine(line string) string {
	idx := strings.Index(line, ":")
	if idx <= 0 || idx > 80 {
		return ""
	}
	return strings.TrimSpace(line[:idx])
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

func combineSummaryAndStats(summary, stats string) string {
	summary = strings.TrimSpace(summary)
	stats = strings.TrimSpace(stats)
	if summary == "" {
		return truncateContent(stats, summaryMessageMaxLen)
	}
	sep := "\n\n"
	available := summaryMessageMaxLen - len(sep) - len(stats)
	if available <= 0 {
		return truncateContent(stats, summaryMessageMaxLen)
	}
	return truncateContent(summary, available) + sep + stats
}

func truncateContent(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
