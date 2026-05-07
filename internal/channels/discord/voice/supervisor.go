package voice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cartridge-gg/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// ErrMissingConfig is returned from NewSupervisor when required config
// fields are empty. We fail fast rather than silently no-op — a misconfig
// that makes the bot join without a transcript destination is strictly
// worse than a startup error.
var ErrMissingConfig = errors.New("voice: config missing required field")

// Supervisor is the top-level orchestrator for one voice channel binding.
// It listens for VoiceStateUpdate on the shared discordgo.Session, joins
// when humans appear in the configured voice channel, and leaves when they
// all go.
//
// Threading model:
//   - VoiceStateUpdate handler: called on discordgo's dispatch goroutine.
//     Only mutates in-memory state, never blocks on I/O.
//   - Join worker: runs ChannelVoiceJoin with backoff; the blocking part.
//   - Demux goroutines: see demux.go.
//   - Transcriber goroutines: see transcript.go.
type Supervisor struct {
	cfg       Config
	session   *discordgo.Session
	audioMgr  *audio.Manager
	tmpDir    string
	log       *slog.Logger
	botUserID string

	// resolvedGuildID is populated by Start via session.Channel(VoiceChannelID).
	// Empty means the lookup failed — voice is dormant for this pod until
	// config is fixed. We don't require the operator to pass a guild ID
	// alongside the voice channel ID; the two are always related 1:1.
	resolvedGuildID string

	// state is everything mutated under mu. Kept in a dedicated struct so
	// we don't accidentally take the lock for cheap reads like cfg.
	mu    sync.Mutex
	state supState

	// removers deregister handlers on Stop so we don't keep firing after
	// the supervisor has shut down.
	removers []func()

	stopCh   chan struct{}
	stopped  atomic.Bool
	stopOnce sync.Once

	wg sync.WaitGroup

	// Injectable clock for tests.
	nowFn func() time.Time

	// State-machine tests call event handlers directly with a bare
	// discordgo.Session. Disable the slow Discord join path there while still
	// exercising join scheduling state.
	disableJoinWorkerForTests bool
}

type supState struct {
	// humans: user_id → present-in-our-voice-channel. We only track the set;
	// presence is the only signal we need.
	humans map[string]struct{}

	// vc is non-nil while connected. Protected by mu.
	vc *discordgo.VoiceConnection

	// demux + transcriber + daveWatchdog run while connected. Detached and
	// stopped on leaveLocked.
	demux        *demux
	transcriber  *transcriber
	daveWatchdog *daveWatchdog

	// sessionOutput owns the per-session summary message + thread. Paired
	// with demux/transcriber lifetime; leaveLocked calls Close on it before
	// disconnecting.
	output *sessionOutput

	// sessionStartedAt is the wall-clock of onJoinSuccess; used for the
	// final summary's duration stat.
	sessionStartedAt time.Time

	// joinScheduled is set while a join attempt is pending/in-flight to
	// coalesce presence events into a single attempt.
	joinScheduled bool

	// idleLeaveTimer, when non-nil, fires the leave after IdleLeaveSeconds
	// of no humans. Cancelled if a human rejoins within the window.
	idleLeaveTimer *time.Timer

	// kickedUntil suppresses auto-rejoin after the bot is forcibly moved
	// out of the voice channel by an admin.
	kickedUntil time.Time

	// joinFailures is the consecutive-failure counter for circuit-break.
	// Reset on successful join.
	joinFailures int

	// circuitOpenUntil, when non-zero, suppresses join attempts after the
	// failure counter hits JoinMaxAttempts.
	circuitOpenUntil time.Time
}

// NewSupervisor validates config and wires the type. Caller must invoke
// Start on it before expecting any join behaviour.
//
// The guild ID is deliberately NOT a config input — it's resolved at
// Start() time via session.Channel(VoiceChannelID).GuildID. That keeps the
// operator from having to paste the same ID twice.
func NewSupervisor(cfg Config, session *discordgo.Session, audioMgr *audio.Manager, tmpDir, botUserID string, log *slog.Logger) (*Supervisor, error) {
	if cfg.VoiceChannelID == "" {
		return nil, fmt.Errorf("%w: VoiceChannelID", ErrMissingConfig)
	}
	if cfg.TranscriptChannelID == "" {
		return nil, fmt.Errorf("%w: TranscriptChannelID", ErrMissingConfig)
	}
	if session == nil {
		return nil, errors.New("voice: nil session")
	}
	if audioMgr == nil {
		return nil, errors.New("voice: nil audio.Manager")
	}
	if log == nil {
		log = slog.Default()
	}
	cfg = ApplyDefaults(cfg)
	return &Supervisor{
		cfg:       cfg,
		session:   session,
		audioMgr:  audioMgr,
		tmpDir:    tmpDir,
		log:       log.With("component", "voice.supervisor", "voice_channel_id", cfg.VoiceChannelID),
		botUserID: botUserID,
		state:     supState{humans: make(map[string]struct{})},
		stopCh:    make(chan struct{}),
		nowFn:     time.Now,
	}, nil
}

// Start registers Discord gateway handlers and returns. Actual joining
// happens lazily in response to VoiceStateUpdate events.
//
// Before registering handlers, Start resolves the voice channel's guild
// via session.Channel(...). Failure (bot not in guild, no perms, network)
// is NOT fatal: the supervisor records empty resolvedGuildID, logs a loud
// error, and no-ops on every subsequent VoiceStateUpdate. Text handling is
// unaffected either way.
func (s *Supervisor) Start(ctx context.Context) {
	// Resolve guild. 5s budget — Channel() is a single REST call.
	resolveCtx, cancelResolve := context.WithTimeout(ctx, 5*time.Second)
	ch, err := s.session.Channel(s.cfg.VoiceChannelID, discordgo.WithContext(resolveCtx))
	cancelResolve()
	if err != nil || ch == nil {
		s.log.Error("voice: cannot resolve guild for voice channel — supervisor will no-op",
			"err", err, "voice_channel_id", s.cfg.VoiceChannelID)
	} else {
		s.resolvedGuildID = ch.GuildID
		s.log = s.log.With("guild_id", s.resolvedGuildID)
	}

	s.removers = append(s.removers,
		s.session.AddHandler(s.onVoiceStateUpdate),
		s.session.AddHandler(s.onGuildCreate),
	)
	s.log.Info("voice: supervisor started",
		"transcript_channel", s.cfg.TranscriptChannelID,
		"idle_leave_s", s.cfg.IdleLeaveSeconds,
		"daily_cap_s", s.cfg.DailyCapSeconds,
		"resolved_guild_id", s.resolvedGuildID,
	)
	// Guild may have already been cached (ready fired before we registered).
	// Prime presence from the state if available. Best-effort: State may be
	// disabled in the parent session config, in which case we rely on
	// future VoiceStateUpdate deltas.
	s.primeFromState()
	// Parent-context cancellation closes our stopCh so goroutines drain
	// cleanly without requiring the caller to also call Stop(). Stop() is
	// still the preferred shutdown path because it waits for drain; ctx
	// cancel is a safety net for callers that forget.
	if ctx != nil && ctx.Done() != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer safego.Recover(nil, "component", "voice.supervisor.ctx-watch")
			select {
			case <-ctx.Done():
				s.stopOnce.Do(func() {
					s.stopped.Store(true)
					close(s.stopCh)
				})
			case <-s.stopCh:
			}
		}()
	}
}

// Stop disconnects any active voice connection, tears down goroutines, and
// deregisters gateway handlers. Idempotent. Blocks until fully drained or
// ctx is cancelled.
//
// Drain time depends on discordgo's voice teardown, which can take several
// seconds on a slow network. Callers who need a hard cap MUST pass a ctx
// with WithTimeout. A context.Background() argument will wait indefinitely
// — intentional, because discarding a live voice connection without
// Disconnect() can leak UDP sockets and WebSocket connections server-side.
func (s *Supervisor) Stop(ctx context.Context) {
	s.stopOnce.Do(func() {
		s.stopped.Store(true)
		close(s.stopCh)
	})

	s.mu.Lock()
	removers := s.removers
	s.removers = nil
	s.mu.Unlock()
	for _, rm := range removers {
		if rm != nil {
			rm()
		}
	}

	// Detach + tear down any active connection. leaveLocked closes the
	// underlying VoiceConnection on a detached goroutine tracked by wg.
	s.mu.Lock()
	s.leaveLocked("supervisor_stop")
	s.mu.Unlock()

	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		// Caller-owned deadline. The detached leave-goroutine continues
		// in the background; its Disconnect will eventually complete (or
		// error out) without our supervision. Logged so operators can
		// spot a slow discordgo shutdown.
		s.log.Warn("voice: Stop() wait cancelled by caller context",
			"err", ctx.Err())
	}
}

// ----- event handlers (called on discordgo's dispatch goroutine) -----

func (s *Supervisor) onVoiceStateUpdate(_ *discordgo.Session, ev *discordgo.VoiceStateUpdate) {
	if s.stopped.Load() {
		return
	}
	if ev == nil || ev.VoiceState == nil {
		return
	}
	if s.resolvedGuildID == "" || ev.GuildID != s.resolvedGuildID {
		return // guild lookup failed at start, or event from a different guild
	}

	// Handle the bot's own voice state first — if someone (or we) moved it
	// out of the target channel we treat it as a kick signal.
	if ev.UserID == s.botUserID {
		s.onOwnVoiceState(ev)
		return
	}

	inOurChannel := ev.ChannelID == s.cfg.VoiceChannelID
	s.mu.Lock()
	defer s.mu.Unlock()

	_, wasPresent := s.state.humans[ev.UserID]
	switch {
	case inOurChannel && !wasPresent:
		s.state.humans[ev.UserID] = struct{}{}
	case !inOurChannel && wasPresent:
		delete(s.state.humans, ev.UserID)
	default:
		// Either still-present (mute/deaf/server-deaf change) or
		// still-not-present (move between other channels). No presence
		// transition — nothing to decide.
		return
	}
	s.reconcileLocked()
}

// onGuildCreate primes the humans set from the guild's cached voice states
// when the gateway sends the initial guild payload. Important for the case
// where we boot while a call is already in progress.
func (s *Supervisor) onGuildCreate(_ *discordgo.Session, ev *discordgo.GuildCreate) {
	if s.stopped.Load() || ev == nil || ev.Guild == nil {
		return
	}
	if s.resolvedGuildID == "" || ev.Guild.ID != s.resolvedGuildID {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, vs := range ev.Guild.VoiceStates {
		if vs == nil || vs.ChannelID != s.cfg.VoiceChannelID {
			continue
		}
		if vs.UserID == s.botUserID {
			continue
		}
		s.state.humans[vs.UserID] = struct{}{}
	}
	s.reconcileLocked()
}

// onOwnVoiceState handles updates about the bot itself. If the bot was
// connected and its new channel is different from our target, treat it as
// an admin kick/move and arm the cooldown.
//
// LOAD-BEARING INVARIANT: leaveLocked sets state.vc = nil BEFORE spawning
// the detached goroutine that calls vc.Disconnect(). discordgo emits a
// VoiceStateUpdate for our bot when that Disconnect succeeds; that handler
// re-enters this function, but state.vc is already nil so the early-return
// below fires and we don't double-leave. Do NOT reorder leaveLocked's
// field clears — this guard depends on them happening before the goroutine
// runs.
func (s *Supervisor) onOwnVoiceState(ev *discordgo.VoiceStateUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connected := s.state.vc != nil
	if !connected {
		return // we never joined, or we already left voluntarily (or our own Disconnect echoed back — see load-bearing invariant above)
	}
	if ev.ChannelID == s.cfg.VoiceChannelID {
		return // still in the right place (e.g., mute toggled)
	}
	// We thought we were connected but Discord just told us we're elsewhere.
	// Tear down cleanly and arm the kick cooldown.
	s.state.kickedUntil = s.nowFn().Add(s.cfg.KickCooldown)
	s.log.Warn("voice: bot removed from voice channel; cooling off",
		"new_channel_id", ev.ChannelID,
		"cooldown_until", s.state.kickedUntil.Format(time.RFC3339))
	s.leaveLocked("bot_kicked")
}

// primeFromState reads discordgo.State if enabled and seeds humans[] so we
// can join immediately on boot if a call is already in progress.
func (s *Supervisor) primeFromState() {
	if s.session.State == nil || s.resolvedGuildID == "" {
		return
	}
	guild, err := s.session.State.Guild(s.resolvedGuildID)
	if err != nil || guild == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, vs := range guild.VoiceStates {
		if vs == nil || vs.ChannelID != s.cfg.VoiceChannelID {
			continue
		}
		if vs.UserID == s.botUserID {
			continue
		}
		s.state.humans[vs.UserID] = struct{}{}
	}
	s.reconcileLocked()
}

// ----- reconcile (mu held) -----

// reconcileLocked decides what to do based on current state. Called under
// s.mu. Fire-and-forget for join — the join worker handles the slow
// ChannelVoiceJoin call outside the lock.
func (s *Supervisor) reconcileLocked() {
	humans := len(s.state.humans)
	connected := s.state.vc != nil

	// Cancel any idle-leave timer the moment a human appears.
	if humans > 0 && s.state.idleLeaveTimer != nil {
		s.state.idleLeaveTimer.Stop()
		s.state.idleLeaveTimer = nil
	}

	switch {
	case humans > 0 && !connected && !s.state.joinScheduled && !s.inCooldownLocked():
		s.state.joinScheduled = true
		if s.disableJoinWorkerForTests {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer safego.Recover(nil, "component", "voice.supervisor.join")
			s.joinWorker()
		}()
	case humans == 0 && connected && s.state.idleLeaveTimer == nil:
		d := time.Duration(s.cfg.IdleLeaveSeconds) * time.Second
		s.state.idleLeaveTimer = time.AfterFunc(d, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.state.humans) == 0 && s.state.vc != nil {
				s.leaveLocked("idle_timeout")
			}
			s.state.idleLeaveTimer = nil
		})
	}
}

func (s *Supervisor) inCooldownLocked() bool {
	if !s.state.kickedUntil.IsZero() && s.nowFn().Before(s.state.kickedUntil) {
		return true
	}
	if !s.state.circuitOpenUntil.IsZero() && s.nowFn().Before(s.state.circuitOpenUntil) {
		return true
	}
	return false
}

// ----- join/leave (slow path; runs off the handler goroutine) -----

// joinWorker drives the exp-backoff ChannelVoiceJoin attempts. Runs in its
// own goroutine; acquires s.mu only to read config snapshots and update
// state transitions.
func (s *Supervisor) joinWorker() {
	defer func() {
		s.mu.Lock()
		s.state.joinScheduled = false
		s.mu.Unlock()
	}()

	backoff := s.cfg.JoinBackoffMin
	attempt := 0
	for {
		if s.stopped.Load() {
			return
		}
		s.mu.Lock()
		if len(s.state.humans) == 0 {
			// Room emptied while we were waiting. Bail quietly.
			s.mu.Unlock()
			return
		}
		if s.state.vc != nil {
			// Reconcile races: another caller already established the VC.
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		attempt++
		vc, err := s.session.ChannelVoiceJoin(s.resolvedGuildID, s.cfg.VoiceChannelID, true /*mute*/, false /*deaf — MUST be false or OpusRecv stays empty*/)
		if err == nil && vc != nil {
			s.onJoinSuccess(vc)
			return
		}

		s.mu.Lock()
		s.state.joinFailures++
		failures := s.state.joinFailures
		s.mu.Unlock()

		s.log.Warn("voice: ChannelVoiceJoin failed",
			"err", err, "attempt", attempt, "consecutive_failures", failures)

		if failures >= s.cfg.JoinMaxAttempts {
			// Circuit-break: stop trying for this cooldown window. The
			// next VoiceStateUpdate after expiry will re-trigger.
			cooldown := s.cfg.JoinBackoffMax
			s.mu.Lock()
			s.state.circuitOpenUntil = s.nowFn().Add(cooldown)
			s.state.joinFailures = 0
			s.mu.Unlock()
			s.log.Error("voice: join circuit-break engaged",
				"failures", failures, "cooldown", cooldown)
			return
		}

		// Exp-backoff with cap. Clamped select so Stop() can interrupt.
		if backoff > s.cfg.JoinBackoffMax {
			backoff = s.cfg.JoinBackoffMax
		}
		select {
		case <-s.stopCh:
			return
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// onJoinSuccess handles a successful ChannelVoiceJoin by creating the
// session's Discord artefacts (summary message + thread) and wiring the
// demux + transcriber. The sessionOutput's initial REST calls are made
// OUTSIDE the mu critical section (s.mu is briefly released around them)
// because each can take hundreds of milliseconds on a slow network;
// holding the supervisor lock for that long would block every concurrent
// VoiceStateUpdate handler.
func (s *Supervisor) onJoinSuccess(vc *discordgo.VoiceConnection) {
	// REST-heavy setup BEFORE taking the lock. sessionOutput construction
	// issues up to three REST calls (Channel lookup, ChannelMessageSend,
	// MessageThreadStart). Total budget: 10s — enough for all three to
	// complete on a tail-latency network, short enough that a wedged call
	// doesn't keep the session half-wired.
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	output := newSessionOutput(setupCtx, s.session, s.cfg.TranscriptChannelID, s.cfg.VoiceChannelID, s.log, s.cfg.TranscriptSummarizer)
	cancelSetup()
	startedAt := s.nowFn()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped.Load() {
		// Shutdown arrived while we were creating the session output. Close
		// the output we just built and disconnect the VC. Run on a detached
		// goroutine so we don't pay REST latency under mu.
		go func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			output.Close(closeCtx, 0)
			cancel()
			s.disconnectVoice(vc, "post-stop")
		}()
		return
	}

	s.state.vc = vc
	s.state.output = output
	s.state.sessionStartedAt = startedAt
	s.state.joinFailures = 0
	s.state.circuitOpenUntil = time.Time{}
	// kickedUntil is normally in the past by the time a successful join
	// happens (reconcile waits out the cooldown before retrying), but clear
	// it explicitly so inCooldownLocked doesn't keep returning true on a
	// stale timestamp after the cooldown window.
	s.state.kickedUntil = time.Time{}
	s.log.Info("voice: joined voice channel", "channel_id", s.cfg.VoiceChannelID)

	// Start the transcriber first so its queue is ready before demux starts
	// enqueueing utterances. Wire the output BEFORE start() so the first
	// utterance processed sees a non-nil output.
	tr := newTranscriber(s.cfg, s.session, s.audioMgr, s.tmpDir, s.resolvedGuildID, s.log)
	tr.setOutput(output)
	tr.start(context.Background())
	s.state.transcriber = tr

	wd := newDAVEWatchdog(vc, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.stopped.Load() && s.state.vc == vc && len(s.state.humans) > 0
	}, s.log)
	wd.start(context.Background())
	s.state.daveWatchdog = wd

	dm := newDemux(s.cfg, vc, tr.inbox(), s.log)
	dm.start(context.Background())
	s.state.demux = dm
}

// leaveLocked tears down the active VoiceConnection and the subsystems
// tied to it. Caller must hold s.mu.
func (s *Supervisor) leaveLocked(reason string) {
	vc := s.state.vc
	dm := s.state.demux
	tr := s.state.transcriber
	wd := s.state.daveWatchdog
	output := s.state.output
	startedAt := s.state.sessionStartedAt
	s.state.vc = nil
	s.state.demux = nil
	s.state.transcriber = nil
	s.state.daveWatchdog = nil
	s.state.output = nil
	s.state.sessionStartedAt = time.Time{}
	if s.state.idleLeaveTimer != nil {
		s.state.idleLeaveTimer.Stop()
		s.state.idleLeaveTimer = nil
	}

	if vc == nil && dm == nil && tr == nil && wd == nil && output == nil {
		return
	}

	s.log.Info("voice: leaving voice channel", "reason", reason)

	// Tear down in reverse order of construction so producers stop before
	// consumers. Each stop is synchronous and drains its own goroutines.
	// Run the shutdown on a detached goroutine so handlers holding mu
	// don't block on our own goroutines (which may want the lock back).
	//
	// Ordering rationale:
	//   1. daveWatchdog.stop() — stops recovery actions during teardown.
	//   2. demux.stop() — stops producing utterances; flushes in-flight.
	//   3. transcriber.stop() — drains queued utterances; stops posting.
	//   4. output.Close() — edits the summary with final stats. Runs AFTER
	//      transcriber stops so its utterance count is stable.
	//   5. vc.Disconnect() — tears down the voice socket; fires our own
	//      VoiceStateUpdate which the bot-self handler sees as a kick but
	//      we've already cleared state so that path no-ops.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer safego.Recover(nil, "component", "voice.supervisor.leave")
		if wd != nil {
			wd.stop()
		}
		if dm != nil {
			dm.stop()
		}
		if tr != nil {
			tr.stop()
		}
		if output != nil {
			duration := time.Duration(0)
			if !startedAt.IsZero() {
				duration = time.Since(startedAt)
			}
			// Budget covers up to two REST calls (delete-thread +
			// delete-summary on the empty-session path) OR an LLM
			// summarizer call + summary-message edit on the
			// non-empty path. 30s is generous for both; a wedged
			// provider can't pin the supervisor longer than that.
			closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			output.Close(closeCtx, duration)
			cancel()
		}
		if vc != nil {
			s.disconnectVoice(vc, "leave")
		}
	}()
}

func (s *Supervisor) disconnectVoice(vc *discordgo.VoiceConnection, phase string) {
	if vc == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.log.Warn("voice: Disconnect panic recovered", "phase", phase, "panic", r)
		}
	}()
	if err := vc.Disconnect(); err != nil {
		s.log.Debug("voice: Disconnect error", "phase", phase, "err", err)
	}
}
