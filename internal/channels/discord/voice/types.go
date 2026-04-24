// Package voice implements real-time Discord voice-channel capture and
// transcription. The bot joins a designated voice channel when humans are
// present, receives per-user Opus frames via discordgo's VoiceConnection,
// packages each speaker's utterance into an Ogg/Opus container, submits to
// audio.Manager.Transcribe, and posts the transcript to a text channel.
//
// Design constraints surfaced by the plan review (do not remove these
// without re-reading the review):
//
//   - OpusRecv is buffered to 2 packets in bwmarrin/discordgo. The drain
//     goroutine MUST be non-blocking; any STT/HTTP/mutex hold there drops
//     Opus frames silently and mutilates transcripts. See stress test.
//   - discordgo does not reliably close voice goroutines on Session.Close.
//     Callers MUST call Supervisor.Stop before session.Close so we can
//     Disconnect() the VoiceConnection and drain our own goroutines.
//   - ChannelVoiceJoin must be called with deaf=false; deaf=true silently
//     disables OpusRecv (common footgun).
//   - VoiceSpeakingUpdate.SSRC is int, Packet.SSRC is uint32 — cast.
//   - Every new goroutine uses safego.Recover; a voice-path panic must not
//     kill the shared discordgo Session (which would also kill text).
package voice

import (
	"os"
	"time"
)

// DefaultTmpDir returns an OS-appropriate tmp directory for ogg utterance
// files. Separate func (not a package var) so tests can override with
// t.TempDir() on a per-test basis.
func DefaultTmpDir() string { return os.TempDir() }

// Config governs a Supervisor. Zero values fall back to package defaults
// via ApplyDefaults, so callers only set what they care about.
//
// GuildID is deliberately NOT a field: the supervisor resolves it from
// VoiceChannelID via session.Channel(...) at Start time. This kept an
// operator from having to paste two IDs where one would do.
type Config struct {
	VoiceChannelID      string // voice channel to monitor + join (required)
	TranscriptChannelID string // text channel where session summaries post (required)

	IdleLeaveSeconds    int           // seconds after humans→0 before leaving; default 60
	MinUtteranceMs      int           // drop utterances shorter than this; default 400
	MaxUtteranceMs      int           // force-flush utterance at this duration; default 10000
	DailyCapSeconds     int           // per-day audio-seconds STT budget; default 7200 (2h)
	SSRCBufferMaxBytes  int           // per-SSRC ring-buffer cap; default 4 MiB
	UtteranceQueueDepth int           // STT worker queue capacity; default 100
	JoinBackoffMin      time.Duration // initial retry; default 1s
	JoinBackoffMax      time.Duration // retry cap; default 5m
	JoinMaxAttempts     int           // circuit-break threshold; default 10
	KickCooldown        time.Duration // don't rejoin after admin kick; default 5m
}

// ApplyDefaults returns a copy of c with zero fields replaced by defaults.
// Required fields (VoiceChannelID, TranscriptChannelID) are left untouched;
// NewSupervisor surfaces missing ones.
func ApplyDefaults(c Config) Config {
	if c.IdleLeaveSeconds == 0 {
		c.IdleLeaveSeconds = 60
	}
	if c.MinUtteranceMs == 0 {
		c.MinUtteranceMs = 400
	}
	if c.MaxUtteranceMs == 0 {
		c.MaxUtteranceMs = 10_000
	}
	if c.DailyCapSeconds == 0 {
		c.DailyCapSeconds = 7200
	}
	if c.SSRCBufferMaxBytes == 0 {
		c.SSRCBufferMaxBytes = 4 << 20
	}
	if c.UtteranceQueueDepth == 0 {
		c.UtteranceQueueDepth = 100
	}
	if c.JoinBackoffMin == 0 {
		c.JoinBackoffMin = time.Second
	}
	if c.JoinBackoffMax == 0 {
		c.JoinBackoffMax = 5 * time.Minute
	}
	if c.JoinMaxAttempts == 0 {
		c.JoinMaxAttempts = 10
	}
	if c.KickCooldown == 0 {
		c.KickCooldown = 5 * time.Minute
	}
	return c
}

// utterance is an internal unit of work passed from Demux to Transcriber.
// Opus frames are stored already-encoded; we pass the RTP-style timestamp
// per frame so pion's oggwriter can compute granule positions.
type utterance struct {
	ssrc         uint32
	userID       string // may be empty if SpeakingUpdate hasn't landed; Transcriber falls back to "user:<ssrc>"
	opusFrames   [][]byte
	rtpTimestamp []uint32 // parallel to opusFrames; RTP timestamp from each discordgo.Packet
	startedAt    time.Time
	durationMs   int
}
