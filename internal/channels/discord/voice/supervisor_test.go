package voice

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// testGuildID is injected into Supervisor.resolvedGuildID by newTestSupervisor
// so event handlers match incoming VoiceStateUpdate.GuildID without needing
// a live session.Channel() REST call.
const testGuildID = "guild-1"

// newTestSupervisor wires a Supervisor without starting it. Useful for
// state-machine tests that exercise the event handlers directly without
// needing a live Discord session.
//
// The supervisor's resolvedGuildID is seeded manually because Start (the
// method that normally resolves it via session.Channel) requires a live
// session. Tests that need guild filtering to work use this seeded value.
func newTestSupervisor(t *testing.T, cfg Config) *Supervisor {
	t.Helper()
	if cfg.VoiceChannelID == "" {
		cfg.VoiceChannelID = "vc-1"
	}
	if cfg.TranscriptChannelID == "" {
		cfg.TranscriptChannelID = "tc-1"
	}
	sup, err := NewSupervisor(
		cfg,
		&discordgo.Session{}, // state-machine tests don't hit session methods
		audio.NewManager(audio.ManagerConfig{}),
		t.TempDir(),
		"bot-self",
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	// Pre-seed the resolved guild so event handlers match. Production wiring
	// does this in Supervisor.Start via session.Channel(...).
	sup.resolvedGuildID = testGuildID
	return sup
}

func Test_NewSupervisor_rejects_missing_required_fields(t *testing.T) {
	sess := &discordgo.Session{}
	am := audio.NewManager(audio.ManagerConfig{})

	cases := []struct {
		name string
		cfg  Config
	}{
		{"no vc", Config{TranscriptChannelID: "t"}},
		{"no transcript", Config{VoiceChannelID: "v"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewSupervisor(tc.cfg, sess, am, t.TempDir(), "bot", discardLogger()); !errors.Is(err, ErrMissingConfig) {
				t.Fatalf("expected ErrMissingConfig, got %v", err)
			}
		})
	}
}

func Test_NewSupervisor_rejects_nil_session_or_manager(t *testing.T) {
	cfg := Config{VoiceChannelID: "v", TranscriptChannelID: "t"}
	if _, err := NewSupervisor(cfg, nil, audio.NewManager(audio.ManagerConfig{}), t.TempDir(), "bot", discardLogger()); err == nil {
		t.Fatal("expected err for nil session")
	}
	if _, err := NewSupervisor(cfg, &discordgo.Session{}, nil, t.TempDir(), "bot", discardLogger()); err == nil {
		t.Fatal("expected err for nil audio.Manager")
	}
}

// Events with GuildID mismatching the resolved guild are ignored. This is
// how a single bot installed in multiple guilds (unsupported today but
// defensive against future misconfig) avoids cross-guild bleed.
func Test_onVoiceStateUpdate_ignores_other_guild(t *testing.T) {
	sup := newTestSupervisor(t, Config{})
	sup.onVoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{
		VoiceState: &discordgo.VoiceState{
			GuildID:   "OTHER",
			ChannelID: sup.cfg.VoiceChannelID,
			UserID:    "u1",
		},
	})
	sup.mu.Lock()
	got := len(sup.state.humans)
	sup.mu.Unlock()
	if got != 0 {
		t.Fatalf("other-guild event leaked into presence set: %d", got)
	}
}

// When guild lookup failed at Start (resolvedGuildID is empty), events
// must be ignored even if they otherwise look like they belong to our
// guild. Prevents a missing-perms boot from accidentally joining later.
func Test_onVoiceStateUpdate_ignores_all_when_guild_unresolved(t *testing.T) {
	sup := newTestSupervisor(t, Config{})
	sup.resolvedGuildID = "" // simulate failed resolve at Start
	sup.onVoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{
		VoiceState: &discordgo.VoiceState{
			GuildID:   testGuildID,
			ChannelID: sup.cfg.VoiceChannelID,
			UserID:    "u1",
		},
	})
	sup.mu.Lock()
	got := len(sup.state.humans)
	sup.mu.Unlock()
	if got != 0 {
		t.Fatalf("event processed despite unresolved guild: %d humans tracked", got)
	}
}

func Test_onVoiceStateUpdate_adds_and_removes_humans(t *testing.T) {
	sup := newTestSupervisor(t, Config{})
	// Human joins our channel.
	sup.onVoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{
		VoiceState: &discordgo.VoiceState{
			GuildID:   sup.resolvedGuildID,
			ChannelID: sup.cfg.VoiceChannelID,
			UserID:    "u1",
		},
	})
	sup.mu.Lock()
	_, present := sup.state.humans["u1"]
	joinScheduled := sup.state.joinScheduled
	sup.mu.Unlock()
	if !present {
		t.Fatal("human not added to presence set on join")
	}
	if !joinScheduled {
		t.Fatal("join not scheduled on human join")
	}

	// Human leaves (ChannelID="" means left all channels).
	sup.onVoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{
		VoiceState: &discordgo.VoiceState{
			GuildID:   sup.resolvedGuildID,
			ChannelID: "",
			UserID:    "u1",
		},
	})
	sup.mu.Lock()
	_, stillPresent := sup.state.humans["u1"]
	sup.mu.Unlock()
	if stillPresent {
		t.Fatal("human not removed from presence set on leave")
	}
}

func Test_onVoiceStateUpdate_ignores_mute_toggle_on_same_channel(t *testing.T) {
	sup := newTestSupervisor(t, Config{})
	ev := &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID:   sup.resolvedGuildID,
		ChannelID: sup.cfg.VoiceChannelID,
		UserID:    "u1",
	}}
	sup.onVoiceStateUpdate(nil, ev)
	sup.mu.Lock()
	firstScheduled := sup.state.joinScheduled
	// Simulate the join worker starting and finishing (we didn't run it; just
	// clear the flag so we can detect a re-trigger). Also clear vc so the
	// second call doesn't think we're connected.
	sup.state.joinScheduled = false
	sup.mu.Unlock()
	if !firstScheduled {
		t.Fatal("first VSU didn't schedule join")
	}

	// Same user, same channel — should be a no-op (still present).
	sup.onVoiceStateUpdate(nil, ev)
	sup.mu.Lock()
	secondScheduled := sup.state.joinScheduled
	sup.mu.Unlock()
	if secondScheduled {
		t.Fatal("redundant same-channel VSU re-scheduled a join")
	}
}

// --- kick cooldown --------------------------------------------------------

func Test_onOwnVoiceState_detects_kick_when_connected(t *testing.T) {
	sup := newTestSupervisor(t, Config{KickCooldown: 5 * time.Minute})
	// Simulate an active connection.
	sup.mu.Lock()
	sup.state.vc = &discordgo.VoiceConnection{}
	sup.mu.Unlock()

	// Bot's own state suddenly says we're in a different channel → kick.
	sup.onOwnVoiceState(&discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID:   sup.resolvedGuildID,
		ChannelID: "some-other-channel",
		UserID:    sup.botUserID,
	}})

	sup.mu.Lock()
	defer sup.mu.Unlock()
	if sup.state.kickedUntil.IsZero() {
		t.Fatal("kick cooldown not set on own-channel-mismatch")
	}
	if !sup.inCooldownLocked() {
		t.Fatal("inCooldownLocked did not report cooldown after kick")
	}
}

func Test_onOwnVoiceState_ignores_when_not_connected(t *testing.T) {
	sup := newTestSupervisor(t, Config{})
	// No active vc.
	sup.onOwnVoiceState(&discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID:   sup.resolvedGuildID,
		ChannelID: "",
		UserID:    sup.botUserID,
	}})
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if !sup.state.kickedUntil.IsZero() {
		t.Fatal("kick cooldown set despite bot never being connected")
	}
}

// --- reconcile behaviour ---------------------------------------------------

func Test_reconcile_arms_idle_leave_timer(t *testing.T) {
	sup := newTestSupervisor(t, Config{IdleLeaveSeconds: 1})
	// Simulate connected state with a human who then leaves.
	sup.mu.Lock()
	sup.state.vc = &discordgo.VoiceConnection{}
	sup.state.humans["u1"] = struct{}{}
	sup.reconcileLocked()
	armedBeforeLeave := sup.state.idleLeaveTimer != nil
	sup.mu.Unlock()
	if armedBeforeLeave {
		t.Fatal("idle-leave armed while humans present")
	}

	// Human leaves; timer should arm.
	sup.onVoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID: sup.resolvedGuildID, ChannelID: "", UserID: "u1",
	}})
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if sup.state.idleLeaveTimer == nil {
		t.Fatal("idle-leave timer not armed after last human left")
	}
}

func Test_reconcile_cancels_idle_timer_on_rejoin(t *testing.T) {
	sup := newTestSupervisor(t, Config{IdleLeaveSeconds: 300}) // long timeout so it can't race
	// State: connected, humans=empty, timer armed.
	var timerFired atomic.Bool
	sup.mu.Lock()
	sup.state.vc = &discordgo.VoiceConnection{}
	sup.state.idleLeaveTimer = time.AfterFunc(300*time.Second, func() { timerFired.Store(true) })
	sup.mu.Unlock()

	// Human joins; reconcile should cancel the timer.
	sup.onVoiceStateUpdate(nil, &discordgo.VoiceStateUpdate{VoiceState: &discordgo.VoiceState{
		GuildID:   sup.resolvedGuildID,
		ChannelID: sup.cfg.VoiceChannelID,
		UserID:    "u-new",
	}})
	sup.mu.Lock()
	timerStillSet := sup.state.idleLeaveTimer != nil
	sup.mu.Unlock()
	if timerStillSet {
		t.Fatal("idle-leave timer not cleared on rejoin")
	}
	if timerFired.Load() {
		t.Fatal("timer fired despite rejoin")
	}
}

// --- config defaults -------------------------------------------------------

func Test_ApplyDefaults_preserves_set_values(t *testing.T) {
	in := Config{
		IdleLeaveSeconds: 30,
		MinUtteranceMs:   200,
		MaxUtteranceMs:   5000,
		DailyCapSeconds:  3600,
	}
	out := ApplyDefaults(in)
	if out.IdleLeaveSeconds != 30 || out.MinUtteranceMs != 200 || out.MaxUtteranceMs != 5000 || out.DailyCapSeconds != 3600 {
		t.Fatalf("ApplyDefaults mutated explicit values: %+v", out)
	}
}

func Test_ApplyDefaults_fills_zero_values(t *testing.T) {
	out := ApplyDefaults(Config{})
	if out.IdleLeaveSeconds != 60 {
		t.Errorf("IdleLeaveSeconds default: got %d", out.IdleLeaveSeconds)
	}
	if out.MinUtteranceMs != 400 {
		t.Errorf("MinUtteranceMs default: got %d", out.MinUtteranceMs)
	}
	if out.MaxUtteranceMs != 10_000 {
		t.Errorf("MaxUtteranceMs default: got %d", out.MaxUtteranceMs)
	}
	if out.DailyCapSeconds != 7200 {
		t.Errorf("DailyCapSeconds default: got %d", out.DailyCapSeconds)
	}
	if out.JoinBackoffMin != time.Second {
		t.Errorf("JoinBackoffMin default: got %v", out.JoinBackoffMin)
	}
	if out.JoinBackoffMax != 5*time.Minute {
		t.Errorf("JoinBackoffMax default: got %v", out.JoinBackoffMax)
	}
	if out.KickCooldown != 5*time.Minute {
		t.Errorf("KickCooldown default: got %v", out.KickCooldown)
	}
}
