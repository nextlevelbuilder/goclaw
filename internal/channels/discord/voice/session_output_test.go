package voice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// On a successful wire-up, the session output posts an initial summary
// message AND creates a thread anchored to it. Subsequent transcript posts
// should go to the thread, not the parent channel.
func Test_newSessionOutput_happy_path_posts_summary_and_creates_thread(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch-xyz", discardLogger())
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
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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

// NoteSpeaker updates the running summary; repeated calls for the same
// speaker don't re-edit (no change to the list).
func Test_NoteSpeaker_updates_summary_and_dedupes(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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

// Throttle suppresses mid-burst edits but Close always flushes the final
// state regardless of recency.
func Test_NoteSpeaker_throttled_but_Close_always_edits(t *testing.T) {
	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
	// First speaker: open the throttle window.
	out.NoteSpeaker(context.Background(), "u1", "Alice")
	// Second speaker immediately — within the throttle window.
	out.NoteSpeaker(context.Background(), "u2", "Bob")
	// Between the two NoteSpeaker calls we expect at MOST one edit (the
	// first), because the second was suppressed by the throttle.
	if fs.channelEditCalls > 1 {
		t.Errorf("expected throttle to suppress second edit; got %d edits", fs.channelEditCalls)
	}
	// Close forces a final edit with the complete speaker list.
	out.Close(context.Background(), 42*time.Second)
	if !strings.Contains(fs.lastEditContent, "ended") {
		t.Errorf("Close should edit with 'ended' marker: %q", fs.lastEditContent)
	}
	if !strings.Contains(fs.lastEditContent, "2 speakers") {
		t.Errorf("Close should report 2 speakers: %q", fs.lastEditContent)
	}
}

// Close is idempotent and safe on a nil or uninitialized output.
func Test_Close_idempotent_and_nil_safe(t *testing.T) {
	var nilOut *sessionOutput
	nilOut.Close(context.Background(), 0) // nil receiver is allowed

	fs := &fakeSession{}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
	before := fs.channelEditCalls
	out.Close(context.Background(), time.Second)
	after1 := fs.channelEditCalls
	out.Close(context.Background(), time.Second)
	after2 := fs.channelEditCalls
	if after1 <= before {
		t.Errorf("first Close should edit the summary; got %d -> %d", before, after1)
	}
	if after2 != after1 {
		t.Errorf("second Close should be a no-op; got %d -> %d", after1, after2)
	}
}

// With no summaryMsgID (initial post failed), Close is a clean no-op.
func Test_Close_noop_when_summary_post_failed(t *testing.T) {
	fs := &fakeSession{
		channelSendFn: func(_, _ string) (*discordgo.Message, error) {
			return nil, errors.New("forbidden")
		},
	}
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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
	out := newSessionOutput(context.Background(), fs, "transcript-ch", "voice-ch", discardLogger())
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
