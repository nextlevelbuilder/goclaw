package zalobot

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
)

type SendReplyFunc func(ctx context.Context, chatID, text string) error

type InboundHandler struct {
	base        *channels.BaseChannel
	bot         *BotUser
	cfg         config.ZaloBotConfig
	sendReplyFn SendReplyFunc
}

func NewInboundHandler(base *channels.BaseChannel, bot *BotUser, cfg config.ZaloBotConfig, sendReplyFn SendReplyFunc) *InboundHandler {
	return &InboundHandler{
		base:        base,
		bot:         bot,
		cfg:         cfg,
		sendReplyFn: sendReplyFn,
	}
}

func sliceContains(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

// StripBotMention checks for @BotName or @Username mentions in the text case-insensitively,
// strips the mention tag, and collapses extra whitespace.
func StripBotMention(text, botName, username string) (string, bool) {
	if text == "" {
		return "", false
	}

	matched := false
	targets := []string{}
	if botName != "" {
		targets = append(targets, "@"+botName)
	}
	if username != "" && !strings.EqualFold(username, botName) {
		targets = append(targets, "@"+username)
	}

	result := text
	for _, target := range targets {
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(target))
		if re.MatchString(result) {
			matched = true
			result = re.ReplaceAllString(result, "")
		}
	}

	// Clean up leftover spaces
	spaceRe := regexp.MustCompile(`\s+`)
	cleaned := strings.TrimSpace(spaceRe.ReplaceAllString(result, " "))

	return cleaned, matched
}

func (h *InboundHandler) sendPairingReply(ctx context.Context, senderID, chatID string) {
	ps := h.base.PairingService()
	if ps == nil {
		return
	}
	if !h.base.CanSendPairingNotif(senderID, 30*time.Second) {
		return
	}
	code, err := ps.RequestPairing(ctx, senderID, h.base.Name(), chatID, "default", nil)
	if err != nil {
		slog.Debug("zalobot pairing request failed", "sender_id", senderID, "error", err)
		return
	}
	replyText := h.base.SystemMessage("", systemmessages.KeyPairingAccountRequired, systemmessages.Vars{
		"platform":  "Zalo",
		"sender_id": senderID,
		"code":      code,
	})
	if h.sendReplyFn != nil {
		if err := h.sendReplyFn(ctx, chatID, replyText); err != nil {
			slog.Warn("failed to send zalobot pairing reply", "error", err)
		} else {
			h.base.MarkPairingNotifSent(senderID)
			slog.Info("zalobot pairing reply sent", "sender_id", senderID, "code", code)
		}
	}
}

func (h *InboundHandler) ProcessUpdate(ctx context.Context, u Update) error {
	msg := u.Message
	if msg == nil {
		return nil
	}

	senderID := msg.From.ID
	chatID := msg.Chat.ID
	senderName := msg.From.GetName()
	if senderName == "" {
		senderName = senderID
	}

	isGroup := strings.EqualFold(msg.Chat.ChatType, "GROUP")

	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = msg.Caption
	}

	var media []string
	photoURL := msg.PhotoURL
	if photoURL == "" {
		photoURL = msg.Photo
	}
	if photoURL != "" {
		media = append(media, photoURL)
	}

	if isGroup {
		// Group policy verification
		groupPolicy := h.cfg.GroupPolicy
		if groupPolicy == "" {
			groupPolicy = "open"
		}
		if groupPolicy == "disabled" {
			return nil
		}
		if groupPolicy == "allowlist" && !sliceContains(h.cfg.GroupAllowFrom, chatID) {
			return nil
		}

		requireMention := true
		if h.cfg.RequireMention != nil {
			requireMention = *h.cfg.RequireMention
		}

		botName := ""
		botUsername := ""
		if h.bot != nil {
			botName = h.bot.GetName()
			botUsername = h.bot.Username
		}

		cleaned, mentioned := StripBotMention(content, botName, botUsername)
		if requireMention && !mentioned {
			return nil
		}
		content = cleaned

		metadata := map[string]string{
			"message_id":   msg.MessageID,
			"platform":     channels.TypeZaloBot,
			"chat_type":    "group",
			"group_id":     chatID,
			"display_name": channels.SanitizeDisplayName(senderName),
			"peer_kind":    "group",
		}

		h.base.HandleAuthorizedMessage(senderID, chatID, content, media, metadata, "group")
		return nil
	}

	// Direct message policy
	dmPolicy := h.cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		return nil
	case "allowlist":
		if !sliceContains(h.cfg.AllowFrom, senderID) {
			return nil
		}
	case "pairing":
		policyRes := h.base.CheckDMPolicy(ctx, senderID, dmPolicy)
		switch policyRes {
		case channels.PolicyDeny:
			return nil
		case channels.PolicyNeedsPairing:
			h.sendPairingReply(ctx, senderID, chatID)
			return nil
		case channels.PolicyAllow:
			// Process message
		}
	case "open":
		// Allowed
	}

	metadata := map[string]string{
		"message_id":   msg.MessageID,
		"platform":     channels.TypeZaloBot,
		"chat_type":    "direct",
		"display_name": channels.SanitizeDisplayName(senderName),
		"peer_kind":    "direct",
	}

	h.base.HandleAuthorizedMessage(senderID, chatID, content, media, metadata, "direct")
	return nil
}
