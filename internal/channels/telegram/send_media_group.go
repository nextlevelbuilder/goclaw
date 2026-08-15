package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/mymmrac/telego"
)

func (c *Channel) sendTelegramMediaGroup(ctx context.Context, chatID telego.ChatID, items []telegramMediaSendItem, replyTo, threadID int) error {
	files := make([]*os.File, 0, len(items))
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()

	params := &telego.SendMediaGroupParams{
		ChatID: chatID,
		Media:  make([]telego.InputMedia, 0, len(items)),
	}
	if sendThreadID := resolveThreadIDForSend(threadID); sendThreadID > 0 {
		params.MessageThreadID = sendThreadID
	}
	if replyTo > 0 {
		params.ReplyParameters = &telego.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}

	for _, item := range items {
		file, err := os.Open(item.media.URL)
		if err != nil {
			return fmt.Errorf("open media %s: %w", item.media.URL, err)
		}
		files = append(files, file)
		params.Media = append(params.Media, inputMediaForTelegramGroupItem(item, file))
	}

	reset := func() {
		for _, file := range files {
			_, _ = file.Seek(0, 0)
		}
	}
	err := c.retrySend(ctx, "sendMediaGroup", reset, func(ctx context.Context) error {
		_, e := c.bot.SendMediaGroup(ctx, params)
		return e
	})
	if err != nil && parseErrRe.MatchString(err.Error()) {
		stripTelegramGroupCaptions(params.Media)
		reset()
		_, err = c.bot.SendMediaGroup(ctx, params)
	}
	if err != nil && params.MessageThreadID != 0 && threadNotFoundRe.MatchString(err.Error()) {
		slog.Warn("sendMediaGroup: thread not found, retrying without thread", "thread_id", params.MessageThreadID)
		params.MessageThreadID = 0
		reset()
		_, err = c.bot.SendMediaGroup(ctx, params)
	}
	return err
}

func inputMediaForTelegramGroupItem(item telegramMediaSendItem, file *os.File) telego.InputMedia {
	input := telego.InputFile{File: file}
	switch item.kind {
	case telegramMediaPhoto:
		media := &telego.InputMediaPhoto{Type: telego.MediaTypePhoto, Media: input, Caption: item.caption}
		if item.caption != "" {
			media.ParseMode = telego.ModeHTML
		}
		return media
	case telegramMediaVideo:
		media := &telego.InputMediaVideo{Type: telego.MediaTypeVideo, Media: input, Caption: item.caption}
		if item.caption != "" {
			media.ParseMode = telego.ModeHTML
		}
		return media
	case telegramMediaAudio:
		media := &telego.InputMediaAudio{Type: telego.MediaTypeAudio, Media: input, Caption: item.caption}
		if item.caption != "" {
			media.ParseMode = telego.ModeHTML
		}
		return media
	default:
		media := &telego.InputMediaDocument{Type: telego.MediaTypeDocument, Media: input, Caption: item.caption}
		if item.caption != "" {
			media.ParseMode = telego.ModeHTML
		}
		return media
	}
}

func stripTelegramGroupCaptions(media []telego.InputMedia) {
	for _, item := range media {
		switch typed := item.(type) {
		case *telego.InputMediaPhoto:
			typed.ParseMode = ""
			typed.Caption = stripHTML(typed.Caption)
		case *telego.InputMediaVideo:
			typed.ParseMode = ""
			typed.Caption = stripHTML(typed.Caption)
		case *telego.InputMediaAudio:
			typed.ParseMode = ""
			typed.Caption = stripHTML(typed.Caption)
		case *telego.InputMediaDocument:
			typed.ParseMode = ""
			typed.Caption = stripHTML(typed.Caption)
		}
	}
}

func (c *Channel) sendSingleTelegramMediaItem(ctx context.Context, chatID telego.ChatID, item telegramMediaSendItem, replyTo, threadID int) error {
	switch item.kind {
	case telegramMediaPhoto:
		return c.sendPhoto(ctx, chatID, item.media.URL, item.caption, replyTo, threadID)
	case telegramMediaVideo:
		return c.sendVideo(ctx, chatID, item.media.URL, item.caption, replyTo, threadID)
	case telegramMediaAudio:
		if item.asVoice {
			return c.sendVoice(ctx, chatID, item.media.URL, item.caption, replyTo, threadID)
		}
		return c.sendAudio(ctx, chatID, item.media.URL, item.caption, replyTo, threadID)
	default:
		return c.sendDocument(ctx, chatID, item.media.URL, item.caption, replyTo, threadID)
	}
}

func (c *Channel) sendTelegramMediaFollowUp(ctx context.Context, chatID int64, text string, threadID int) error {
	if text == "" {
		return nil
	}
	for _, chunk := range chunkHTML(text, telegramMaxMessageLen) {
		if err := c.sendHTML(ctx, chatID, chunk, 0, threadID); err != nil {
			return err
		}
	}
	return nil
}
