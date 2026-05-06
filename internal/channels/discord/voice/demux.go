package voice

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cartridge-gg/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// Discord voice frames are 20ms each (48kHz * 0.02s). Used to compute
// rough utterance duration before STT returns the real value.
const opusFrameMs = 20

// demux owns the non-blocking OpusRecv drain + per-SSRC buffering for one
// active VoiceConnection. One-shot: callers create a demux per voice join
// and call Stop when leaving.
//
// Critical invariant (plan decision 1A): the drain goroutine performs
// exactly three operations per packet — lookup/create SSRC buffer, append
// frame, maybe enqueue utterance via non-blocking TryEnqueue — and then
// returns to pull the next packet. Any blocking call here will cost frames
// because OpusRecv is buffered to 2 in bwmarrin/discordgo.
type demux struct {
	cfg     Config
	vc      *discordgo.VoiceConnection
	out     chan<- utterance
	log     *slog.Logger
	stopCh  chan struct{}
	stopped atomic.Bool
	wg      sync.WaitGroup

	// ssrcBufs maps SSRC → per-speaker active buffer. Guarded by mu.
	// VoiceSpeakingUpdate fires on a separate goroutine, so reads from the
	// drain loop and writes from the speaking-update handler must both take
	// the lock. Kept tight: no I/O inside the critical section.
	mu       sync.Mutex
	ssrcBufs map[uint32]*ssrcBuffer

	// Diagnostic counters (exposed for tests + metrics).
	droppedFramesCapacity   atomic.Uint64 // per-SSRC buffer hit SSRCBufferMaxBytes
	droppedFramesQueue      atomic.Uint64 // utterance queue was full
	droppedFramesCiphertext atomic.Uint64 // utterance looked like encrypted bytes, not Opus
}

// ssrcBuffer accumulates Opus frames for a single speaker's current
// utterance. Flushed on VoiceSpeakingUpdate{Speaking=false} or when the
// buffer duration exceeds MaxUtteranceMs (force-flush ceiling).
type ssrcBuffer struct {
	ssrc         uint32
	userID       string // populated lazily from VoiceSpeakingUpdate; may be ""
	opusFrames   [][]byte
	rtpTimestamp []uint32
	totalBytes   int
	startedAt    time.Time
}

// newDemux wires the drain + handlers. The caller must subsequently call
// demux.start; splitting construction and start lets tests wire a fake
// VoiceConnection before the drain loop begins.
func newDemux(cfg Config, vc *discordgo.VoiceConnection, out chan<- utterance, log *slog.Logger) *demux {
	return &demux{
		cfg:      cfg,
		vc:       vc,
		out:      out,
		log:      log,
		stopCh:   make(chan struct{}),
		ssrcBufs: make(map[uint32]*ssrcBuffer),
	}
}

// start registers the VoiceSpeakingUpdate handler and launches the drain
// + ceiling-watchdog goroutines. Idempotent per instance.
//
// Start reads packets from vc.OpusRecv; tests can use startWithSource to
// feed a synthetic channel directly.
func (d *demux) start(ctx context.Context) {
	// SSRC→UserID mapping. VoiceSpeakingUpdate can arrive BEFORE or AFTER
	// the first Opus packet for an SSRC. We handle both:
	//   - update first: create/refresh ssrcBuffer.userID.
	//   - packets first: drain loop creates the buffer with userID="";
	//     this handler retroactively attaches userID when the update arrives.
	//   - Speaking=false: flush and clear.
	d.vc.AddHandler(func(_ *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		// discordgo's VoiceConnection.AddHandler has no remover — handlers
		// are kept for the lifetime of the VoiceConnection. After stop(),
		// the connection may still dispatch one or two stray events (e.g.,
		// a Speaking=false from discordgo's own shutdown), which would hit
		// a now-abandoned out channel. Guard early so those events no-op.
		if d.stopped.Load() {
			return
		}
		// Cast int → uint32 explicitly (plan regression guard). Discord SSRCs
		// are defined as uint32 on the wire; discordgo decodes them into int
		// for historical reasons, so bits above 2^31 would be negative after
		// JSON decode on 32-bit platforms — not a concern on amd64/arm64 but
		// the explicit cast keeps the intent visible.
		//nolint:gosec // G115: Discord SSRCs fit in uint32 by spec.
		ssrc := uint32(vs.SSRC)
		d.onSpeakingUpdate(ssrc, vs.UserID, vs.Speaking)
	})

	d.startWithSource(ctx, d.vc.OpusRecv)
}

// startWithSource launches the drain + ceiling goroutines against an
// arbitrary packet source. Separated from start() so tests can feed a
// synthetic channel without constructing a real VoiceConnection.
func (d *demux) startWithSource(ctx context.Context, src <-chan *discordgo.Packet) {
	d.wg.Add(2)
	go func() {
		defer d.wg.Done()
		defer safego.Recover(nil, "component", "voice.demux.drain")
		d.drainLoopFrom(ctx, src)
	}()
	go func() {
		defer d.wg.Done()
		defer safego.Recover(nil, "component", "voice.demux.ceiling")
		d.ceilingWatchdog(ctx)
	}()
}

// stop signals both goroutines and waits for them.
func (d *demux) stop() {
	if !d.stopped.CompareAndSwap(false, true) {
		return
	}
	close(d.stopCh)
	d.wg.Wait()
	// Flush any still-open per-SSRC buffers on the way out so a clean
	// disconnect doesn't silently drop in-progress speech.
	d.flushAll("stop")
}

// drainLoopFrom is the hot path. See the package doc + struct comment: any
// operation here that can block for more than a single-digit millisecond
// will cost Opus packets (OpusRecv buffer is 2).
func (d *demux) drainLoopFrom(ctx context.Context, src <-chan *discordgo.Packet) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case pkt, ok := <-src:
			if !ok {
				// source closed by discordgo on Disconnect; normal exit.
				return
			}
			d.onPacket(pkt)
		}
	}
}

// onPacket is the per-frame hot path. Strictly no I/O here.
func (d *demux) onPacket(pkt *discordgo.Packet) {
	if pkt == nil || len(pkt.Opus) == 0 {
		return
	}
	d.mu.Lock()
	buf, ok := d.ssrcBufs[pkt.SSRC]
	if !ok {
		buf = &ssrcBuffer{
			ssrc:      pkt.SSRC,
			startedAt: time.Now(),
		}
		d.ssrcBufs[pkt.SSRC] = buf
	}
	// Cap per-SSRC ring to prevent a runaway speaker (or a silent pathological
	// SSRC whose Speaking=false never arrives) from eating unbounded memory.
	if buf.totalBytes+len(pkt.Opus) > d.cfg.SSRCBufferMaxBytes {
		d.droppedFramesCapacity.Add(1)
		d.mu.Unlock()
		return
	}
	buf.opusFrames = append(buf.opusFrames, pkt.Opus)
	buf.rtpTimestamp = append(buf.rtpTimestamp, pkt.Timestamp)
	buf.totalBytes += len(pkt.Opus)
	d.mu.Unlock()
}

// onSpeakingUpdate handles VoiceSpeakingUpdate transitions. Speaking=true
// ensures the buffer exists with a userID; Speaking=false flushes it.
func (d *demux) onSpeakingUpdate(ssrc uint32, userID string, speaking bool) {
	if speaking {
		d.mu.Lock()
		buf, ok := d.ssrcBufs[ssrc]
		if !ok {
			buf = &ssrcBuffer{
				ssrc:      ssrc,
				userID:    userID,
				startedAt: time.Now(),
			}
			d.ssrcBufs[ssrc] = buf
		} else if buf.userID == "" {
			// Retroactively attach userID to a buffer that was populated by
			// packets arriving before the VoiceSpeakingUpdate landed.
			buf.userID = userID
		}
		d.mu.Unlock()
		return
	}
	// Speaking=false → flush and remove the buffer for this SSRC.
	d.flushSSRC(ssrc, "speaking_false")
}

// ceilingWatchdog enforces MaxUtteranceMs so a speaker holding the floor
// for longer than the ceiling gets intermediate utterances shipped to STT
// rather than one giant buffer. Also catches pathological "never sent
// Speaking=false" SSRCs.
func (d *demux) ceilingWatchdog(ctx context.Context) {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case now := <-tick.C:
			d.flushExceedingCeiling(now)
		}
	}
}

func (d *demux) flushExceedingCeiling(now time.Time) {
	max := time.Duration(d.cfg.MaxUtteranceMs) * time.Millisecond
	var toFlush []uint32
	d.mu.Lock()
	for ssrc, buf := range d.ssrcBufs {
		if len(buf.opusFrames) == 0 {
			continue
		}
		if now.Sub(buf.startedAt) >= max {
			toFlush = append(toFlush, ssrc)
		}
	}
	d.mu.Unlock()
	for _, ssrc := range toFlush {
		d.flushSSRC(ssrc, "max_duration")
	}
}

// flushSSRC extracts the per-SSRC buffer, resets it, and enqueues the
// utterance to the transcriber. Drops sub-threshold utterances.
func (d *demux) flushSSRC(ssrc uint32, reason string) {
	d.mu.Lock()
	buf, ok := d.ssrcBufs[ssrc]
	if !ok || len(buf.opusFrames) == 0 {
		// No pending utterance; if the entry exists but is empty, clear it.
		if ok {
			delete(d.ssrcBufs, ssrc)
		}
		d.mu.Unlock()
		return
	}
	// Detach the buffer and clear the map entry BEFORE releasing the lock
	// so a subsequent packet creates a fresh buffer with a correct
	// startedAt. (No lock held during enqueue is deliberate.)
	userID := buf.userID
	u := utterance{
		ssrc:         buf.ssrc,
		userID:       userID,
		opusFrames:   buf.opusFrames,
		rtpTimestamp: buf.rtpTimestamp,
		startedAt:    buf.startedAt,
		durationMs:   len(buf.opusFrames) * opusFrameMs,
	}
	delete(d.ssrcBufs, ssrc)
	d.mu.Unlock()

	// If our own VoiceSpeakingUpdate handler hadn't been registered yet
	// when discordgo dispatched the initial Speaking=true for this SSRC,
	// buf.userID stays empty and the transcriber falls back to
	// "user:<ssrc>" attribution. discordgo records the SSRC→userID map
	// unconditionally on every OP5; pull from that as a backstop so we
	// get proper display names even on the first utterance after join.
	if u.userID == "" && d.vc != nil {
		if late := d.vc.SSRCUserID(buf.ssrc); late != "" {
			u.userID = late
		}
	}

	if u.durationMs < d.cfg.MinUtteranceMs {
		// Drop sub-threshold utterances (clicks, short coughs) without
		// burning an STT call. Log at debug so we don't flood in a noisy
		// channel.
		d.log.Debug("voice: drop short utterance",
			"ssrc", ssrc, "duration_ms", u.durationMs,
			"min_ms", d.cfg.MinUtteranceMs, "reason", reason)
		return
	}

	if likely, distinctTOC := likelyCiphertextOpus(u.opusFrames); likely {
		d.droppedFramesCiphertext.Add(uint64(len(u.opusFrames)))
		d.log.Warn("voice: drop utterance suspected to be encrypted audio",
			"ssrc", ssrc, "duration_ms", u.durationMs, "frames", len(u.opusFrames),
			"distinct_toc", distinctTOC, "reason", reason)
		return
	}

	// Non-blocking send: if the transcriber queue is full, drop and count.
	// Blocking would back-pressure the speaking-update handler, which
	// discordgo dispatches on a shared goroutine; a slow STT could then
	// block other voice events.
	select {
	case d.out <- u:
	default:
		d.droppedFramesQueue.Add(1)
		d.log.Warn("voice: drop utterance — transcriber queue full",
			"ssrc", ssrc, "duration_ms", u.durationMs, "reason", reason)
	}
}

// flushAll enqueues every in-flight buffer. Used on Stop so a clean
// shutdown doesn't silently lose in-progress speech.
func (d *demux) flushAll(reason string) {
	d.mu.Lock()
	ssrcs := make([]uint32, 0, len(d.ssrcBufs))
	for ssrc, buf := range d.ssrcBufs {
		if len(buf.opusFrames) > 0 {
			ssrcs = append(ssrcs, ssrc)
		}
	}
	d.mu.Unlock()
	for _, ssrc := range ssrcs {
		d.flushSSRC(ssrc, reason)
	}
}
