package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

const (
	discordThreadHistoryLimit       = 25
	discordThreadHistoryMaxBytes    = 5 * 1024 * 1024
	discordThreadHistoryMaxFiles    = 15
	discordThreadHistoryTimeout     = 30 * time.Second
	discordThreadHistoryContextHead = "[Discord thread messages before this mention - for context]"
)

type threadBackfillResult struct {
	Context string
	Media   []bus.MediaFile
}

type threadHistoryAttachment struct {
	File bus.MediaFile
	Tag  string
}

func (c *Channel) backfillThreadHistory(ctx context.Context, m *discordgo.MessageCreate, maxBytes int64) threadBackfillResult {
	if m == nil || m.Message == nil || m.ChannelID == "" || m.ID == "" {
		return threadBackfillResult{}
	}
	if !c.isThreadChannel(ctx, m.ChannelID) {
		return threadBackfillResult{}
	}
	ctx, cancel := context.WithTimeout(ctx, discordThreadHistoryTimeout)
	defer cancel()

	messages, err := c.session.ChannelMessages(
		m.ChannelID,
		discordThreadHistoryLimit,
		m.ID,
		"",
		"",
		discordgo.WithContext(ctx),
	)
	if err != nil {
		slog.Warn("discord: thread history backfill failed", "channel_id", m.ChannelID, "message_id", m.ID, "error", err)
		return threadBackfillResult{}
	}
	if len(messages) == 0 {
		return threadBackfillResult{}
	}
	if maxBytes <= 0 || maxBytes > discordThreadHistoryMaxBytes {
		maxBytes = discordThreadHistoryMaxBytes
	}

	lines := []string{discordThreadHistoryContextHead}
	var mediaFiles []bus.MediaFile
	for i := len(messages) - 1; i >= 0; i-- {
		hm := messages[i]
		if hm == nil || hm.ID == m.ID || hm.Author == nil || hm.Author.ID == c.botUserID {
			continue
		}
		body := strings.TrimSpace(hm.Content)
		if body != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", discordHistoryAuthorName(hm), channels.Truncate(body, 1000)))
		}
		for _, attachment := range c.resolveThreadHistoryAttachments(ctx, hm.Attachments, maxBytes, discordThreadHistoryMaxFiles-len(mediaFiles)) {
			if attachment.Tag != "" {
				lines = append(lines, attachment.Tag)
			}
			mediaFiles = append(mediaFiles, attachment.File)
		}
	}
	if len(lines) == 1 && len(mediaFiles) == 0 {
		return threadBackfillResult{}
	}
	var contextBlock string
	if len(lines) > 1 {
		contextBlock = strings.Join(lines, "\n")
	}
	return threadBackfillResult{Context: contextBlock, Media: mediaFiles}
}

func (c *Channel) isThreadChannel(ctx context.Context, channelID string) bool {
	if c.session == nil {
		return false
	}
	if c.session.State != nil {
		if ch, err := c.session.State.Channel(channelID); err == nil && ch != nil {
			return ch.IsThread()
		}
	}
	ch, err := c.session.Channel(channelID, discordgo.WithContext(ctx))
	if err != nil {
		slog.Warn("discord: thread channel lookup failed", "channel_id", channelID, "error", err)
		return false
	}
	return ch != nil && ch.IsThread()
}

func (c *Channel) resolveThreadHistoryAttachments(ctx context.Context, attachments []*discordgo.MessageAttachment, maxBytes int64, remaining int) []threadHistoryAttachment {
	if len(attachments) == 0 || remaining <= 0 {
		return nil
	}
	var mediaFiles []threadHistoryAttachment
	for _, att := range attachments {
		if att == nil || att.URL == "" || remaining <= 0 {
			continue
		}
		if int64(att.Size) > maxBytes {
			slog.Debug("discord: thread history attachment too large, skipping", "filename", att.Filename, "size", att.Size, "max", maxBytes)
			continue
		}
		filePath, err := downloadFromURLContext(ctx, att.URL, maxBytes)
		if err != nil {
			slog.Warn("discord: thread history attachment download failed", "filename", att.Filename, "error", err)
			continue
		}
		ct := att.ContentType
		if ct == "" {
			ct = media.DetectMIMEType(att.Filename)
		}
		info := media.MediaInfo{
			Type:        classifyMediaType(ct, att.Filename),
			FilePath:    filePath,
			SourceURL:   att.URL,
			ContentType: ct,
			FileName:    att.Filename,
			FileSize:    int64(att.Size),
		}
		mediaFiles = append(mediaFiles, threadHistoryAttachment{
			File: bus.MediaFile{
				Path:     filePath,
				MimeType: ct,
				Filename: att.Filename,
			},
			Tag: media.BuildMediaTags([]media.MediaInfo{info}),
		})
		remaining--
	}
	return mediaFiles
}

func discordHistoryAuthorName(m *discordgo.Message) string {
	if m == nil || m.Author == nil {
		return "unknown"
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}
