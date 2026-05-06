package voice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cartridge-gg/discordgo"
)

// On a successful wire-up, the session output posts an initial summary
// message AND creates a thread anchored to it. Subsequent transcript posts
// should go to the thread, not the parent channel.
func Test_newSessionOutput_happy_path_posts_summary_and_creates_thread(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	if fs.channelCalls != 1 {
		t.Errorf("expected one Channel() lookup for channel name; got %d", fs.channelCalls)
	}
	if fs.channelSendCalls != 1 {
		t.Errorf("expected one ChannelMessageSend for the initial summary; got %d", fs.channelSendCalls)
	}
	if fs.threadStartCalls != 1 {
		t.Errorf("expected one MessageThreadStart; got %d", fs.threadStartCalls)
	}
	if out.summaryMsgID == "" {
		t.Error("summaryMsgID should be populated on successful post")
	}
	if out.threadChannelID == "" {
		t.Error("threadChannelID should be populated on successful thread create")
	}
	if !strings.Contains(fs.lastSentContent, "Voice session started") {
		t.Errorf("summary text should indicate session start: %q", fs.lastSentContent)
	}
	if !strings.Contains(fs.lastSentContent, "#test-channel") {
		t.Errorf("summary should include resolved channel name: %q", fs.lastSentContent)
	}
	if !strings.Contains(fs.lastThreadName, "test-channel") {
		t.Errorf("thread name should include channel name: %q", fs.lastThreadName)
	}
}

// Channel lookup failure → output still usable; summary falls back to the
// bare channel ID for naming but the session can proceed.
func Test_newSessionOutput_channel_lookup_failure_falls_back_to_id(t *testing.T) {
	fs := &fakeSession{
		channelFn: func(_ string) (*discordgo.Channel, error) { return nil, errors.New("no perms") },
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch-xyz", discardLogger(), nil)
	if !strings.Contains(fs.lastSentContent, "voice-ch-xyz") {
		t.Errorf("fallback should use raw voice channel ID in summary: %q", fs.lastSentContent)
	}
	// Summary post + thread create still happen.
	if out.summaryMsgID == "" || out.threadChannelID == "" {
		t.Errorf("summary/thread should still be created despite channel-name lookup failure")
	}
}

// Initial summary-post failure → output is in degraded mode. PostLine still
// works by falling through to the parent transcript channel, since there's
// no thread to anchor to.
func Test_newSessionOutput_summary_post_failure_keeps_output_usable(t *testing.T) {
	fs := &fakeSession{
		channelSendFn: func(ch, _ string) (*discordgo.Message, error) {
			if ch == "transcript-ch" {
				return nil, errors.New("forbidden")
			}
			return &discordgo.Message{ID: "msg-x"}, nil
		},
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	if out.summaryMsgID != "" {
		t.Error("summaryMsgID should be empty after failed post")
	}
	if out.threadChannelID != "" {
		t.Error("thread should not have been created after summary post failed")
	}
	// PostLine should still be callable and fall back to parent channel.
	out.PostLine(context.Background(), "alice", "hi there")
	lines := fs.sendsByChannel["transcript-ch"]
	if len(lines) < 1 {
		t.Fatalf("expected at least one parent-channel post after summary failure")
	}
}

// Thread-create failure leaves the output in "parent-channel only" mode.
func Test_newSessionOutput_thread_failure_falls_back_to_parent(t *testing.T) {
	fs := &fakeSession{
		messageThreadStart: func(_, _, _ string, _ int) (*discordgo.Channel, error) {
			return nil, errors.New("rate limit")
		},
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	if out.summaryMsgID == "" {
		t.Error("summary should have been posted before thread create failed")
	}
	if out.threadChannelID != "" {
		t.Error("threadChannelID should stay empty after thread create failed")
	}
	out.PostLine(context.Background(), "alice", "hi")
	// Should post in the PARENT transcript channel (fallback), not a thread.
	if posts := fs.sendsByChannel["transcript-ch"]; len(posts) < 2 {
		t.Fatalf("expected >=2 parent-channel sends (summary + fallback line); got %d", len(posts))
	}
}

func Test_newSessionOutput_recovers_active_summary_and_thread(t *testing.T) {
	now := time.Now().Add(-5 * time.Minute)
	fs := &fakeSession{
		channelMessagesFn: func(channelID string, limit int, _, _, _ string) ([]*discordgo.Message, error) {
			switch channelID {
			case "transcript-ch":
				return []*discordgo.Message{
					{
						ID:        "summary-old",
						Content:   "🎤 Voice session in #test-channel — 1 speaker: Alice",
						Timestamp: now,
						Thread:    &discordgo.Channel{ID: "thread-old"},
					},
				}, nil
			case "thread-old":
				return []*discordgo.Message{
					{Content: "Bob: newest line"},
					{Content: "Alice: older line"},
				}, nil
			default:
				return nil, nil
			}
		},
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	if fs.channelSendCalls != 0 {
		t.Fatalf("recovery should not create a new summary message, sent %d", fs.channelSendCalls)
	}
	if fs.threadStartCalls != 0 {
		t.Fatalf("recovery should not create a new thread, created %d", fs.threadStartCalls)
	}
	if out.summaryMsgID != "summary-old" || out.threadChannelID != "thread-old" {
		t.Fatalf("did not recover existing summary/thread: summary=%q thread=%q", out.summaryMsgID, out.threadChannelID)
	}
	if out.utteranceCount != 2 {
		t.Fatalf("expected recovered utterance count 2, got %d", out.utteranceCount)
	}
	out.PostLine(context.Background(), "Alice", "new line after restart")
	if got := fs.sendsByChannel["thread-old"]; len(got) != 1 {
		t.Fatalf("expected new post to recovered thread, got %d", len(got))
	}
	out.Close(context.Background(), time.Minute)
	if !strings.Contains(fs.lastEditContent, "3 utterances") {
		t.Fatalf("final summary should include recovered + new utterance count: %q", fs.lastEditContent)
	}
}

func Test_newSessionOutput_ignores_ended_summary_when_recovering(t *testing.T) {
	fs := &fakeSession{
		channelMessagesFn: func(channelID string, _ int, _, _, _ string) ([]*discordgo.Message, error) {
			if channelID != "transcript-ch" {
				return nil, nil
			}
			return []*discordgo.Message{{
				ID:      "summary-ended",
				Content: "Discussed shipping.\n\n✅ Voice session ended in #test-channel — 5m · 2 speakers · 8 utterances",
				Thread:  &discordgo.Channel{ID: "thread-ended"},
			}}, nil
		},
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	if out.summaryMsgID == "summary-ended" {
		t.Fatal("must not recover an already-ended voice summary")
	}
	if fs.channelSendCalls != 1 || fs.threadStartCalls != 1 {
		t.Fatalf("expected fresh summary/thread after ignoring ended summary, sends=%d threads=%d", fs.channelSendCalls, fs.threadStartCalls)
	}
}

// NoteSpeaker updates the running summary; repeated calls for the same
// speaker don't re-edit (no change to the list).
func Test_NoteSpeaker_updates_summary_and_dedupes(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	// Force the edit-throttle timer to the distant past so our edits fire.
	out.lastEditAt = time.Time{}

	out.NoteSpeaker(context.Background(), "u1", "Alice")
	if fs.channelEditCalls != 1 {
		t.Fatalf("first speaker should trigger one summary edit; got %d", fs.channelEditCalls)
	}
	if !strings.Contains(fs.lastEditContent, "Alice") {
		t.Errorf("edit should include Alice: %q", fs.lastEditContent)
	}
	// Same speaker again — no new edit.
	before := fs.channelEditCalls
	out.NoteSpeaker(context.Background(), "u1", "Alice")
	if fs.channelEditCalls != before {
		t.Errorf("repeat same-speaker call should not edit again; got %d -> %d", before, fs.channelEditCalls)
	}
}

// Throttle suppresses mid-burst edits but Close on a non-empty session
// always flushes the final state regardless of recency.
func Test_NoteSpeaker_throttled_but_Close_always_edits(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	// First speaker: open the throttle window.
	out.NoteSpeaker(context.Background(), "u1", "Alice")
	// Second speaker immediately — within the throttle window.
	out.NoteSpeaker(context.Background(), "u2", "Bob")
	// Between the two NoteSpeaker calls we expect at MOST one edit (the
	// first), because the second was suppressed by the throttle.
	if fs.channelEditCalls > 1 {
		t.Errorf("expected throttle to suppress second edit; got %d edits", fs.channelEditCalls)
	}
	// Post a transcript line so Close takes the non-empty path. With
	// utteranceCount==0 Close deletes the summary instead of editing
	// (verified separately in Test_Close_empty_session_deletes_summary).
	out.PostLine(context.Background(), "Alice", "hi")
	editsBefore := fs.channelEditCalls
	out.Close(context.Background(), 42*time.Second)
	if fs.channelEditCalls <= editsBefore {
		t.Fatalf("Close on non-empty session should fire a final edit; calls %d -> %d", editsBefore, fs.channelEditCalls)
	}
	if !strings.Contains(fs.lastEditContent, "ended") {
		t.Errorf("Close should edit with 'ended' marker: %q", fs.lastEditContent)
	}
	if !strings.Contains(fs.lastEditContent, "2 speakers") {
		t.Errorf("Close should report 2 speakers: %q", fs.lastEditContent)
	}
}

// Close is idempotent and safe on a nil or uninitialized output.
// Empty session (no PostLine calls) deletes the summary + thread on
// the first Close; second Close is a no-op.
func Test_Close_idempotent_and_nil_safe(t *testing.T) {
	var nilOut *sessionOutput
	nilOut.Close(context.Background(), 0) // nil receiver is allowed

	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	beforeDel := fs.channelMessageDeleteCalls
	beforeChDel := fs.channelDeleteCalls
	out.Close(context.Background(), time.Second)
	afterDel1 := fs.channelMessageDeleteCalls
	afterChDel1 := fs.channelDeleteCalls
	out.Close(context.Background(), time.Second)
	afterDel2 := fs.channelMessageDeleteCalls
	afterChDel2 := fs.channelDeleteCalls
	if afterDel1 <= beforeDel {
		t.Errorf("first Close on empty session should delete the summary message; got %d -> %d", beforeDel, afterDel1)
	}
	if afterChDel1 <= beforeChDel {
		t.Errorf("first Close on empty session should delete the thread; got %d -> %d", beforeChDel, afterChDel1)
	}
	if afterDel2 != afterDel1 || afterChDel2 != afterChDel1 {
		t.Errorf("second Close should be a no-op; deletes %d -> %d, channel deletes %d -> %d",
			afterDel1, afterDel2, afterChDel1, afterChDel2)
	}
}

// Empty session (zero utterances): Close deletes both the parent
// summary message and the attached thread to keep the transcript
// channel quiet for sessions where no human spoke.
func Test_Close_empty_session_deletes_summary_and_thread(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	editsBefore := fs.channelEditCalls
	out.Close(context.Background(), time.Minute)
	if fs.channelMessageDeleteCalls != 1 {
		t.Errorf("empty session should delete exactly one summary message; got %d", fs.channelMessageDeleteCalls)
	}
	if fs.channelDeleteCalls != 1 {
		t.Errorf("empty session should delete exactly one thread; got %d", fs.channelDeleteCalls)
	}
	if fs.channelEditCalls != editsBefore {
		t.Errorf("empty session should NOT edit the summary (just delete); edits %d -> %d", editsBefore, fs.channelEditCalls)
	}
	if fs.lastDeletedChannelID != "transcript-ch" {
		t.Errorf("summary delete should target the transcript channel; got %q", fs.lastDeletedChannelID)
	}
}

// When a TranscriptSummarizer is configured and the session has at
// least one transcribed utterance, Close runs the summarizer and uses
// its output as the final summary message body (with the stats line
// appended).
func Test_Close_runs_summarizer_when_set(t *testing.T) {
	fs := &fakeSession{}
	called := false
	var seenTranscript string
	summarizer := func(_ context.Context, transcript string) (string, error) {
		called = true
		seenTranscript = transcript
		return "Discussed the new feature rollout.", nil
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), summarizer)
	out.PostLine(context.Background(), "Alice", "we're rolling out the new feature next week")
	out.PostLine(context.Background(), "Bob", "anything I should help with?")
	out.Close(context.Background(), 5*time.Minute)
	if !called {
		t.Fatal("summarizer should have been invoked")
	}
	if !strings.Contains(seenTranscript, "Alice: we're rolling out") {
		t.Errorf("summarizer received unexpected transcript: %q", seenTranscript)
	}
	if !strings.Contains(seenTranscript, "Bob: anything I should help with") {
		t.Errorf("summarizer should see all lines: %q", seenTranscript)
	}
	if !strings.Contains(fs.lastEditContent, "Discussed the new feature rollout.") {
		t.Errorf("summary edit should contain summarizer output: %q", fs.lastEditContent)
	}
	if !strings.Contains(fs.lastEditContent, "ended") {
		t.Errorf("summary edit should still include stats line: %q", fs.lastEditContent)
	}
}

// Summarizer returning an error → fall back to the legacy stats line.
func Test_Close_summarizer_error_falls_back_to_stats(t *testing.T) {
	fs := &fakeSession{}
	summarizer := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("provider down")
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), summarizer)
	out.PostLine(context.Background(), "Alice", "hi")
	out.Close(context.Background(), time.Minute)
	if strings.Contains(fs.lastEditContent, "provider") {
		t.Errorf("error string should not leak into summary: %q", fs.lastEditContent)
	}
	if !strings.Contains(fs.lastEditContent, "1 utterance") {
		t.Errorf("fallback should include stats line: %q", fs.lastEditContent)
	}
}

func Test_Close_truncates_overlong_summarizer_output(t *testing.T) {
	fs := &fakeSession{}
	summarizer := func(_ context.Context, _ string) (string, error) {
		return strings.Repeat("x", summaryMessageMaxLen+500), nil
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), summarizer)
	out.PostLine(context.Background(), "Alice", "hi")
	out.Close(context.Background(), time.Minute)
	if len(fs.lastEditContent) > summaryMessageMaxLen {
		t.Fatalf("summary edit content length = %d, want <= %d", len(fs.lastEditContent), summaryMessageMaxLen)
	}
	if !strings.Contains(fs.lastEditContent, "✅ Voice session ended") {
		t.Fatalf("truncated summary should preserve stats line: %q", fs.lastEditContent)
	}
}

// With no summaryMsgID (initial post failed), Close is a clean no-op.
func Test_Close_noop_when_summary_post_failed(t *testing.T) {
	fs := &fakeSession{
		channelSendFn: func(_, _ string) (*discordgo.Message, error) {
			return nil, errors.New("forbidden")
		},
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	before := fs.channelEditCalls
	out.Close(context.Background(), time.Minute)
	if fs.channelEditCalls != before {
		t.Errorf("Close should not edit when summary post never succeeded; got %d -> %d", before, fs.channelEditCalls)
	}
}

// PostLine records utterances in utteranceCount, exposed through the final
// summary's stats line.
func Test_Close_reports_utterance_count(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	for i := 0; i < 3; i++ {
		out.PostLine(context.Background(), "alice", "hi")
	}
	out.Close(context.Background(), 90*time.Second)
	if !strings.Contains(fs.lastEditContent, "3 utterances") {
		t.Errorf("final summary should report 3 utterances: %q", fs.lastEditContent)
	}
	if !strings.Contains(fs.lastEditContent, "1m") {
		t.Errorf("final summary should include duration: %q", fs.lastEditContent)
	}
}

// The thread-message cap spills overflow to the parent transcript channel
// so late-session transcripts still reach operators.
func Test_PostLine_falls_back_to_parent_on_thread_cap(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger(), nil)
	out.mu.Lock()
	out.utteranceCount = threadMessageCap // simulate cap already hit
	out.mu.Unlock()

	out.PostLine(context.Background(), "alice", "hi after cap")
	// At least one post must have landed in the parent transcript channel
	// beyond the initial summary.
	parentPosts := fs.sendsByChannel["transcript-ch"]
	foundFallback := false
	for _, p := range parentPosts {
		if strings.Contains(p, "hi after cap") {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Errorf("post-cap line should have spilled to parent channel; parent posts: %v", parentPosts)
	}
}

// Regression guard: nil receivers on the hot-path calls don't panic.
func Test_sessionOutput_nil_methods_are_safe(t *testing.T) {
	var out *sessionOutput
	out.PostLine(context.Background(), "a", "b")
	out.NoteSpeaker(context.Background(), "u", "n")
	out.Close(context.Background(), 0)
	// If any of the above panicked, the test would have already failed.
}
