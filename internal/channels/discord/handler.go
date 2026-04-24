package discord

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleMessage processes incoming Discord messages.
func (c *Channel) handleMessage(_ *discordgo.Session, m *discordgo.MessageCreate) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())
	// Ignore bot's own messages
	if m.Author == nil || m.Author.ID == c.botUserID {
		return
	}

	// Ignore bot messages
	if m.Author.Bot {
		return
	}

	senderID := m.Author.ID
	senderName := resolveDisplayName(m)

	channelID := m.ChannelID
	isDM := m.GuildID == ""

	// DM/Group policy check
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	if isDM {
		if !c.checkDMPolicy(ctx, senderID, channelID) {
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, senderID, channelID) {
			slog.Debug("discord group message rejected by policy",
				"user_id", senderID,
				"username", senderName,
			)
			return
		}
	}

	// Handle bot commands (writer management, etc.) before further processing.
	if c.tryHandleCommand(m) {
		return
	}

	// Build content
	content := m.Content

	// Build reply context if replying to another message.
	if m.ReferencedMessage != nil {
		author := "unknown"
		if m.ReferencedMessage.Author != nil {
			author = m.ReferencedMessage.Author.Username
		}
		body := channels.Truncate(m.ReferencedMessage.Content, 500)
		replyCtx := fmt.Sprintf("[Replying to %s]\n%s\n[/Replying]", author, body)
		if content != "" {
			content = replyCtx + "\n\n" + content
		} else {
			content = replyCtx
		}
	}

	// Resolve media attachments (download files, classify types)
	maxBytes := c.config.MediaMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMediaMaxBytes
	}
	mediaList := resolveMedia(m.Attachments, maxBytes)

	// Download media from replied-to message and merge (reply first, current second).
	if m.ReferencedMessage != nil && len(m.ReferencedMessage.Attachments) > 0 {
		replyMedia := resolveMedia(m.ReferencedMessage.Attachments, maxBytes)
		for i := range replyMedia {
			replyMedia[i].FromReply = true
		}
		mediaList = append(replyMedia, mediaList...)
	}

	// Process media: STT, document extraction, build tags
	var mediaFiles []bus.MediaFile
	if len(mediaList) > 0 {
		var extraContent string
		for i := range mediaList {
			mi := &mediaList[i]

			switch mi.Type {
			case media.TypeAudio, media.TypeVoice:
				var transcript string
				var sttErr error
				if c.audioMgr != nil {
					sttCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					res, err := c.audioMgr.Transcribe(sttCtx, audio.STTInput{FilePath: mi.FilePath, MimeType: "audio/ogg"}, audio.STTOptions{})
					cancel()
					if err == nil && res != nil {
						transcript = res.Text
					} else {
						sttErr = err
					}
				}
				if sttErr != nil {
					slog.Warn("discord: STT transcription failed",
						"type", mi.Type, "error", sttErr,
					)
				} else {
					mi.Transcript = transcript
				}

			case media.TypeDocument:
				if mi.FileName != "" && mi.FilePath != "" {
					docContent, err := media.ExtractDocumentContent(mi.FilePath, mi.FileName)
					if err != nil {
						slog.Warn("discord: document extraction failed", "file", mi.FileName, "error", err)
					} else if docContent != "" {
						extraContent += "\n\n" + docContent
					}
				}
			}

			if mi.FilePath != "" {
				mediaFiles = append(mediaFiles, bus.MediaFile{
					Path:     mi.FilePath,
					MimeType: mi.ContentType,
					Filename: mi.FileName,
				})
			}
		}

		// Build media tags AFTER processing so transcript fields are populated.
		mediaTags := media.BuildMediaTags(mediaList)
		if mediaTags != "" {
			if content != "" {
				content = mediaTags + "\n\n" + content
			} else {
				content = mediaTags
			}
		}

		if extraContent != "" {
			content += extraContent
		}
	}

	if content == "" {
		content = "[empty message]"
	}

	// Mention gating: in groups, only respond when bot is @mentioned (default true).
	// When not mentioned, record message to pending history for later context.
	if peerKind == "group" && c.RequireMention() {
		mentioned := false
		for _, u := range m.Mentions {
			if u.ID == c.botUserID {
				mentioned = true
				break
			}
		}
		// Reply to bot's message counts as implicit mention.
		if !mentioned && m.ReferencedMessage != nil &&
			m.ReferencedMessage.Author != nil &&
			m.ReferencedMessage.Author.ID == c.botUserID {
			mentioned = true
		}
		if !mentioned {
			// Collect media file paths for group history context.
			var mediaPaths []string
			for _, mf := range mediaFiles {
				if mf.Path != "" {
					mediaPaths = append(mediaPaths, mf.Path)
				}
			}
			c.GroupHistory().Record(channelID, channels.HistoryEntry{
				Sender:    senderName,
				SenderID:  senderID,
				Body:      content,
				Media:     mediaPaths,
				Timestamp: m.Timestamp,
				MessageID: m.ID,
			}, c.HistoryLimit())

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, senderName, m.Author.Username, "group", "user", "", "")
			}

			slog.Debug("discord group message recorded (no mention)",
				"channel_id", channelID,
				"user_id", senderID,
				"username", senderName,
			)
			return
		}
	}

	slog.Debug("discord message received",
		"sender_id", senderID,
		"channel_id", channelID,
		"is_dm", isDM,
		"preview", channels.Truncate(content, 50),
	)

	// Send typing indicator with keepalive + TTL safety net.
	// Discord typing expires after 10s, so keepalive every 9s.
	// TTL auto-stops after 60s to prevent stuck indicators.
	typingCtrl := typing.New(typing.Options{
		MaxDuration:       60 * time.Second,
		KeepaliveInterval: 9 * time.Second,
		StartFn: func() error {
			return c.session.ChannelTyping(channelID)
		},
	})
	// Stop previous typing controller for this channel (if any)
	if prev, ok := c.typingCtrls.Load(channelID); ok {
		prev.(*typing.Controller).Stop()
	}
	c.typingCtrls.Store(channelID, typingCtrl)
	typingCtrl.Start()

	// Send placeholder "Thinking..." message.
	// Key by inbound message ID (not channel ID) to avoid race conditions
	// when multiple messages arrive in the same channel concurrently.
	placeholder, err := c.session.ChannelMessageSend(channelID, "Thinking...")
	if err == nil {
		c.placeholders.Store(m.ID, placeholder.ID)
	}

	// Strip bot @mention from content — it's just the trigger, not meaningful.
	content = strings.ReplaceAll(content, "<@"+c.botUserID+">", "")
	content = strings.TrimSpace(content)

	// Build final content with group context.
	finalContent := content
	if peerKind == "group" {
		annotated := fmt.Sprintf("[From: %s (<@%s>)]\n%s", senderName, senderID, content)
		if c.HistoryLimit() > 0 {
			finalContent = c.GroupHistory().BuildContext(channelID, annotated, c.HistoryLimit())
		} else {
			finalContent = annotated
		}
		// Collect media from pending history entries (sent before this @mention).
		// Original filename not retained by CollectMedia; use disk basename so
		// persistMedia's sanitizer gets a meaningful stem instead of UUID fallback.
		if histMediaPaths := c.GroupHistory().CollectMedia(channelID); len(histMediaPaths) > 0 {
			for _, p := range histMediaPaths {
				mediaFiles = append(mediaFiles, bus.MediaFile{Path: p, Filename: filepath.Base(p)})
			}
		}
	}

	metadata := map[string]string{
		"message_id":      m.ID,
		"user_id":         senderID,
		"username":        m.Author.Username,
		"display_name":    channels.SanitizeDisplayName(senderName),
		"guild_id":        m.GuildID,
		"channel_id":      channelID,
		"is_dm":           fmt.Sprintf("%t", isDM),
		"placeholder_key": m.ID, // keyed by inbound message ID for placeholder lookup
	}

	// Voice agent routing
	targetAgentID := c.AgentID()
	if c.config.VoiceAgentID != "" {
		for _, mi := range mediaList {
			if mi.Type == media.TypeAudio || mi.Type == media.TypeVoice {
				targetAgentID = c.config.VoiceAgentID
				slog.Debug("discord: routing voice inbound to speaking agent",
					"agent_id", targetAgentID, "media_type", mi.Type,
				)
				break
			}
		}
	}

	// Collect contact for processed messages (DM + group-mentioned).
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, senderName, m.Author.Username, peerKind, "user", "", "")
	}

	// Publish directly to bus (to preserve MediaFile MIME types)
	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: senderID,
		ChatID:   channelID,
		Content:  finalContent,
		Media:    mediaFiles,
		PeerKind: peerKind,
		UserID:   senderID,
		AgentID:  targetAgentID,
		TenantID: c.TenantID(),
		Metadata: metadata,
	})

	// Clear pending history after sending to agent.
	if peerKind == "group" {
		c.GroupHistory().Clear(channelID)
	}
}

// checkGroupPolicy evaluates the group policy for a sender, with pairing support.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, channelID string) bool {
	result := c.CheckGroupPolicy(ctx, senderID, channelID, c.config.GroupPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		groupSenderID := fmt.Sprintf("group:%s", channelID)
		c.sendPairingReply(ctx, groupSenderID, channelID)
		return false
	default:
		return false
	}
}

// checkDMPolicy evaluates the DM policy for a sender, handling pairing flow.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, channelID string) bool {
	result := c.CheckDMPolicy(ctx, senderID, c.config.DMPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, channelID)
		return false
	default:
		slog.Debug("discord DM rejected by policy", "sender_id", senderID, "policy", c.config.DMPolicy)
		return false
	}
}

// sendPairingReply sends a pairing code to the user via DM.
func (c *Channel) sendPairingReply(ctx context.Context, senderID, channelID string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), channelID, "default", nil)
	if err != nil {
		slog.Debug("discord pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Discord user ID: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	if _, err := c.session.ChannelMessageSend(channelID, replyText); err != nil {
		slog.Warn("failed to send discord pairing reply", "error", err)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("discord pairing reply sent", "sender_id", senderID, "code", code)
	}
}

// resolveDisplayName returns the best available display name for a Discord message author.
// Priority: server nickname > global display name > username.
func resolveDisplayName(m *discordgo.MessageCreate) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}
