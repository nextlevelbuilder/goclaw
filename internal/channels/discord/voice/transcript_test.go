package voice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cartridge-gg/discordgo"
)

// fakeSession implements discordSession for tests. Hookable methods return
// zero values by default; set the Fn fields to override per-test.
type fakeSession struct {
	mu sync.Mutex

	guildMemberFn      func(guildID, userID string) (*discordgo.Member, error)
	channelSendFn      func(channelID, content string) (*discordgo.Message, error)
	channelEditFn      func(channelID, messageID, content string) (*discordgo.Message, error)
	channelFn          func(channelID string) (*discordgo.Channel, error)
	messageThreadStart func(channelID, messageID, name string, archive int) (*discordgo.Channel, error)

	guildMemberCalls  int
	channelSendCalls  int
	channelEditCalls  int
	channelCalls      int
	threadStartCalls  int
	lastSentChannelID string
	lastSentContent   string
	lastEditChannelID string
	lastEditMessageID string
	lastEditContent   string
	lastThreadName    string
	sendsByChannel    map[string][]string
}

func (f *fakeSession) recordSend(channelID, content string) {
	if f.sendsByChannel == nil {
		f.sendsByChannel = make(map[string][]string)
	}
	f.sendsByChannel[channelID] = append(f.sendsByChannel[channelID], content)
}

func (f *fakeSession) GuildMember(guildID, userID string, _ ...discordgo.RequestOption) (*discordgo.Member, error) {
	f.mu.Lock()
	f.guildMemberCalls++
	fn := f.guildMemberFn
	f.mu.Unlock()
	if fn == nil {
		return nil, errors.New("fake: GuildMember not stubbed")
	}
	return fn(guildID, userID)
}

func (f *fakeSession) ChannelMessageSend(channelID, content string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	f.channelSendCalls++
	f.lastSentChannelID = channelID
	f.lastSentContent = content
	f.recordSend(channelID, content)
	fn := f.channelSendFn
	f.mu.Unlock()
	if fn == nil {
		return &discordgo.Message{ID: "msg-1"}, nil
	}
	return fn(channelID, content)
}

func (f *fakeSession) ChannelMessageEdit(channelID, messageID, content string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.mu.Lock()
	f.channelEditCalls++
	f.lastEditChannelID = channelID
	f.lastEditMessageID = messageID
	f.lastEditContent = content
	fn := f.channelEditFn
	f.mu.Unlock()
	if fn == nil {
		return &discordgo.Message{ID: messageID}, nil
	}
	return fn(channelID, messageID, content)
}

func (f *fakeSession) Channel(channelID string, _ ...discordgo.RequestOption) (*discordgo.Channel, error) {
	f.mu.Lock()
	f.channelCalls++
	fn := f.channelFn
	f.mu.Unlock()
	if fn == nil {
		return &discordgo.Channel{ID: channelID, Name: "test-channel"}, nil
	}
	return fn(channelID)
}

func (f *fakeSession) MessageThreadStart(channelID, messageID, name string, archive int, _ ...discordgo.RequestOption) (*discordgo.Channel, error) {
	f.mu.Lock()
	f.threadStartCalls++
	f.lastThreadName = name
	fn := f.messageThreadStart
	f.mu.Unlock()
	if fn == nil {
		return &discordgo.Channel{ID: "thread-1"}, nil
	}
	return fn(channelID, messageID, name, archive)
}

// --- display name cache ----------------------------------------------------

func Test_displayNameCache_miss_then_hit(t *testing.T) {
	c := newDisplayNameCache()
	if _, ok := c.get("u1"); ok {
		t.Fatal("empty cache returned hit")
	}
	c.set("u1", "Alice")
	if got, ok := c.get("u1"); !ok || got != "Alice" {
		t.Fatalf("cache get after set: got (%q, %v)", got, ok)
	}
}

func Test_displayNameCache_expiry(t *testing.T) {
	c := newDisplayNameCache()
	now := time.Now()
	c.now = func() time.Time { return now }
	c.set("u1", "Alice")
	// Advance past the TTL.
	c.now = func() time.Time { return now.Add(displayNameTTL + time.Minute) }
	if _, ok := c.get("u1"); ok {
		t.Fatal("cache returned expired entry")
	}
}

// sweepExpired prevents unbounded growth from speaker churn. Without the
// sweep, a silent speaker's cache entry would stick around past its TTL
// until someone happened to call get() for that userID.
func Test_displayNameCache_sweepExpired_drops_stale_entries(t *testing.T) {
	c := newDisplayNameCache()
	now := time.Now()
	c.now = func() time.Time { return now }
	c.set("alice", "Alice")
	c.set("bob", "Bob")
	// Age alice out; bob stays fresh.
	c.now = func() time.Time { return now.Add(displayNameTTL + time.Minute) }
	c.set("charlie", "Charlie")

	c.sweepExpired()

	c.mu.Lock()
	_, aliceStillCached := c.m["alice"]
	_, bobStillCached := c.m["bob"]
	_, charlieStillCached := c.m["charlie"]
	c.mu.Unlock()
	if aliceStillCached {
		t.Error("expired entry 'alice' not swept")
	}
	if bobStillCached {
		t.Error("expired entry 'bob' not swept")
	}
	if !charlieStillCached {
		t.Error("fresh entry 'charlie' incorrectly swept")
	}
}

// --- daily cap counter -----------------------------------------------------

func Test_dailyCapCounter_consumes_under_cap(t *testing.T) {
	c := newDailyCapCounter(10) // 10s = 10_000ms
	if !c.tryConsume(5_000) {
		t.Fatal("expected first consume under cap to succeed")
	}
	if !c.tryConsume(4_000) {
		t.Fatal("expected second consume under cap to succeed")
	}
}

func Test_dailyCapCounter_rejects_over_cap(t *testing.T) {
	c := newDailyCapCounter(10)
	if !c.tryConsume(9_000) {
		t.Fatal("expected consume under cap to succeed")
	}
	if c.tryConsume(2_000) {
		t.Fatal("expected consume over cap to be rejected")
	}
	if !c.tryConsume(1_000) {
		t.Fatal("expected consume using remaining 1s of budget to succeed")
	}
}

func Test_dailyCapCounter_zero_cap_is_unlimited(t *testing.T) {
	c := newDailyCapCounter(0)
	for i := 0; i < 1000; i++ {
		if !c.tryConsume(60_000) {
			t.Fatal("zero-cap counter rejected an utterance; expected unlimited")
		}
	}
}

func Test_dailyCapCounter_rolls_over_at_UTC_day_boundary(t *testing.T) {
	c := newDailyCapCounter(10)
	day1 := time.Date(2026, 4, 23, 23, 59, 0, 0, time.UTC)
	c.nowFn = func() time.Time { return day1 }
	if !c.tryConsume(10_000) {
		t.Fatal("expected day1 consume to succeed")
	}
	if c.tryConsume(1) {
		t.Fatal("expected day1 over-cap reject")
	}
	day2 := time.Date(2026, 4, 24, 0, 0, 1, 0, time.UTC)
	c.nowFn = func() time.Time { return day2 }
	if !c.tryConsume(5_000) {
		t.Fatal("expected day2 to have fresh budget")
	}
}

// --- display name resolution -----------------------------------------------

func Test_resolveDisplayName_empty_userID_uses_ssrc_fallback(t *testing.T) {
	tr := &transcriber{
		cfg:       Config{},
		session:   &fakeSession{},
		log:       discardLogger(),
		nameCache: newDisplayNameCache(),
		guildID:   "g",
	}
	got := tr.resolveDisplayName(context.Background(), 1234, "")
	if got != "user:1234" {
		t.Fatalf("expected user:1234 fallback, got %q", got)
	}
}

func Test_resolveDisplayName_cache_hit_avoids_api_call(t *testing.T) {
	fs := &fakeSession{
		guildMemberFn: func(_, _ string) (*discordgo.Member, error) {
			return &discordgo.Member{Nick: "Alice"}, nil
		},
	}
	tr := &transcriber{
		cfg:       Config{},
		session:   fs,
		log:       discardLogger(),
		nameCache: newDisplayNameCache(),
		guildID:   "g",
	}
	tr.nameCache.set("u1", "CachedAlice")
	got := tr.resolveDisplayName(context.Background(), 0, "u1")
	if got != "CachedAlice" {
		t.Fatalf("expected cache hit, got %q", got)
	}
	if fs.guildMemberCalls != 0 {
		t.Fatalf("expected 0 GuildMember calls on cache hit, got %d", fs.guildMemberCalls)
	}
}

func Test_resolveDisplayName_api_error_falls_back_to_userID(t *testing.T) {
	fs := &fakeSession{
		guildMemberFn: func(_, _ string) (*discordgo.Member, error) {
			return nil, errors.New("permissions")
		},
	}
	tr := &transcriber{
		cfg:       Config{},
		session:   fs,
		log:       discardLogger(),
		nameCache: newDisplayNameCache(),
		guildID:   "g",
	}
	got := tr.resolveDisplayName(context.Background(), 7, "u7")
	if got != "u7" {
		t.Fatalf("expected userID fallback, got %q", got)
	}
	// Subsequent call must not re-fetch (we cached the fallback).
	_ = tr.resolveDisplayName(context.Background(), 7, "u7")
	if fs.guildMemberCalls != 1 {
		t.Fatalf("expected 1 GuildMember call total (api failures should also cache), got %d", fs.guildMemberCalls)
	}
}

func Test_memberDisplayName_precedence(t *testing.T) {
	cases := []struct {
		name string
		m    *discordgo.Member
		want string
	}{
		{"nil member", nil, ""},
		{"nick wins", &discordgo.Member{Nick: "Nicky"}, "Nicky"},
		{"global over username", &discordgo.Member{User: &discordgo.User{GlobalName: "G", Username: "u"}}, "G"},
		{"username when no global", &discordgo.Member{User: &discordgo.User{Username: "u"}}, "u"},
		{"id as last resort", &discordgo.Member{User: &discordgo.User{ID: "id"}}, "id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := memberDisplayName(tc.m)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- STT error taxonomy ----------------------------------------------------

func newTestTranscriber(fs *fakeSession) *transcriber {
	return &transcriber{
		cfg:        ApplyDefaults(Config{VoiceChannelID: "c", TranscriptChannelID: "t"}),
		session:    fs,
		log:        discardLogger(),
		nameCache:  newDisplayNameCache(),
		capCounter: newDailyCapCounter(7200),
		guildID:    "g",
	}
}

func Test_handleSTTError_auth_disables_session(t *testing.T) {
	tr := newTestTranscriber(&fakeSession{})
	tr.handleSTTError(errors.New("elevenlabs stt: API error 401: unauthorized"), utterance{ssrc: 1})
	if !tr.sttDisabled.Load() {
		t.Fatal("401 did not set sttDisabled")
	}
	tr = newTestTranscriber(&fakeSession{})
	tr.handleSTTError(errors.New("elevenlabs stt: API error 403: forbidden"), utterance{ssrc: 1})
	if !tr.sttDisabled.Load() {
		t.Fatal("403 did not set sttDisabled")
	}
}

func Test_handleSTTError_quota_opens_circuit(t *testing.T) {
	tr := newTestTranscriber(&fakeSession{})
	before := time.Now().UnixNano()
	tr.handleSTTError(errors.New("elevenlabs stt: API error 429: rate limited"), utterance{ssrc: 1})
	got := tr.circuitOpen.Load()
	if got <= before {
		t.Fatalf("429 did not open circuit: deadline=%d before=%d", got, before)
	}
}

func Test_handleSTTError_transient_leaves_state_untouched(t *testing.T) {
	tr := newTestTranscriber(&fakeSession{})
	tr.handleSTTError(errors.New("elevenlabs stt: API error 500: server error"), utterance{ssrc: 1})
	if tr.sttDisabled.Load() {
		t.Fatal("500 should not disable session")
	}
	if tr.circuitOpen.Load() != 0 {
		t.Fatal("500 should not open quota circuit")
	}
}

// --- postTranscript --------------------------------------------------------

// With sessionOutput wired, PostTranscript routes lines through the output.
// The output falls through to its PostLine, which posts to the thread when
// set or to the parent transcript channel when not. Here we verify via a
// session-output whose thread is unwired (falls back to transcript channel).
func Test_postTranscript_routes_through_sessionOutput(t *testing.T) {
	fs := &fakeSession{
		guildMemberFn: func(_, _ string) (*discordgo.Member, error) {
			return &discordgo.Member{Nick: "Alice"}, nil
		},
	}
	tr := newTestTranscriber(fs)
	// Build an output with empty thread and summary (both REST calls fail
	// silently here because the default fakeSession returns success but we
	// manually construct the output to bypass the init path).
	out := &sessionOutput{
		session:             fs,
		transcriptChannelID: tr.cfg.TranscriptChannelID,
		voiceChannelID:      tr.cfg.VoiceChannelID,
		log:                 discardLogger(),
		speakers:            make(map[string]string),
		startedAt:           time.Now(),
		summaryMsgID:        "summary-1",
		threadChannelID:     "thread-1",
	}
	tr.setOutput(out)
	tr.postTranscript(context.Background(), utterance{ssrc: 1, userID: "u1"}, "hello world")

	if got := fs.sendsByChannel["thread-1"]; len(got) != 1 {
		t.Fatalf("expected one post to thread-1, got %d", len(got))
	}
	line := fs.sendsByChannel["thread-1"][0]
	if !strings.HasPrefix(line, "Alice:") {
		t.Fatalf("expected 'Alice:' prefix, got %q", line)
	}
	if !strings.Contains(line, "hello world") {
		t.Fatalf("expected transcript body, got %q", line)
	}
}

func Test_postTranscript_fallback_when_no_output_wired(t *testing.T) {
	fs := &fakeSession{
		guildMemberFn: func(_, _ string) (*discordgo.Member, error) {
			return &discordgo.Member{Nick: "Alice"}, nil
		},
	}
	tr := newTestTranscriber(fs)
	// No setOutput call — output stays nil. postTranscript should fall back
	// to posting directly in the transcript channel.
	tr.postTranscript(context.Background(), utterance{ssrc: 1, userID: "u1"}, "fallback line")

	if got := fs.sendsByChannel[tr.cfg.TranscriptChannelID]; len(got) != 1 {
		t.Fatalf("expected one send in fallback to transcript channel, got %d", len(got))
	}
	if !strings.Contains(fs.sendsByChannel[tr.cfg.TranscriptChannelID][0], "fallback line") {
		t.Fatalf("fallback line content missing: %q", fs.sendsByChannel[tr.cfg.TranscriptChannelID][0])
	}
}

// --- sweepOnce -------------------------------------------------------------

func Test_sweepOnce_removes_old_orphan_and_keeps_recent(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "voice-old-123.ogg")
	recent := filepath.Join(dir, "voice-new-456.ogg")
	other := filepath.Join(dir, "unrelated.txt")

	for _, p := range []string{old, recent, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-orphanMaxAge - time.Minute)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	tr := &transcriber{tmpDir: dir, log: discardLogger(), nameCache: newDisplayNameCache()}
	tr.sweepOnce(time.Now())

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old orphan not removed: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent file wrongly removed: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated file wrongly removed: %v", err)
	}
}
