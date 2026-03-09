package slack

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func (c *Channel) handleEventsAPI(evt socketmode.Event) {
	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	// Ack immediately (Slack requires ack within ~3s)
	c.sm.Ack(*evt.Request)

	switch ev := eventsAPI.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		c.handleMessage(ev)
	case *slackevents.AppMentionEvent:
		c.handleAppMention(ev)
	}
}

func (c *Channel) handleMessage(ev *slackevents.MessageEvent) {
	// For message_changed: extract user/text from the nested Message field.
	// Only process if the edit introduces a new @bot mention.
	if ev.SubType == "message_changed" {
		if ev.Message == nil {
			return
		}
		// Skip bot's own edits or messages without a user
		if ev.Message.User == c.botUserID || ev.Message.User == "" {
			return
		}
		// Only process if the edited message mentions the bot
		if !c.isBotMentioned(ev.Message.Text) {
			return
		}
		// Check that the previous version did NOT mention the bot (newly added mention)
		if ev.PreviousMessage != nil && c.isBotMentioned(ev.PreviousMessage.Text) {
			return
		}
		// Promote nested fields to top-level for unified processing below
		ev.User = ev.Message.User
		ev.Text = ev.Message.Text
		ev.TimeStamp = ev.Message.Timestamp
		ev.ThreadTimeStamp = ev.Message.ThreadTimestamp
	}

	if ev.User == c.botUserID || ev.User == "" {
		return
	}

	// Skip message subtypes (edits, deletes, bot_message, joins, etc.)
	// Allow "file_share" and "message_changed" subtypes.
	if ev.SubType != "" && ev.SubType != "file_share" && ev.SubType != "message_changed" {
		return
	}

	// Explicit dedup: prevent duplicate processing on Socket Mode reconnect
	dedupKey := ev.Channel + ":" + ev.TimeStamp
	if _, loaded := c.dedup.LoadOrStore(dedupKey, time.Now()); loaded {
		return
	}

	senderID := ev.User
	channelID := ev.Channel
	content := ev.Text

	isDM := ev.ChannelType == "im"
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	// Resolve display name; strip "|" to prevent compound senderID corruption
	displayName := strings.ReplaceAll(c.resolveDisplayName(senderID), "|", "_")
	compoundSenderID := fmt.Sprintf("%s|%s", senderID, displayName)

	// Policy check
	if isDM {
		if !c.checkDMPolicy(senderID, channelID) {
			return
		}
	} else {
		if !c.checkGroupPolicy(senderID, channelID) {
			return
		}
	}

	// For DMs, apply global allowlist filter (allow_from contains user IDs).
	// For groups, skip — group policy already handles channel/user filtering.
	if isDM && !c.IsAllowed(compoundSenderID) {
		slog.Debug("slack message rejected by allowlist",
			"user_id", senderID, "display_name", displayName)
		return
	}

	// Process file attachments from Slack message
	var mediaPaths []string
	if ev.Message != nil && len(ev.Message.Files) > 0 {
		items, docContent := c.resolveMedia(ev.Message.Files)

		for _, item := range items {
			if item.FilePath != "" {
				mediaPaths = append(mediaPaths, item.FilePath)
			}
		}

		// Prepend media tags and document content to message text
		mediaTags := buildMediaTags(items)
		if mediaTags != "" {
			if content != "" {
				content = mediaTags + "\n\n" + content
			} else {
				content = mediaTags
			}
		}
		if docContent != "" {
			if content != "" {
				content = content + "\n\n" + docContent
			} else {
				content = docContent
			}
		}
	}

	if content == "" {
		return
	}

	// Determine local_key and thread context
	localKey := channelID
	threadTS := ev.ThreadTimeStamp
	if !isDM && threadTS != "" {
		localKey = fmt.Sprintf("%s:thread:%s", channelID, threadTS)
	}

	// Mention gating in groups (with thread participation cache)
	if !isDM && c.requireMention {
		mentioned := c.isBotMentioned(content)

		// Thread participation cache: auto-reply in threads where bot previously participated
		if !mentioned && threadTS != "" && c.threadTTL > 0 {
			participKey := channelID + ":particip:" + threadTS
			if lastReply, ok := c.threadParticip.Load(participKey); ok {
				if time.Since(lastReply.(time.Time)) < c.threadTTL {
					mentioned = true
					slog.Debug("slack: auto-reply in participated thread",
						"channel_id", channelID, "thread_ts", threadTS)
				} else {
					c.threadParticip.Delete(participKey)
				}
			}
		}

		if !mentioned {
			c.groupHistory.Record(localKey, channels.HistoryEntry{
				Sender:    displayName,
				Body:      content,
				Timestamp: time.Now(),
				MessageID: ev.TimeStamp,
			}, c.historyLimit)

			slog.Debug("slack group message recorded (no mention)",
				"channel_id", channelID, "user", displayName)
			return
		}
	}

	content = c.stripBotMention(content)
	content = strings.TrimSpace(content)

	slog.Debug("slack message received",
		"sender_id", senderID, "channel_id", channelID,
		"is_dm", isDM, "preview", channels.Truncate(content, 50))

	// Send "Thinking..." placeholder
	replyThreadTS := threadTS
	if !isDM && replyThreadTS == "" {
		replyThreadTS = ev.TimeStamp // start thread from the triggering message
	}

	placeholderOpts := []slackapi.MsgOption{
		slackapi.MsgOptionText("Thinking...", false),
	}
	if replyThreadTS != "" {
		placeholderOpts = append(placeholderOpts, slackapi.MsgOptionTS(replyThreadTS))
	}

	_, placeholderTS, err := c.api.PostMessage(channelID, placeholderOpts...)
	if err == nil {
		c.placeholders.Store(localKey, placeholderTS)
	}

	// Build final content with group history context
	finalContent := content
	if peerKind == "group" {
		annotated := fmt.Sprintf("[From: %s]\n%s", displayName, content)
		if c.historyLimit > 0 {
			finalContent = c.groupHistory.BuildContext(localKey, annotated, c.historyLimit)
		} else {
			finalContent = annotated
		}
	}

	metadata := map[string]string{
		"message_id":      ev.TimeStamp,
		"user_id":         senderID,
		"username":        displayName,
		"channel_id":      channelID,
		"is_dm":           fmt.Sprintf("%t", isDM),
		"local_key":       localKey,
		"placeholder_key": localKey,
	}
	if replyThreadTS != "" {
		metadata["message_thread_id"] = replyThreadTS
	}

	// Message debounce: batch rapid messages per-thread
	if c.debounceDelay > 0 {
		if c.debounceMessage(localKey, compoundSenderID, channelID, finalContent, mediaPaths, metadata, peerKind) {
			// Record thread participation even when debounced
			if peerKind == "group" && replyThreadTS != "" {
				participKey := channelID + ":particip:" + replyThreadTS
				c.threadParticip.Store(participKey, time.Now())
			}
			return
		}
	}

	c.HandleMessage(compoundSenderID, channelID, finalContent, mediaPaths, metadata, peerKind)

	// Record thread participation for auto-reply cache
	if peerKind == "group" {
		if replyThreadTS != "" {
			participKey := channelID + ":particip:" + replyThreadTS
			c.threadParticip.Store(participKey, time.Now())
		}
		c.groupHistory.Clear(localKey)
	}
}

func (c *Channel) handleAppMention(ev *slackevents.AppMentionEvent) {
	if ev.User == c.botUserID || ev.User == "" {
		return
	}

	// Dedup: app_mention may arrive alongside a message event
	dedupKey := ev.Channel + ":" + ev.TimeStamp
	if _, loaded := c.dedup.LoadOrStore(dedupKey, time.Now()); loaded {
		return
	}

	// If requireMention is false, message handler already processes all channel messages
	if !c.requireMention {
		return
	}

	senderID := ev.User
	channelID := ev.Channel
	content := ev.Text

	displayName := strings.ReplaceAll(c.resolveDisplayName(senderID), "|", "_")
	compoundSenderID := fmt.Sprintf("%s|%s", senderID, displayName)

	if !c.checkGroupPolicy(senderID, channelID) {
		return
	}

	content = c.stripBotMention(content)
	content = strings.TrimSpace(content)

	if content == "" {
		return
	}

	localKey := channelID
	threadTS := ev.ThreadTimeStamp
	if threadTS != "" {
		localKey = fmt.Sprintf("%s:thread:%s", channelID, threadTS)
	}

	slog.Debug("slack app_mention received",
		"sender_id", senderID, "channel_id", channelID,
		"preview", channels.Truncate(content, 50))

	replyThreadTS := threadTS
	if replyThreadTS == "" {
		replyThreadTS = ev.TimeStamp
	}

	placeholderOpts := []slackapi.MsgOption{
		slackapi.MsgOptionText("Thinking...", false),
	}
	if replyThreadTS != "" {
		placeholderOpts = append(placeholderOpts, slackapi.MsgOptionTS(replyThreadTS))
	}

	_, placeholderTS, err := c.api.PostMessage(channelID, placeholderOpts...)
	if err == nil {
		c.placeholders.Store(localKey, placeholderTS)
	}

	annotated := fmt.Sprintf("[From: %s]\n%s", displayName, content)
	finalContent := annotated
	if c.historyLimit > 0 {
		finalContent = c.groupHistory.BuildContext(localKey, annotated, c.historyLimit)
	}

	metadata := map[string]string{
		"message_id":      ev.TimeStamp,
		"user_id":         senderID,
		"username":        displayName,
		"channel_id":      channelID,
		"is_dm":           "false",
		"local_key":       localKey,
		"placeholder_key": localKey,
	}
	if replyThreadTS != "" {
		metadata["message_thread_id"] = replyThreadTS
	}

	c.HandleMessage(compoundSenderID, channelID, finalContent, nil, metadata, "group")

	// Record thread participation
	if replyThreadTS != "" {
		participKey := channelID + ":particip:" + replyThreadTS
		c.threadParticip.Store(participKey, time.Now())
	}

	c.groupHistory.Clear(localKey)
}

// isBotMentioned checks if the message text contains <@botUserID>.
func (c *Channel) isBotMentioned(text string) bool {
	return strings.Contains(text, "<@"+c.botUserID+">")
}

// stripBotMention removes <@botUserID> from message text.
func (c *Channel) stripBotMention(text string) string {
	return strings.ReplaceAll(text, "<@"+c.botUserID+">", "")
}

// --- Message debounce/batching ---

type debounceEntry struct {
	timer     *time.Timer
	messages  []string
	mu        sync.Mutex
	senderID  string
	channelID string
	media     []string
	metadata  map[string]string
	peerKind  string
}

// debounceMessage batches rapid messages. Returns true if message was debounced.
func (c *Channel) debounceMessage(localKey, senderID, channelID, content string, media []string, metadata map[string]string, peerKind string) bool {
	c.debounceMu.Lock()
	entry, loaded := c.debounceTimers[localKey]
	if !loaded {
		entry = &debounceEntry{
			senderID:  senderID,
			channelID: channelID,
			media:     media,
			metadata:  metadata,
			peerKind:  peerKind,
		}
		c.debounceTimers[localKey] = entry
	}
	c.debounceMu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.messages = append(entry.messages, content)
	if loaded {
		// Only append media for subsequent messages; first message's media is set in constructor.
		entry.media = append(entry.media, media...)
	}
	entry.metadata = metadata // use latest message's metadata

	if !loaded {
		entry.timer = time.AfterFunc(c.debounceDelay, func() {
			c.flushDebounce(localKey)
		})
		return true
	}

	if entry.timer != nil {
		entry.timer.Reset(c.debounceDelay)
	}
	return true
}

func (c *Channel) flushDebounce(localKey string) {
	c.debounceMu.Lock()
	entry, ok := c.debounceTimers[localKey]
	if ok {
		delete(c.debounceTimers, localKey)
	}
	c.debounceMu.Unlock()

	if !ok {
		return
	}

	entry.mu.Lock()
	combined := strings.Join(entry.messages, "\n")
	entry.mu.Unlock()

	c.HandleMessage(entry.senderID, entry.channelID, combined, entry.media, entry.metadata, entry.peerKind)

	if entry.peerKind == "group" {
		c.groupHistory.Clear(localKey)
	}
}

// --- Policy checks ---

func (c *Channel) checkDMPolicy(senderID, channelID string) bool {
	dmPolicy := c.config.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		return false
	case "open":
		return true
	case "allowlist":
		return c.HasAllowList() && c.IsAllowed(senderID)
	default: // "pairing"
		if c.pairingService != nil && c.pairingService.IsPaired(senderID, c.Name()) {
			return true
		}
		if c.HasAllowList() && c.IsAllowed(senderID) {
			return true
		}
		c.sendPairingReply(senderID, channelID)
		return false
	}
}

func (c *Channel) checkGroupPolicy(senderID, channelID string) bool {
	groupPolicy := c.config.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		if !c.HasAllowList() {
			return false
		}
		// Allow if user ID or channel ID is in the allowlist
		return c.IsAllowed(senderID) || c.IsAllowed(channelID)
	case "pairing":
		if c.HasAllowList() && c.IsAllowed(senderID) {
			return true
		}
		if _, cached := c.approvedGroups.Load(channelID); cached {
			return true
		}
		groupSenderID := fmt.Sprintf("group:%s", channelID)
		if c.pairingService != nil && c.pairingService.IsPaired(groupSenderID, c.Name()) {
			c.approvedGroups.Store(channelID, true)
			return true
		}
		c.sendPairingReply(groupSenderID, channelID)
		return false
	default: // "open"
		return true
	}
}

func (c *Channel) sendPairingReply(senderID, channelID string) {
	if c.pairingService == nil {
		return
	}

	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounceTime {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(senderID, c.Name(), channelID, "default", nil)
	if err != nil {
		slog.Warn("slack: failed to request pairing code", "error", err)
		return
	}

	// Security: do not expose pairing code in group channels (visible to all members).
	// Instead, direct admin to CLI or web UI where pending codes are listed.
	var msg string
	if strings.HasPrefix(senderID, "group:") {
		msg = fmt.Sprintf("This channel is not authorized to use this bot.\n\n"+
			"An admin can approve via CLI:\n  goclaw pairing approve %s\n\n"+
			"Or approve via the GoClaw web UI (Pairing section).", code)
	} else {
		msg = fmt.Sprintf("GoClaw: access not configured.\n\nYour Slack user ID: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
			senderID, code, code)
	}
	if _, _, err := c.api.PostMessage(channelID, slackapi.MsgOptionText(msg, false)); err != nil {
		slog.Warn("slack: failed to send pairing reply",
			"channel_id", channelID, "error", err)
	}
	c.pairingDebounce.Store(senderID, time.Now())
}

// --- File download (SSRF-protected) ---

var slackDownloadAllowlist = []string{
	".slack.com",
	".slack-edge.com",
	".slack-files.com",
}

func isAllowedDownloadHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range slackDownloadAllowlist {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func (c *Channel) downloadFile(name, urlPrivate, urlPrivateDownload string, maxBytes int64) (string, error) {
	downloadURL := urlPrivateDownload
	if downloadURL == "" {
		downloadURL = urlPrivate
	}
	if downloadURL == "" {
		return "", fmt.Errorf("no download URL for file %s", name)
	}

	if !isAllowedDownloadHost(downloadURL) {
		return "", fmt.Errorf("security: download URL hostname not in Slack allowlist: %s", downloadURL)
	}

	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".dat"
	}
	tmpFile, err := os.CreateTemp("", "slack-file-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Authorization") // strip auth on redirect (CDN presigned URL)
			if req.URL.Scheme != "https" {
				return fmt.Errorf("security: redirect to non-HTTPS URL blocked: %s", req.URL)
			}
			// Only allow redirects to known Slack CDN domains to prevent SSRF.
			host := req.URL.Hostname()
			if !isAllowedSlackHost(host) {
				return fmt.Errorf("security: redirect to untrusted host blocked: %s", host)
			}
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.config.BotToken)

	resp, err := client.Do(req)
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxBytes)); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// allowedSlackHosts contains trusted Slack CDN domains for redirect validation.
var allowedSlackHosts = []string{
	".slack-edge.com",
	".slack.com",
	"files.slack.com",
}

// isAllowedSlackHost checks if a hostname belongs to a known Slack CDN domain.
func isAllowedSlackHost(host string) bool {
	for _, suffix := range allowedSlackHosts {
		if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// --- File upload (v2 3-step API) ---

func (c *Channel) uploadFile(channelID, threadTS string, media bus.MediaAttachment) error {
	filePath := media.URL
	fileName := filepath.Base(filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	params := slackapi.UploadFileParameters{
		Filename:       fileName,
		FileSize:       len(data),
		Reader:         bytes.NewReader(data),
		Title:          fileName,
		Channel:        channelID,
		ThreadTimestamp: threadTS,
	}

	_, err = c.api.UploadFile(params)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	return nil
}
