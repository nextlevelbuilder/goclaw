package channels

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// recorderChannel records every delivered message in arrival order, and can
// block inside Send to simulate a slow upload.
type recorderChannel struct {
	BaseChannel

	mu       sync.Mutex
	received []bus.OutboundMessage

	// blockOn holds Send hostage for messages whose ChatID matches, until
	// release is closed. Empty means never block.
	blockOn string
	release chan struct{}
}

func newRecorderChannel(name string) *recorderChannel {
	rc := &recorderChannel{release: make(chan struct{})}
	rc.BaseChannel = BaseChannel{name: name}
	return rc
}

func (r *recorderChannel) Type() string                  { return TypeTelegram }
func (r *recorderChannel) Start(_ context.Context) error { return nil }
func (r *recorderChannel) Stop(_ context.Context) error  { return nil }
func (r *recorderChannel) IsRunning() bool               { return true }
func (r *recorderChannel) IsAllowed(_ string) bool       { return true }

func (r *recorderChannel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if r.blockOn != "" && msg.ChatID == r.blockOn {
		<-r.release
	}
	r.mu.Lock()
	r.received = append(r.received, msg)
	r.mu.Unlock()
	return nil
}

func (r *recorderChannel) contentsFor(chatID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, m := range r.received {
		if m.ChatID == chatID {
			out = append(out, m.Content)
		}
	}
	return out
}

func (r *recorderChannel) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.received)
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// distinctShardChats returns two chat IDs that hash to different shards, so a
// test can prove two conversations really do run on separate workers.
func distinctShardChats(t *testing.T, channel string, shards int) (string, string) {
	t.Helper()
	base := "chat-0"
	baseIdx := outboundShardIndex(channel, base, shards)
	for i := 1; i < 500; i++ {
		other := fmt.Sprintf("chat-%d", i)
		if outboundShardIndex(channel, other, shards) != baseIdx {
			return base, other
		}
	}
	t.Fatalf("no two chat ids landed on different shards out of %d", shards)
	return "", ""
}

// A conversation must always map to the same shard — that mapping is the only
// thing preserving per-chat ordering once dispatch runs in parallel.
func TestOutboundShardIndex_StableAndBounded(t *testing.T) {
	t.Parallel()

	const shards = 8
	for i := range 200 {
		chatID := fmt.Sprintf("chat-%d", i)
		got := outboundShardIndex("telegram-test", chatID, shards)
		if got < 0 || got >= shards {
			t.Fatalf("shard index %d out of range [0,%d)", got, shards)
		}
		if again := outboundShardIndex("telegram-test", chatID, shards); again != got {
			t.Fatalf("shard index for %q not stable: %d then %d", chatID, got, again)
		}
	}
}

// ChatIDs are only unique within a channel, so the channel name must be part
// of the shard key — otherwise two channels sharing a conversation id would
// have their messages interleaved on one worker.
func TestOutboundShardIndex_ChannelIsPartOfKey(t *testing.T) {
	t.Parallel()

	const shards = 64
	same := 0
	for i := range 100 {
		chatID := fmt.Sprintf("chat-%d", i)
		if outboundShardIndex("alpha", chatID, shards) == outboundShardIndex("beta", chatID, shards) {
			same++
		}
	}
	// With 64 shards, identical placement for every id would mean the channel
	// name was ignored entirely.
	if same == 100 {
		t.Fatal("channel name does not affect shard placement")
	}
}

// The ordering guarantee dispatch actually owes callers: a run emits block
// replies, retry notices and the final answer to one ChatID, and they must
// arrive in that order.
func TestDispatchOutbound_PreservesPerChatOrder(t *testing.T) {
	t.Parallel()

	mb := bus.New()
	mgr := NewManager(mb)
	ch := newRecorderChannel("telegram-test")
	mgr.channels["telegram-test"] = ch

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.dispatchOutbound(ctx)

	const n = 50
	for i := range n {
		mb.PublishOutbound(bus.OutboundMessage{
			Channel: "telegram-test",
			ChatID:  "chat-order",
			Content: fmt.Sprintf("msg-%d", i),
		})
	}

	if !waitFor(t, 5*time.Second, func() bool { return ch.count() >= n }) {
		t.Fatalf("only %d/%d messages delivered", ch.count(), n)
	}

	got := ch.contentsFor("chat-order")
	for i := range n {
		want := fmt.Sprintf("msg-%d", i)
		if got[i] != want {
			t.Fatalf("message %d out of order: got %q, want %q", i, got[i], want)
		}
	}
}

// The regression this whole change exists for: one stalled conversation must
// not hold up every other conversation in the process.
func TestDispatchOutbound_SlowChatDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	shards := outboundShardCount()
	slowChat, fastChat := distinctShardChats(t, "telegram-test", shards)

	mb := bus.New()
	mgr := NewManager(mb)
	ch := newRecorderChannel("telegram-test")
	ch.blockOn = slowChat
	mgr.channels["telegram-test"] = ch

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.dispatchOutbound(ctx)

	// The stuck conversation goes first, exactly as it would when a media
	// upload lands ahead of everyone else's replies.
	mb.PublishOutbound(bus.OutboundMessage{
		Channel: "telegram-test",
		ChatID:  slowChat,
		Content: "slow-upload",
	})
	mb.PublishOutbound(bus.OutboundMessage{
		Channel: "telegram-test",
		ChatID:  fastChat,
		Content: "should-not-wait",
	})

	if !waitFor(t, 5*time.Second, func() bool { return len(ch.contentsFor(fastChat)) == 1 }) {
		close(ch.release)
		t.Fatal("a reply on an unrelated conversation was blocked behind a slow send")
	}

	// The slow one still completes once it is unblocked.
	close(ch.release)
	if !waitFor(t, 5*time.Second, func() bool { return len(ch.contentsFor(slowChat)) == 1 }) {
		t.Fatal("the slow conversation never completed after release")
	}
}

// Temp media was deduped by the fact that serial dispatch removed the file
// before the next message was looked at. Concurrent shards make that a TOCTOU
// race, so the claim must be exclusive.
func TestClaimTempMedia_ExclusiveUnderConcurrency(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())

	path := filepath.Join(t.TempDir(), "shared.jpg")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp media: %v", err)
	}
	// claimTempMedia only guards files under os.TempDir(); t.TempDir() lives
	// there, so this mirrors the real path.
	if !isUnderTempDir(path) {
		t.Skipf("t.TempDir() %q is not under os.TempDir() %q on this platform", path, os.TempDir())
	}

	media := []bus.MediaAttachment{{URL: path, ContentType: "image/jpeg"}}

	const racers = 16
	var wg sync.WaitGroup
	winners := make(chan []string, racers)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kept, claimed := mgr.claimTempMedia(media)
			if len(kept) > 0 {
				winners <- claimed
			}
		}()
	}
	wg.Wait()
	close(winners)

	if n := len(winners); n != 1 {
		t.Fatalf("%d dispatchers claimed the same temp file; want exactly 1", n)
	}
}

// Non-temp media (workspace files) is never removed after delivery, so it must
// pass through unclaimed — otherwise a second message referencing the same
// workspace file would silently lose it.
func TestClaimTempMedia_PassesThroughNonTempAndSkipsMissing(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())

	missing := filepath.Join(os.TempDir(), "goclaw-does-not-exist-12345.bin")
	workspace := "/workspace/report.pdf"

	kept, claimed := mgr.claimTempMedia([]bus.MediaAttachment{
		{URL: workspace},
		{URL: missing},
	})

	if len(kept) != 1 || kept[0].URL != workspace {
		t.Fatalf("kept = %+v, want only the workspace file", kept)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want no claims on non-temp media", claimed)
	}
}

// releaseTempMedia must drop the claim as well as the file, so the claim set
// stays bounded across a long-running process.
func TestReleaseTempMedia_ClearsClaim(t *testing.T) {
	t.Parallel()

	mgr := NewManager(bus.New())

	path := filepath.Join(t.TempDir(), "once.jpg")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp media: %v", err)
	}
	if !isUnderTempDir(path) {
		t.Skipf("t.TempDir() %q is not under os.TempDir() %q on this platform", path, os.TempDir())
	}

	media := []bus.MediaAttachment{{URL: path}}

	kept, claimed := mgr.claimTempMedia(media)
	if len(kept) != 1 || len(claimed) != 1 {
		t.Fatalf("first claim failed: kept=%d claimed=%d", len(kept), len(claimed))
	}
	mgr.releaseTempMedia(claimed)

	if _, stillHeld := mgr.mediaClaims.Load(path); stillHeld {
		t.Fatal("claim survived release; the claim set would grow without bound")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("temp file survived release")
	}
}

func isUnderTempDir(path string) bool {
	rel, err := filepath.Rel(os.TempDir(), path)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && len(rel) > 0 && rel[0] != '.'
}
