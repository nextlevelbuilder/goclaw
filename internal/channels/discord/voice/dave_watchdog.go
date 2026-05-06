package voice

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cartridge-gg/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// daveWatchdog polls discordgo's DAVE health snapshot and applies bounded
// recovery when the MLS handshake is stuck or has diverged.
type daveWatchdog struct {
	vc              *discordgo.VoiceConnection
	tickEvery       time.Duration
	stuckTimeout    time.Duration
	missingTimeout  time.Duration
	divergedTimeout time.Duration
	resendCap       int
	resendWindow    time.Duration
	resetCap        int
	resetWindow     time.Duration
	humansActive    func() bool
	log             *slog.Logger

	mu      sync.Mutex
	resends []time.Time
	resets  []time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func newDAVEWatchdog(vc *discordgo.VoiceConnection, humansActive func() bool, log *slog.Logger) *daveWatchdog {
	if log == nil {
		log = slog.Default()
	}
	return &daveWatchdog{
		vc:              vc,
		tickEvery:       2 * time.Second,
		stuckTimeout:    10 * time.Second,
		missingTimeout:  15 * time.Second,
		divergedTimeout: 5 * time.Second,
		resendCap:       3,
		resendWindow:    60 * time.Second,
		resetCap:        3,
		resetWindow:     120 * time.Second,
		humansActive:    humansActive,
		log:             log.With("component", "voice.dave_watchdog"),
		stopCh:          make(chan struct{}),
	}
}

func (w *daveWatchdog) start(ctx context.Context) {
	if w == nil || w.vc == nil {
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer safego.Recover(nil, "component", "voice.dave_watchdog")
		w.run(ctx)
	}()
}

func (w *daveWatchdog) stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
	w.wg.Wait()
}

func (w *daveWatchdog) run(ctx context.Context) {
	t := time.NewTicker(w.tickEvery)
	defer t.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctxDone(ctx):
			return
		case now := <-t.C:
			w.tick(now)
		}
	}
}

func (w *daveWatchdog) tick(now time.Time) {
	if w.humansActive != nil && !w.humansActive() {
		return
	}
	h := w.vc.DAVEHealth()
	if !h.Initialized || h.OP26SentAt.IsZero() {
		return
	}

	switch {
	case !h.ProposalFailedSince.IsZero() && now.Sub(h.ProposalFailedSince) > w.divergedTimeout:
		w.tryReset(now, "epoch_diverged")
	case !h.OP30Received && now.Sub(h.OP26SentAt) > w.stuckTimeout:
		w.tryResend(now)
	case h.OP30Received && h.LastMissing > 0 && !h.MissingFirstSeen.IsZero() && now.Sub(h.MissingFirstSeen) > w.missingTimeout:
		w.tryReset(now, "missing_ratchets")
	}
}

func (w *daveWatchdog) tryResend(now time.Time) {
	w.mu.Lock()
	w.resends = pruneOlderThan(w.resends, now.Add(-w.resendWindow))
	if len(w.resends) >= w.resendCap {
		w.mu.Unlock()
		return
	}
	w.resends = append(w.resends, now)
	count := len(w.resends)
	w.mu.Unlock()

	if err := w.vc.ResendDAVEKeyPackage(); err != nil {
		w.log.Warn("voice: DAVE watchdog resend failed", "err", err, "count", count, "cap", w.resendCap)
		return
	}
	w.log.Info("voice: DAVE watchdog resent key package", "count", count, "cap", w.resendCap, "window", w.resendWindow)
}

func (w *daveWatchdog) tryReset(now time.Time, reason string) {
	w.mu.Lock()
	w.resets = pruneOlderThan(w.resets, now.Add(-w.resetWindow))
	if len(w.resets) >= w.resetCap {
		w.mu.Unlock()
		return
	}
	w.resets = append(w.resets, now)
	count := len(w.resets)
	w.mu.Unlock()

	if err := w.vc.SoftResetDAVE(); err != nil {
		w.log.Warn("voice: DAVE watchdog soft-reset failed", "err", err, "reason", reason, "count", count, "cap", w.resetCap)
		return
	}
	w.log.Warn("voice: DAVE watchdog soft-reset", "reason", reason, "count", count, "cap", w.resetCap, "window", w.resetWindow)
}

func pruneOlderThan(stamps []time.Time, cutoff time.Time) []time.Time {
	out := stamps[:0]
	for _, t := range stamps {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

func ctxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
