package line

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/line/line-bot-sdk-go/v7/linebot"
)

// hookContext returns c.hookCtx or context.Background() if the channel
// has not been Start'd yet. The fallback exists so unit tests that
// exercise fan-out helpers directly without calling Start still work.
func (c *Channel) hookContext() context.Context {
	if c.hookCtx != nil {
		return c.hookCtx
	}
	return context.Background()
}

// fanOutAudio delivers an AudioEvent to every registered hook in a goroutine
// so one slow hook cannot block others. Errors are logged but do not retry.
// Goroutines are tracked via hookWG so Stop can wait for them to drain.
func (c *Channel) fanOutAudio(ev AudioEvent) {
	ctx := c.hookContext()
	for _, h := range c.hooks {
		h := h
		c.hookWG.Add(1)
		go func() {
			defer c.hookWG.Done()
			if err := h.OnAudio(ctx, ev); err != nil {
				slog.Error("LINE: hook OnAudio failed", "err", err)
			}
		}()
	}
}

// fanOutText delivers a TextEvent to every registered hook.
func (c *Channel) fanOutText(ev TextEvent) {
	ctx := c.hookContext()
	for _, h := range c.hooks {
		h := h
		c.hookWG.Add(1)
		go func() {
			defer c.hookWG.Done()
			if err := h.OnText(ctx, ev); err != nil {
				slog.Error("LINE: hook OnText failed", "err", err)
			}
		}()
	}
}

// fanOutPostback delivers a PostbackEvent to every registered hook.
func (c *Channel) fanOutPostback(ev PostbackEvent) {
	ctx := c.hookContext()
	for _, h := range c.hooks {
		h := h
		c.hookWG.Add(1)
		go func() {
			defer c.hookWG.Done()
			if err := h.OnPostback(ctx, ev); err != nil {
				slog.Error("LINE: hook OnPostback failed", "err", err)
			}
		}()
	}
}

// imageExtForContentType maps an HTTP Content-Type header from a LINE Content
// API response to the file extension we save the image with. Falls back to
// .jpg for missing or unknown content types so vision providers always get
// a recognisable extension.
func imageExtForContentType(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	}
	return ".jpg"
}

// classifySource derives (userID, chatID, peerKind) from a LINE event source.
// peerKind is "direct" for 1:1 chats and "group" for both group and room sources
// (LINE distinguishes Group vs Room but downstream routing treats both as group).
// Returns empty peerKind for unrecognised source types so callers can drop.
func classifySource(src *linebot.EventSource) (userID, chatID, peerKind string) {
	if src == nil {
		return "", "", ""
	}
	switch src.Type {
	case linebot.EventSourceTypeUser:
		return src.UserID, src.UserID, "direct"
	case linebot.EventSourceTypeGroup:
		return src.UserID, src.GroupID, "group"
	case linebot.EventSourceTypeRoom:
		return src.UserID, src.RoomID, "group"
	}
	return "", "", ""
}

// handleEvent dispatches a single LINE webhook event.
func (c *Channel) handleEvent(event *linebot.Event) {
	// Postback events are delivered to every registered MessageHook.
	// The LINE channel itself has no business-logic opinion on postbacks;
	// hooks interpret the `data` payload and route to their own state
	// machine.
	if event.Type == linebot.EventTypePostback {
		var uid, cid string
		switch event.Source.Type {
		case linebot.EventSourceTypeUser:
			uid, cid = event.Source.UserID, event.Source.UserID
		case linebot.EventSourceTypeGroup:
			uid, cid = event.Source.UserID, event.Source.GroupID
		case linebot.EventSourceTypeRoom:
			uid, cid = event.Source.UserID, event.Source.RoomID
		}
		c.fanOutPostback(PostbackEvent{
			UserID:     uid,
			ChatID:     cid,
			Data:       event.Postback.Data,
			ReplyToken: event.ReplyToken,
		})
		return
	}
	if event.Type != linebot.EventTypeMessage {
		return
	}

	userID, chatID, peerKind := classifySource(event.Source)
	if peerKind == "" {
		return
	}

	senderID := "line:" + userID

	// Policy check.
	if !c.CheckPolicy(peerKind, c.cfg.DMPolicy, c.cfg.GroupPolicy, senderID) {
		slog.Debug("LINE: message rejected by policy", "sender", senderID, "peerKind", peerKind)
		return
	}

	// Send loading animation (best-effort).
	go c.sendLoadingAnimation(chatID)

	// Cache reply token.
	c.replyTokens.Store(chatID, replyTokenEntry{
		token:      event.ReplyToken,
		receivedAt: time.Now(),
	})

	var text string
	var mediaFiles []string

	switch msg := event.Message.(type) {
	case *linebot.TextMessage:
		text = msg.Text
		// Fan out to hooks for any subscribers. The agent still sees
		// the original text via HandleMessage below — hooks run in
		// parallel to the agent path.
		c.fanOutText(TextEvent{
			UserID:     userID,
			ChatID:     chatID,
			Text:       msg.Text,
			ReplyToken: event.ReplyToken,
		})
	case *linebot.ImageMessage:
		path, err := c.downloadContent(msg.ID)
		if err != nil {
			slog.Error("LINE: failed to download image", "err", err)
			return
		}
		mediaFiles = append(mediaFiles, path)
	case *linebot.AudioMessage:
		// Audio messages are generic LINE content — download to a tmp
		// file, then fan out to hooks with TempPath + ContentType set.
		// Hooks take ownership of the tmp file (move/delete). The channel
		// itself has no opinion on what to do with audio.
		tmpPath, contentType, err := c.downloadAudioContent(msg.ID)
		if err != nil {
			slog.Error("LINE: failed to download audio", "err", err, "message_id", msg.ID)
			return
		}
		c.fanOutAudio(AudioEvent{
			UserID:      userID,
			ChatID:      chatID,
			MessageID:   msg.ID,
			ContentType: contentType,
			TempPath:    tmpPath,
		})
		return
	default:
		// Unsupported message type — ignore.
		return
	}

	metadata := map[string]string{
		"reply_token": event.ReplyToken,
	}

	c.HandleMessage(senderID, chatID, text, mediaFiles, metadata, peerKind)
}

// downloadAudioContent fetches a LINE audio message body to a temp file and
// returns the file path plus the response Content-Type. The caller is
// responsible for moving / renaming the file out of the temp area.
func (c *Channel) downloadAudioContent(messageID string) (string, string, error) {
	resp, err := c.bot.GetMessageContent(messageID).Do()
	if err != nil {
		return "", "", fmt.Errorf("get audio content: %w", err)
	}
	defer resp.Content.Close()

	tmp, err := os.CreateTemp("", "line-audio-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, resp.Content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("write audio: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("close temp file: %w", err)
	}

	return tmp.Name(), resp.ContentType, nil
}

// downloadContent downloads message content to a temp file and returns the path.
func (c *Channel) downloadContent(messageID string) (string, error) {
	resp, err := c.bot.GetMessageContent(messageID).Do()
	if err != nil {
		return "", fmt.Errorf("get message content: %w", err)
	}
	defer resp.Content.Close()

	tmpFile, err := os.CreateTemp("", "line-media-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Content); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("write media: %w", err)
	}

	// Rename with proper extension based on content type.
	ext := imageExtForContentType(resp.ContentType)
	finalPath := tmpFile.Name() + ext
	if err := os.Rename(tmpFile.Name(), finalPath); err != nil {
		return tmpFile.Name(), nil // fallback to original name
	}
	return filepath.Clean(finalPath), nil
}

// sendLoadingAnimation sends a loading indicator to the chat via LINE API.
func (c *Channel) sendLoadingAnimation(chatID string) {
	body, _ := json.Marshal(map[string]interface{}{
		"chatId":         chatID,
		"loadingSeconds": loadingSeconds,
	})

	req, err := http.NewRequest(http.MethodPost, loadingAPIURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.ChannelAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("LINE: loading animation request failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn("LINE: loading animation error", "status", resp.StatusCode, "body", string(respBody))
	}
}
