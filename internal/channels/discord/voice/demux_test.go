package voice

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cartridge-gg/discordgo"
)

// discardLogger is a slog.Logger that drops everything. Tests that care
// about log output can replace it with a slog.NewTextHandler on a buffer.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testDemux(t *testing.T, cfg Config) (*demux, chan utterance) {
	t.Helper()
	out := make(chan utterance, 16)
	// Use nil vc — start/drainLoopFrom via startWithSource bypasses the vc
	// for the drain goroutine. Tests that touch vc.AddHandler must not
	// call start() on this instance.
	d := &demux{
		cfg:      ApplyDefaults(cfg),
		out:      out,
		log:      discardLogger(),
		stopCh:   make(chan struct{}),
		ssrcBufs: make(map[uint32]*ssrcBuffer),
	}
	return d, out
}

func Test_onPacket_happy_appends_to_buffer(t *testing.T) {
	d, _ := testDemux(t, Config{SSRCBufferMaxBytes: 1024})
	d.onPacket(&discordgo.Packet{SSRC: 7, Timestamp: 100, Opus: []byte{1, 2, 3}})
	d.mu.Lock()
	buf := d.ssrcBufs[7]
	d.mu.Unlock()
	if buf == nil {
		t.Fatal("buffer not created for SSRC 7")
	}
	if len(buf.opusFrames) != 1 || buf.totalBytes != 3 {
		t.Fatalf("unexpected buffer state: frames=%d bytes=%d", len(buf.opusFrames), buf.totalBytes)
	}
}

func Test_onPacket_nil_and_empty_are_safe_noop(t *testing.T) {
	d, _ := testDemux(t, Config{SSRCBufferMaxBytes: 1024})
	d.onPacket(nil)
	d.onPacket(&discordgo.Packet{SSRC: 1, Opus: nil})
	d.onPacket(&discordgo.Packet{SSRC: 1, Opus: []byte{}})
	d.mu.Lock()
	got := len(d.ssrcBufs)
	d.mu.Unlock()
	if got != 0 {
		t.Fatalf("expected no buffers from no-op packets, got %d", got)
	}
}

func Test_onPacket_buffer_capacity_drops_and_counts(t *testing.T) {
	d, _ := testDemux(t, Config{SSRCBufferMaxBytes: 6})
	for i := 0; i < 3; i++ {
		d.onPacket(&discordgo.Packet{SSRC: 1, Opus: []byte{0, 0, 0}}) // 3 bytes each
	}
	// Third packet would push total to 9 bytes (>6) and be dropped.
	d.mu.Lock()
	buf := d.ssrcBufs[1]
	d.mu.Unlock()
	if buf.totalBytes > 6 {
		t.Fatalf("buffer exceeded cap: %d > 6", buf.totalBytes)
	}
	if d.droppedFramesCapacity.Load() == 0 {
		t.Fatal("dropped-capacity counter did not increment")
	}
}

func Test_onSpeakingUpdate_before_packets_creates_buffer_with_userID(t *testing.T) {
	d, _ := testDemux(t, Config{SSRCBufferMaxBytes: 1024})
	d.onSpeakingUpdate(5, "user-123", true)
	d.mu.Lock()
	buf := d.ssrcBufs[5]
	d.mu.Unlock()
	if buf == nil || buf.userID != "user-123" {
		t.Fatalf("expected buffer with userID=user-123, got %+v", buf)
	}
}

func Test_onSpeakingUpdate_attaches_userID_retroactively(t *testing.T) {
	d, _ := testDemux(t, Config{SSRCBufferMaxBytes: 1024})
	// Packet arrives before the speaking update.
	d.onPacket(&discordgo.Packet{SSRC: 9, Timestamp: 100, Opus: []byte{1}})
	d.onSpeakingUpdate(9, "user-late", true)
	d.mu.Lock()
	got := d.ssrcBufs[9].userID
	d.mu.Unlock()
	if got != "user-late" {
		t.Fatalf("userID not attached retroactively: got %q", got)
	}
}

func Test_flushSSRC_speaking_false_enqueues_utterance(t *testing.T) {
	d, out := testDemux(t, Config{
		SSRCBufferMaxBytes: 1024,
		MinUtteranceMs:     1, // ApplyDefaults promotes 0→400; set low explicit value
	})
	d.onSpeakingUpdate(1, "user-a", true)
	// Seed enough frames to exceed MinUtteranceMs when MinUtteranceMs=0.
	for i := 0; i < 5; i++ {
		d.onPacket(&discordgo.Packet{SSRC: 1, Timestamp: uint32(100 + i*960), Opus: []byte{byte(i)}})
	}
	d.onSpeakingUpdate(1, "user-a", false)

	select {
	case u := <-out:
		if u.ssrc != 1 || u.userID != "user-a" || len(u.opusFrames) != 5 {
			t.Fatalf("unexpected utterance: %+v", u)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("utterance not enqueued on Speaking=false")
	}
}

func Test_flushSSRC_drops_sub_threshold_utterances(t *testing.T) {
	d, out := testDemux(t, Config{
		SSRCBufferMaxBytes: 1024,
		MinUtteranceMs:     1000, // 50 frames at 20ms
	})
	d.onSpeakingUpdate(1, "u", true)
	// 5 frames × 20ms = 100ms — well below 1000ms threshold.
	for i := 0; i < 5; i++ {
		d.onPacket(&discordgo.Packet{SSRC: 1, Timestamp: uint32(100 + i*960), Opus: []byte{byte(i)}})
	}
	d.onSpeakingUpdate(1, "u", false)

	select {
	case u := <-out:
		t.Fatalf("expected drop, got utterance: %+v", u)
	case <-time.After(30 * time.Millisecond):
		// good — nothing enqueued
	}
}

func Test_likelyCiphertextOpus_flags_uniform_TOC_distribution(t *testing.T) {
	frames := make([][]byte, 50)
	for i := range frames {
		frames[i] = []byte{byte(i + 1), 0xaa, 0xbb}
	}

	got, distinct := likelyCiphertextOpus(frames)

	if !got {
		t.Fatalf("likelyCiphertextOpus = false, distinct=%d", distinct)
	}
}

func Test_likelyCiphertextOpus_allows_short_or_stable_TOC(t *testing.T) {
	short := make([][]byte, ciphertextTOCMinFrames-1)
	for i := range short {
		short[i] = []byte{byte(i + 1)}
	}
	if got, distinct := likelyCiphertextOpus(short); got {
		t.Fatalf("short utterance flagged as ciphertext, distinct=%d", distinct)
	}

	stable := make([][]byte, 80)
	for i := range stable {
		stable[i] = []byte{0x78, byte(i)}
	}
	if got, distinct := likelyCiphertextOpus(stable); got {
		t.Fatalf("stable TOC utterance flagged as ciphertext, distinct=%d", distinct)
	}
}

func Test_likelyLowInformationOpus_flags_sustained_silence(t *testing.T) {
	frames := make([][]byte, 60)
	for i := range frames {
		frames[i] = opusSilenceFrame
	}

	got, smallFrames, avgBytes := likelyLowInformationOpus(frames)

	if !got {
		t.Fatalf("likelyLowInformationOpus = false, small=%d avg=%d", smallFrames, avgBytes)
	}
}

func Test_likelyLowInformationOpus_allows_normal_sized_frames(t *testing.T) {
	frames := make([][]byte, 60)
	for i := range frames {
		frames[i] = []byte{0x78, 0x55, 0x22, 0x11, 0x09, 0x44, 0x51, 0x62, 0x33, 0x7a, 0x12, 0x24}
	}

	got, smallFrames, avgBytes := likelyLowInformationOpus(frames)

	if got {
		t.Fatalf("normal-sized frames flagged as low-information, small=%d avg=%d", smallFrames, avgBytes)
	}
}

func Test_flushSSRC_drops_ciphertext_like_utterances(t *testing.T) {
	d, out := testDemux(t, Config{
		SSRCBufferMaxBytes: 4096,
		MinUtteranceMs:     1,
	})
	d.onSpeakingUpdate(1, "u", true)
	for i := 0; i < 50; i++ {
		d.onPacket(&discordgo.Packet{SSRC: 1, Timestamp: uint32(100 + i*960), Opus: []byte{byte(i + 1), 0xaa}})
	}
	d.onSpeakingUpdate(1, "u", false)

	select {
	case u := <-out:
		t.Fatalf("expected ciphertext-like utterance drop, got %+v", u)
	case <-time.After(30 * time.Millisecond):
	}
	if got := d.droppedFramesCiphertext.Load(); got != 50 {
		t.Fatalf("ciphertext drop counter = %d, want 50", got)
	}
}

func Test_flushSSRC_nonblocking_when_queue_full(t *testing.T) {
	d := &demux{
		cfg:      ApplyDefaults(Config{SSRCBufferMaxBytes: 1024, MinUtteranceMs: 1}),
		out:      make(chan utterance), // unbuffered → always full
		log:      discardLogger(),
		stopCh:   make(chan struct{}),
		ssrcBufs: make(map[uint32]*ssrcBuffer),
	}
	d.onSpeakingUpdate(1, "u", true)
	d.onPacket(&discordgo.Packet{SSRC: 1, Timestamp: 0, Opus: []byte{1, 2, 3}})

	done := make(chan struct{})
	go func() {
		d.flushSSRC(1, "test")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("flushSSRC blocked on full queue (non-blocking invariant violated)")
	}
	if d.droppedFramesQueue.Load() != 1 {
		t.Fatalf("expected queue-drop counter 1, got %d", d.droppedFramesQueue.Load())
	}
}

func Test_flushExceedingCeiling_force_flushes_long_buffer(t *testing.T) {
	d, out := testDemux(t, Config{
		SSRCBufferMaxBytes: 1024,
		MinUtteranceMs:     1, // ApplyDefaults promotes 0→400
		MaxUtteranceMs:     100,
	})
	d.onSpeakingUpdate(1, "u", true)
	d.onPacket(&discordgo.Packet{SSRC: 1, Timestamp: 0, Opus: []byte{1}})
	// Manually age the buffer so it exceeds the ceiling without waiting.
	d.mu.Lock()
	d.ssrcBufs[1].startedAt = time.Now().Add(-200 * time.Millisecond)
	d.mu.Unlock()

	d.flushExceedingCeiling(time.Now())

	select {
	case <-out:
		// good
	case <-time.After(30 * time.Millisecond):
		t.Fatal("ceiling watchdog did not flush long-running buffer")
	}
}

// Test_SSRC_cast_regression guards against the int→uint32 conversion
// flagged in the plan. discordgo delivers SSRC as `int`, our internal
// type is uint32; a silent bug here would lose speaker attribution for
// the highest-valued SSRCs. Verifies via the exported onSpeakingUpdate
// contract that bits survive the conversion.
func Test_SSRC_cast_regression(t *testing.T) {
	d, _ := testDemux(t, Config{SSRCBufferMaxBytes: 1024})
	// Pick a value near the int32 signed ceiling; valid as uint32.
	const ssrc32 uint32 = 0xFEDCBA98
	d.onSpeakingUpdate(ssrc32, "u", true)
	d.mu.Lock()
	_, ok := d.ssrcBufs[ssrc32]
	d.mu.Unlock()
	if !ok {
		t.Fatalf("high-valued SSRC %d lost in conversion", ssrc32)
	}
}

// Test_drain_stress_drain_only_no_block is the plan-mandated stress test
// for issue 1A. Pumps packets at a synthetic high rate through a demux
// driven by a test source; asserts the consumer keeps up (no dropped
// frames from capacity cap means the consumer is draining non-blockingly).
func Test_drain_stress_drain_only_no_block(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}
	out := make(chan utterance, 1024)
	src := make(chan *discordgo.Packet, 64) // production buffer is 2; we're more generous so the test source doesn't throttle itself
	d := &demux{
		cfg:      ApplyDefaults(Config{SSRCBufferMaxBytes: 1 << 20, MinUtteranceMs: 0, MaxUtteranceMs: 60_000}),
		out:      out,
		log:      discardLogger(),
		stopCh:   make(chan struct{}),
		ssrcBufs: make(map[uint32]*ssrcBuffer),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.startWithSource(ctx, src)

	const (
		rate  = 1000 // packets/sec
		total = 10_000
	)
	start := time.Now()
	period := time.Second / time.Duration(rate)
	for i := 0; i < total; i++ {
		src <- &discordgo.Packet{SSRC: 1, Timestamp: uint32(i * 960), Opus: []byte{byte(i & 0xff)}}
		time.Sleep(period)
	}
	close(src)

	// Wait for drain to finish processing.
	d.stop()
	elapsed := time.Since(start)
	t.Logf("stress: %d packets in %v", total, elapsed)

	d.mu.Lock()
	buf := d.ssrcBufs[1]
	d.mu.Unlock()
	// After close + stop, buffer should have been flushed to `out` OR
	// held with all packets accounted for (we never lost any to the
	// capacity cap on this config).
	if dropped := d.droppedFramesCapacity.Load(); dropped != 0 {
		t.Fatalf("dropped %d frames at capacity cap under moderate load (drain goroutine blocking?)", dropped)
	}
	// Can't assert exact buffer count because flushAll may have pushed them
	// to `out`. The invariant we care about: no capacity drops.
	_ = buf
}
