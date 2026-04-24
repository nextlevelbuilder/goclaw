package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/discord/voice"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	pairingDebounceTime = 60 * time.Second
	// defaultTypingTTL caps the typing indicator when the "Thinking..." placeholder is shown —
	// the placeholder carries the in-progress signal, so typing only needs to cover the first
	// flush. When suppress_placeholder is set the indicator is the sole signal for the whole
	// turn, so we raise the cap via suppressedPlaceholderTypingTTL. 30 min is a pragmatic
	// ceiling: it covers long multi-step tool chains while still stopping a stuck indicator
	// if a run fails silently without any Send() call on the channel.
	defaultTypingTTL               = 60 * time.Second
	suppressedPlaceholderTypingTTL = 30 * time.Minute
)

// Channel connects to Discord via the Bot API using gateway events.
type Channel struct {
	*channels.BaseChannel
	session           *discordgo.Session
	config            config.DiscordConfig
	botUserID         string                      // populated on start
	applicationID     string                      // populated on start; required for slash-command registration
	testGuildID       string                      // optional: dev-only guild for instant command propagation (empty = global)
	placeholders      sync.Map                    // placeholderKey string → messageID string
	typingCtrls       sync.Map                    // channelID string → *typing.Controller
	interactionTokens sync.Map                    // discord interaction ID string → *interactionEcho (reply via interaction token)
	agentStore        store.AgentStore            // for agent key lookup (nil = writer commands disabled)
	configPermStore   store.ConfigPermissionStore // for group file writer management (nil = writer commands disabled)
	audioMgr          *audio.Manager              // unified STT via audio.Manager (nil = no STT)
	voiceSupervisor   *voice.Supervisor           // real-time voice-channel join + transcription (nil = disabled)
	// pairingService, pairingDebounce, approvedGroups, groupHistory, historyLimit, requireMention
	// are inherited from channels.BaseChannel.
}

// New creates a new Discord channel from config.
// agentStore and configPermStore are optional (nil = writer commands disabled).
// audioMgr is optional (nil = STT disabled).
func New(cfg config.DiscordConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore,
	agentStore store.AgentStore, configPermStore store.ConfigPermissionStore,
	pendingStore store.PendingMessageStore, audioMgr *audio.Manager) (*Channel, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	// Request necessary intents. Voice-state events are gated by their own
	// intent + a privileged toggle in the Discord Developer Portal; we
	// request the intent unconditionally because the voice-channel feature
	// may be enabled at runtime via channel instance config. Guild/guild
	// members are required alongside voice states so we can resolve user
	// display names for transcripts.
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuilds |
		discordgo.IntentsGuildVoiceStates

	base := channels.NewBaseChannel(channels.TypeDiscord, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	historyLimit := cfg.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}

	ch := &Channel{
		BaseChannel:     base,
		session:         session,
		config:          cfg,
		testGuildID:     cfg.TestGuildID,
		agentStore:      agentStore,
		configPermStore: configPermStore,
		audioMgr:        audioMgr,
	}
	ch.SetRequireMention(requireMention)
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory(channels.TypeDiscord, pendingStore, base.TenantID()))
	ch.SetHistoryLimit(historyLimit)
	return ch, nil
}

// Start opens the Discord gateway connection and begins receiving events.
func (c *Channel) Start(ctx context.Context) error {
	c.GroupHistory().StartFlusher()
	slog.Info("starting discord bot")

	c.session.AddHandler(c.handleMessage)
	c.session.AddHandler(c.handleInteraction)

	if err := c.session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}

	// Fetch bot identity
	user, err := c.session.User("@me")
	if err != nil {
		c.session.Close()
		return fmt.Errorf("fetch discord bot identity: %w", err)
	}
	c.botUserID = user.ID

	// Fetch application ID — required for slash-command registration. Slash
	// commands are owned by the application, not the bot user, and the two
	// can differ (the bot is part of the application).
	app, appErr := c.session.Application("@me")
	if appErr != nil {
		// Non-fatal: the channel still runs, just without slash commands.
		// Typical cause: token lacks applications.commands scope on its
		// OAuth2 installation. Surface the reason in the log and move on.
		slog.Warn("discord: failed to fetch application ID (slash commands disabled)",
			"error", appErr)
	} else if app != nil {
		c.applicationID = app.ID
	}

	c.SetRunning(true)
	slog.Info("discord bot connected",
		"username", user.Username, "id", user.ID, "app_id", c.applicationID)

	// Slash commands: opt-in (default true). If the app-ID fetch failed above
	// we can't register, so surface that as a separate log line instead of
	// merging the two gate conditions into one opaque branch.
	if !c.slashCommandsEnabled() {
		// user explicitly disabled — no action
	} else if c.applicationID == "" {
		slog.Warn("discord: skipping slash command sync (no application ID — check applications.commands OAuth scope on bot token)")
	} else {
		// Register slash commands (non-blocking, retries on transient errors).
		// Bulk-overwrite replaces the full command list, so any stale commands
		// from a previous backend are automatically removed on first boot.
		c.startSlashCommandSync(ctx)
		// Background sweeper for the per-interaction echo map. Ties its
		// lifetime to the channel's ctx so Stop() terminates it.
		c.startInteractionSweeper(ctx)
	}

	// Real-time voice-channel transcription. Opt-in per-instance via
	// voice_channel_enabled; requires audioMgr + transcript channel + guild.
	// Start after the session is open and we have the bot user ID so the
	// supervisor can ignore its own VoiceStateUpdate echoes.
	if err := c.startVoiceSupervisor(ctx); err != nil {
		// Treat misconfiguration as fatal for this Channel; a bot that
		// enables voice but can't transcribe is worse than one that doesn't
		// try. The caller decides whether to abort process startup.
		c.session.Close()
		return fmt.Errorf("discord: voice supervisor: %w", err)
	}

	return nil
}

// startVoiceSupervisor is a no-op when voice_channel_enabled is unset or
// false. When enabled, it validates the instance config (required fields,
// audioMgr presence) and starts the Supervisor.
func (c *Channel) startVoiceSupervisor(ctx context.Context) error {
	if c.config.VoiceChannelEnabled == nil || !*c.config.VoiceChannelEnabled {
		return nil
	}
	if c.audioMgr == nil {
		return fmt.Errorf("voice_channel_enabled=true but no audio.Manager wired (check STT provider config)")
	}
	// Guild is auto-resolved inside the supervisor via session.Channel() —
	// no operator-supplied guild_id needed.
	vcfg := voice.Config{
		VoiceChannelID:      c.config.VoiceChannelID,
		TranscriptChannelID: c.config.VoiceChannelTranscriptChannelID,
		IdleLeaveSeconds:    c.config.VoiceChannelIdleLeaveSeconds,
		MinUtteranceMs:      c.config.VoiceChannelMinUtteranceMs,
		MaxUtteranceMs:      c.config.VoiceChannelMaxUtteranceMs,
		DailyCapSeconds:     c.config.VoiceChannelDailyCapSeconds,
	}
	sup, err := voice.NewSupervisor(vcfg, c.session, c.audioMgr, voice.DefaultTmpDir(), c.botUserID, slog.Default())
	if err != nil {
		return err
	}
	sup.Start(ctx)
	c.voiceSupervisor = sup
	slog.Info("discord: voice supervisor started",
		"voice_channel_id", vcfg.VoiceChannelID,
		"transcript_channel_id", vcfg.TranscriptChannelID,
	)
	return nil
}

// slashCommandsEnabled returns whether slash-command registration is on
// for this channel. Default: true — opt out by setting slash_commands=false
// in the channel config.
func (c *Channel) slashCommandsEnabled() bool {
	return c.config.SlashCommands == nil || *c.config.SlashCommands
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// SetPendingCompaction configures LLM-based auto-compaction for pending messages.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	if gh := c.GroupHistory(); gh != nil {
		gh.SetCompactionConfig(cfg)
	}
}

// SetPendingHistoryTenantID propagates tenant_id to the pending history for DB operations.
func (c *Channel) SetPendingHistoryTenantID(id uuid.UUID) {
	if gh := c.GroupHistory(); gh != nil {
		gh.SetTenantID(id)
	}
}

// Stop closes the Discord gateway connection. Voice supervisor is torn
// down first so its Disconnect call can drain cleanly before the session
// websocket closes — discordgo doesn't reliably reap voice goroutines
// otherwise.
//
// Voice teardown gets a 10s deadline. Longer than most Discord round-trips,
// short enough that a wedged voice goroutine doesn't block the entire bot
// shutdown. If the caller already passed a ctx with a deadline, we honor
// the tighter of the two.
func (c *Channel) Stop(ctx context.Context) error {
	if c.voiceSupervisor != nil {
		voiceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		c.voiceSupervisor.Stop(voiceCtx)
		cancel()
		c.voiceSupervisor = nil
	}
	c.GroupHistory().StopFlusher()
	slog.Info("stopping discord bot")
	c.SetRunning(false)
	return c.session.Close()
}

// Send delivers an outbound message to a Discord channel.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) (err error) {
	if !c.IsRunning() {
		return fmt.Errorf("discord bot not running")
	}

	channelID := msg.ChatID
	if channelID == "" {
		return fmt.Errorf("empty chat ID for discord send")
	}

	// Resolve placeholder key from metadata (inbound message ID), fall back to channelID.
	// Keying by message ID prevents race conditions when multiple messages
	// arrive in the same channel before the first response is sent.
	placeholderKey := channelID
	if pk := msg.Metadata["placeholder_key"]; pk != "" {
		placeholderKey = pk
	}

	// Placeholder update (e.g. LLM retry notification): edit the placeholder
	// but keep it alive for the final response. Don't stop typing or cleanup.
	if msg.Metadata["placeholder_update"] == "true" {
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			if msgID, ok := pID.(string); ok {
				_, _ = c.session.ChannelMessageEdit(channelID, msgID, msg.Content)
			}
		}
		return nil
	}

	// Slash-command interaction reply path. When the inbound came from a
	// slash command, handleInteraction stashed an interactionEcho keyed by
	// the interaction ID and the outbound metadata carries the token. Route
	// via InteractionResponseEdit + followups instead of ChannelMessageSend
	// so the reply appears inline against the slash command in Discord's UI.
	//
	// Falls through to the regular channel-post path if the token has expired
	// or the edit failed UNLESS the reply was marked ephemeral in a guild
	// channel (discord_interaction_flags=ephemeral). In that case
	// trySendViaInteraction returns handled=true and we drop the reply rather
	// than leak private content to the full channel — see
	// ephemeralSuppressesFallback.
	if tok := msg.Metadata["discord_interaction_token"]; tok != "" {
		if handled, sendErr := c.trySendViaInteraction(ctx, msg, tok); handled {
			return sendErr
		}
	}

	typingCtrl := c.currentTypingCtrl(channelID)
	defer func() {
		c.finishTyping(channelID, typingCtrl, err)
	}()

	content := msg.Content

	// TTS auto-apply: convert [[tts]] tagged responses to voice
	if c.audioMgr != nil && content != "" {
		isVoiceInbound := msg.Metadata["is_voice_inbound"] == "true"
		ttsResult, ttsErr := c.audioMgr.AutoApplyToText(ctx, content, "discord", isVoiceInbound, "")
		if ttsErr != nil {
			slog.Debug("discord: tts auto-apply error", "error", ttsErr)
		}
		if ttsResult != nil && ttsResult.AudioPath != "" {
			// Send voice file via media API
			if err := c.sendMediaMessage(channelID, "", []bus.MediaAttachment{{
				URL:         ttsResult.AudioPath,
				ContentType: ttsResult.AudioMime,
			}}); err != nil {
				slog.Warn("discord: tts auto-apply voice send failed, falling back to text", "error", err)
			} else {
				// Voice sent successfully
				strippedText := strings.TrimSpace(ttsResult.Text)
				if strippedText == "" {
					// Voice-only: delete placeholder (no text to show)
					if pID, ok := c.placeholders.LoadAndDelete(placeholderKey); ok {
						if msgID, ok := pID.(string); ok {
							_ = c.session.ChannelMessageDelete(channelID, msgID)
						}
					}
					return nil
				}
				// Has remaining text: let normal flow handle placeholder edit
				content = strippedText
			}
		}
		// Update content with directives stripped (even if TTS not applied)
		if ttsResult != nil {
			content = ttsResult.Text
		}
	}

	// Handle outbound media attachments: send files via Discord's file upload API.
	if len(msg.Media) > 0 {
		// Delete placeholder if present
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			c.placeholders.Delete(placeholderKey)
			if msgID, ok := pID.(string); ok {
				_ = c.session.ChannelMessageDelete(channelID, msgID)
			}
		}
		return c.sendMediaMessage(channelID, content, msg.Media)
	}

	// NO_REPLY cleanup: content is empty when agent suppresses reply.
	// Delete placeholder and return without sending any message.
	if content == "" {
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			c.placeholders.Delete(placeholderKey)
			if msgID, ok := pID.(string); ok {
				_ = c.session.ChannelMessageDelete(channelID, msgID)
			}
		}
		return nil
	}

	// Try to edit the placeholder "Thinking..." message with the first chunk,
	// then send the rest as follow-up messages.
	if pID, ok := c.placeholders.Load(placeholderKey); ok {
		c.placeholders.Delete(placeholderKey)
		if msgID, ok := pID.(string); ok {
			const maxLen = 2000
			editContent := content
			remaining := ""

			if len(editContent) > maxLen {
				// Break at a newline if possible
				cutAt := maxLen
				if idx := lastIndexByte(content[:maxLen], '\n'); idx > maxLen/2 {
					cutAt = idx + 1
				}
				editContent = content[:cutAt]
				remaining = content[cutAt:]
			}

			if _, editErr := c.session.ChannelMessageEdit(channelID, msgID, editContent); editErr == nil {
				// Send remaining content as follow-up messages
				if remaining != "" {
					return c.sendChunked(channelID, remaining)
				}
				return nil
			} else {
				slog.Warn("discord: placeholder edit failed, sending new message",
					"channel_id", channelID, "placeholder_id", msgID, "error", editErr)
			}
		}
		// Fall through to send new message if edit fails
	}

	// Send as new message(s), chunking if needed
	return c.sendChunked(channelID, content)
}

// sendChunked sends a message, splitting into multiple messages if over 2000 chars.
// Uses markdown-aware chunking to avoid splitting inside fenced code blocks.
func (c *Channel) sendChunked(channelID, content string) error {
	const maxLen = 2000

	for _, chunk := range channels.ChunkMarkdown(content, maxLen) {
		if _, err := c.session.ChannelMessageSend(channelID, chunk); err != nil {
			return fmt.Errorf("send discord message: %w", err)
		}
	}

	return nil
}

// lastIndexByte returns the last index of byte c in s, or -1.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func (c *Channel) currentTypingCtrl(channelID string) *typing.Controller {
	ctrl, ok := c.typingCtrls.Load(channelID)
	if !ok {
		return nil
	}

	typed, ok := ctrl.(*typing.Controller)
	if !ok {
		c.typingCtrls.Delete(channelID)
		return nil
	}

	return typed
}

func (c *Channel) finishTyping(channelID string, expected *typing.Controller, sendErr error) {
	if expected == nil {
		return
	}
	if sendErr != nil {
		slog.Warn("discord: outbound send failed; keeping typing indicator active until TTL",
			"channel_id", channelID, "error", sendErr)
		return
	}

	current, ok := c.typingCtrls.Load(channelID)
	if !ok {
		return
	}

	typed, ok := current.(*typing.Controller)
	if !ok {
		c.typingCtrls.Delete(channelID)
		return
	}
	if typed != expected {
		return
	}

	c.typingCtrls.Delete(channelID)
	typed.Stop()
}
