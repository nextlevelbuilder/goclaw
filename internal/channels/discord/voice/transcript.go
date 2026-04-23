package voice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// displayNameTTL bounds how long a cached display name is trusted before
// we re-fetch. 1h keeps the hit rate high during a typical voice session
// without letting nickname changes go stale forever.
const displayNameTTL = time.Hour

// orphanSweepInterval is the cadence for removing tmpfiles left behind by
// a panicking or crashing worker. 10min of audio is ~1MB of Ogg/Opus, so
// cleaning anything older than that is a generous upper bound.
const (
	orphanSweepInterval = 5 * time.Minute
	orphanMaxAge        = 10 * time.Minute
	orphanFilePrefix    = "voice-"
)

// transcriber consumes utterances from demux, packages them into Ogg files,
// calls audio.Manager.Transcribe, and posts the result to Discord.
//
// Lifecycle: one transcriber per active voice session. Stop() blocks until
// the worker goroutine has finished in-flight work or context cancels.
type transcriber struct {
	cfg         Config
	session     discordSession // tested via fake
	audioMgr    *audio.Manager
	tmpDir      string
	log         *slog.Logger
	in          chan utterance
	stopCh      chan struct{}
	stopped     atomic.Bool
	wg          sync.WaitGroup
	nameCache   *displayNameCache
	capCounter  *dailyCapCounter
	sttDisabled atomic.Bool // set when we hit an auth error; stays off until Stop
	circuitOpen atomic.Int64 // unix-nano deadline when quota-circuit reopens
}

// discordSession is the subset of *discordgo.Session we need. Matched by
// method set, so production code passes *discordgo.Session directly while
// tests pass a fake.
type discordSession interface {
	GuildMember(guildID, userID string, options ...discordgo.RequestOption) (*discordgo.Member, error)
	ChannelMessageSend(channelID, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

// newTranscriber wires but does not start. Call start() after the voice
// connection is established.
func newTranscriber(cfg Config, session discordSession, audioMgr *audio.Manager, tmpDir string, log *slog.Logger) *transcriber {
	return &transcriber{
		cfg:        cfg,
		session:    session,
		audioMgr:   audioMgr,
		tmpDir:     tmpDir,
		log:        log,
		in:         make(chan utterance, cfg.UtteranceQueueDepth),
		stopCh:     make(chan struct{}),
		nameCache:  newDisplayNameCache(),
		capCounter: newDailyCapCounter(cfg.DailyCapSeconds),
	}
}

// start runs the STT worker and the orphan-tmpfile sweeper.
func (t *transcriber) start(ctx context.Context) {
	t.wg.Add(2)
	go func() {
		defer t.wg.Done()
		defer safego.Recover(nil, "component", "voice.transcriber.worker")
		t.workerLoop(ctx)
	}()
	go func() {
		defer t.wg.Done()
		defer safego.Recover(nil, "component", "voice.transcriber.sweeper")
		t.sweepLoop(ctx)
	}()
}

// stop closes the input channel and waits. Safe to call twice.
func (t *transcriber) stop() {
	if !t.stopped.CompareAndSwap(false, true) {
		return
	}
	close(t.stopCh)
	t.wg.Wait()
}

// inbox exposes the channel demux pushes to.
func (t *transcriber) inbox() chan<- utterance { return t.in }

// workerLoop is the STT pump. One worker is sufficient for v1: ElevenLabs
// latency is ~2-4s per call, utterances are ≤10s each, so a single worker
// keeps up with a 3-4 speaker call. If we ever need concurrency we can
// size this from cfg.
func (t *transcriber) workerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopCh:
			return
		case u, ok := <-t.in:
			if !ok {
				return
			}
			t.processUtterance(ctx, u)
		}
	}
}

// processUtterance is the full STT→post pipeline for one utterance.
// All failure paths must delete the tmpfile.
func (t *transcriber) processUtterance(ctx context.Context, u utterance) {
	// Check daily cap first — if we're over, skip the STT call but continue
	// so queue keeps draining. (We still deliver the package to the worker
	// so it exercises the same path; no oggwriter work is done.)
	if !t.capCounter.tryConsume(u.durationMs) {
		t.log.Warn("voice: daily STT cap reached; skipping transcription",
			"ssrc", u.ssrc, "duration_ms", u.durationMs,
			"cap_seconds", t.cfg.DailyCapSeconds)
		return
	}

	if t.sttDisabled.Load() {
		t.log.Debug("voice: STT disabled for session; skipping",
			"ssrc", u.ssrc, "duration_ms", u.durationMs)
		return
	}
	if dl := t.circuitOpen.Load(); dl != 0 && time.Now().UnixNano() < dl {
		t.log.Debug("voice: STT quota circuit open; skipping",
			"ssrc", u.ssrc, "duration_ms", u.durationMs)
		return
	}

	// Package to tmpfile. defer-remove so every return path cleans up.
	path, err := writeUtteranceOgg(t.tmpDir, u)
	if err != nil {
		t.log.Warn("voice: ogg packaging failed", "err", err, "ssrc", u.ssrc)
		return
	}
	defer func() {
		if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			t.log.Debug("voice: tmpfile remove failed (continuing)", "err", rerr, "path", path)
		}
	}()

	// Wrap the STT context with the Discord channel tag so per-channel STT
	// provider overrides resolve (audio.WithChannel is required — see the
	// manager_stt.go resolveSTTChain logic and plan codex-13).
	sttCtx := audio.WithChannel(ctx, channels.TypeDiscord)

	result, err := t.audioMgr.Transcribe(sttCtx, audio.STTInput{
		FilePath: path,
		MimeType: "audio/ogg",
		Filename: filepath.Base(path),
	}, audio.STTOptions{
		Diarize: false, // we're already speaker-separated via SSRC
	})
	if err != nil {
		t.handleSTTError(err, u)
		return
	}
	if result == nil || strings.TrimSpace(result.Text) == "" {
		// Scribe occasionally returns empty text for non-speech audio
		// (music, keyboard noise). Swallow silently at debug level.
		t.log.Debug("voice: empty transcript", "ssrc", u.ssrc, "duration_ms", u.durationMs)
		return
	}

	t.postTranscript(ctx, u, result.Text)
}

// handleSTTError classifies an STT error into transient / quota / auth per
// the 3-bucket taxonomy agreed in the eng review (issue 2C):
//   - transient (5xx, network): drop with warn; STT chain has its own retries
//   - quota (429): open a 60s circuit; drop in-flight utterances until it closes
//   - auth (401/403): disable STT for the rest of the session; loud log
//
// ElevenLabs provider surfaces errors as formatted strings like
// "elevenlabs stt: API error 429: ...". We string-match; refactoring the
// audio package to return typed errors is out of scope for v1.
func (t *transcriber) handleSTTError(err error, u utterance) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "API error 401"), strings.Contains(msg, "API error 403"):
		// Auth failure: disable for rest of session. A config reload via pod
		// restart is the only way to re-enable (hot-reload is a known-unfixed
		// limitation per plan codex-14).
		t.sttDisabled.Store(true)
		t.log.Error("voice: STT auth failure — disabling for session", "err", err)
	case strings.Contains(msg, "API error 429"):
		// Quota: open a 60s circuit and drop in-flight work.
		deadline := time.Now().Add(60 * time.Second).UnixNano()
		t.circuitOpen.Store(deadline)
		t.log.Warn("voice: STT quota 429 — circuit open 60s",
			"ssrc", u.ssrc, "duration_ms", u.durationMs)
	default:
		// Transient (network/5xx): drop and move on. audio.Manager already
		// walks the provider chain internally, so by the time we see an
		// error every provider has failed once.
		t.log.Warn("voice: STT failed (transient); dropping utterance",
			"err", err, "ssrc", u.ssrc, "duration_ms", u.durationMs)
	}
}

// postTranscript sends "<DisplayName>: <text>" to the configured transcript
// channel. DisplayName is cached to avoid a GuildMember API call per line.
func (t *transcriber) postTranscript(ctx context.Context, u utterance, text string) {
	name := t.resolveDisplayName(u.ssrc, u.userID)
	line := fmt.Sprintf("%s: %s", name, strings.TrimSpace(text))
	// Discord message cap is 2000 chars. Scribe utterances are ≤10s; even
	// speedtalkers rarely exceed a few hundred chars per utterance, so cut
	// conservatively at 1900 to leave headroom for the name prefix.
	if len(line) > 1900 {
		line = line[:1897] + "..."
	}
	if _, err := t.session.ChannelMessageSend(t.cfg.TranscriptChannelID, line); err != nil {
		// A missing channel (10003) or missing-permissions response happens
		// if the channel was deleted or perms changed mid-session. Accepted
		// v1 behaviour: warn-log, keep running. If the channel stays gone,
		// every line will warn — future work tracks the "unknown channel"
		// code and disables for the session (see plan TODO 6).
		t.log.Warn("voice: transcript post failed",
			"err", err, "channel_id", t.cfg.TranscriptChannelID, "ssrc", u.ssrc)
	}
	_ = ctx // reserved for request cancellation if we add per-post timeouts
}

// resolveDisplayName returns a human-readable speaker label. Falls back to
// "user:<ssrc>" when GuildMember is unavailable and userID is empty.
func (t *transcriber) resolveDisplayName(ssrc uint32, userID string) string {
	if userID == "" {
		return fmt.Sprintf("user:%d", ssrc)
	}
	if name, ok := t.nameCache.get(userID); ok {
		return channels.SanitizeDisplayName(name)
	}
	// GuildMember is a REST call; ~50-200ms. Running it from the transcriber
	// worker (not the drain loop) is safe; the worker is already the slow
	// path. Cache on success and on not-found to avoid hammering.
	member, err := t.session.GuildMember(t.cfg.GuildID, userID)
	if err != nil || member == nil {
		t.log.Debug("voice: GuildMember lookup failed; falling back to userID",
			"err", err, "user_id", userID)
		// Cache the userID itself so we don't hammer Discord on every
		// utterance for a user Discord doesn't know about.
		t.nameCache.set(userID, userID)
		return channels.SanitizeDisplayName(userID)
	}
	name := memberDisplayName(member)
	t.nameCache.set(userID, name)
	return channels.SanitizeDisplayName(name)
}

// memberDisplayName pulls the best available display string off a Member:
// guild nickname → global display name → username → userID fallback.
func memberDisplayName(m *discordgo.Member) string {
	if m == nil {
		return ""
	}
	if m.Nick != "" {
		return m.Nick
	}
	if m.User != nil {
		if m.User.GlobalName != "" {
			return m.User.GlobalName
		}
		if m.User.Username != "" {
			return m.User.Username
		}
		return m.User.ID
	}
	return ""
}

// sweepLoop removes orphan ogg tmpfiles. Defensive: the happy-path uses
// defer-remove, so the only files this catches are leftovers from a
// goroutine that panicked and was caught by safego.Recover before the
// defer fired — or from a previous crashed process.
func (t *transcriber) sweepLoop(ctx context.Context) {
	// First sweep runs a short while after start so we don't race with
	// concurrent tmpfile creation during heavy load.
	tick := time.NewTicker(orphanSweepInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopCh:
			return
		case <-tick.C:
			t.sweepOnce(time.Now())
		}
	}
}

func (t *transcriber) sweepOnce(now time.Time) {
	entries, err := os.ReadDir(t.tmpDir)
	if err != nil {
		t.log.Debug("voice: tmpDir read failed in sweeper", "err", err, "tmp_dir", t.tmpDir)
		return
	}
	cutoff := now.Add(-orphanMaxAge)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, orphanFilePrefix) || !strings.HasSuffix(name, ".ogg") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		p := filepath.Join(t.tmpDir, name)
		if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			t.log.Debug("voice: orphan sweep remove failed", "err", rerr, "path", p)
		}
	}
}

// ---- display-name cache ---------------------------------------------------

type displayNameCache struct {
	mu  sync.Mutex
	m   map[string]nameEntry
	now func() time.Time // injectable for tests
}

type nameEntry struct {
	name    string
	expires time.Time
}

func newDisplayNameCache() *displayNameCache {
	return &displayNameCache{m: make(map[string]nameEntry), now: time.Now}
}

func (c *displayNameCache) get(userID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[userID]
	if !ok {
		return "", false
	}
	if c.now().After(e.expires) {
		delete(c.m, userID)
		return "", false
	}
	return e.name, true
}

func (c *displayNameCache) set(userID, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[userID] = nameEntry{name: name, expires: c.now().Add(displayNameTTL)}
}

// ---- daily cap counter ----------------------------------------------------

// dailyCapCounter tracks cumulative audio-seconds transcribed per UTC day.
// Resets at day rollover. Safe for concurrent use. nowFn is injectable.
type dailyCapCounter struct {
	mu         sync.Mutex
	day        string // UTC date key, e.g. "2026-04-23"
	consumedMs int
	capMs      int
	nowFn      func() time.Time
}

func newDailyCapCounter(capSeconds int) *dailyCapCounter {
	return &dailyCapCounter{
		capMs: capSeconds * 1000,
		nowFn: func() time.Time { return time.Now().UTC() },
	}
}

// tryConsume records durMs of audio against the daily budget. Returns true
// if the caller should proceed with STT; false if the budget is exhausted.
// Consumption is best-effort: we book the ms BEFORE the STT call so a
// runaway doesn't blow past the cap while in-flight calls drain. If STT
// fails we don't refund (a failed call still consumed provider quota for
// the 5xx/429/401 status).
func (d *dailyCapCounter) tryConsume(durMs int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	today := d.nowFn().Format("2006-01-02")
	if today != d.day {
		d.day = today
		d.consumedMs = 0
	}
	if d.consumedMs+durMs > d.capMs {
		return false
	}
	d.consumedMs += durMs
	return true
}
