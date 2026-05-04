package oa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// isZaloSupportedFileMIME: /v2.0/oa/upload/file accepts PDF/DOC/DOCX only;
// other types are silently rejected by Zalo.
func isZaloSupportedFileMIME(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return true
	}
	return false
}

// maxTextLength is Zalo's per-message cap; longer payloads error -210.
// Same value across zalo_bot / zalo_personal / zalo_oa.
const maxTextLength = 2000

// SendText splits replies via channels.ChunkMarkdown so >2000-char
// messages reach the user as multiple ordered sends. Returns the final
// upstream message_id. quoteID, when non-empty, is sent as Zalo's
// message.quote_message_id on the FIRST chunk only — continuation chunks
// ride unquoted.
func (c *Channel) SendText(ctx context.Context, userID, text, quoteID string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	parts := channels.ChunkMarkdown(text, maxTextLength)
	if len(parts) == 0 {
		return "", nil
	}
	var lastMID string
	for i, part := range parts {
		q := ""
		if i == 0 {
			q = quoteID
		}
		mid, err := c.postCSWithQuoteFallback(ctx, userID, part, q)
		if err != nil {
			return lastMID, fmt.Errorf("zalo_oa.sendtext part %d/%d: %w", i+1, len(parts), err)
		}
		lastMID = mid
		slog.Info("zalo_oa.sent", "type", "text", "message_id", mid, "oa_id", c.creds().OAID,
			"part", i+1, "total_parts", len(parts), "quoted", q != "")
	}
	return lastMID, nil
}

// postCSWithQuoteFallback posts a text body and, on FamilyPayload errors
// when a quote was set, retries once without the quote field. Covers the
// expired/deleted source-message case without masking other error families.
func (c *Channel) postCSWithQuoteFallback(ctx context.Context, userID, text, quoteID string) (string, error) {
	mid, err := c.post(ctx, pathSendMessage, buildTextBody(userID, text, quoteID))
	if err == nil || quoteID == "" {
		return mid, err
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && Classify(apiErr.Code).Family == FamilyPayload {
		slog.Warn("zalo_oa.send.quote_dropped_payload_error",
			"oa_id", c.creds().OAID,
			"user_id", userID,
			"quote_message_id", quoteID,
			"zalo_code", apiErr.Code,
			"zalo_msg", apiErr.Message,
			"hint", "quoted message likely expired/deleted; retrying without quote")
		return c.post(ctx, pathSendMessage, buildTextBody(userID, text, ""))
	}
	return mid, err
}

// SendImage uploads + sends an image. mime must be image/jpeg or image/png
// (drives the multipart filename extension Zalo validates against).
// Image attachments require the template/media payload shape; the simpler
// {"type":"image","payload":{"attachment_id"}} returns -201.
func (c *Channel) SendImage(ctx context.Context, userID string, data []byte, mime string) (string, error) {
	tok, err := c.uploadImage(ctx, data, mime)
	if err != nil {
		return "", err
	}
	body := buildMediaAttachmentBody(userID, "image", tok)
	mid, err := c.post(ctx, pathSendMessage, body)
	if err == nil {
		slog.Info("zalo_oa.sent", "type", "image", "message_id", mid, "oa_id", c.creds().OAID)
	}
	return mid, err
}

// SendGIF uploads + sends a GIF via /upload/gif (5MB cap, enforced by caller).
func (c *Channel) SendGIF(ctx context.Context, userID string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("zalo_oa: refusing to send empty gif")
	}
	tok, err := c.uploadGIF(ctx, data)
	if err != nil {
		return "", err
	}
	body := buildMediaAttachmentBody(userID, "gif", tok)
	mid, err := c.post(ctx, pathSendMessage, body)
	if err == nil {
		slog.Info("zalo_oa.sent", "type", "gif", "message_id", mid, "oa_id", c.creds().OAID)
	}
	return mid, err
}

// Payload builders for /v3.0/oa/message/cs. Images + gifs use template/media;
// files use plain type=file; text has no attachment wrapper.

func buildTextBody(userID, text, quoteMessageID string) map[string]any {
	msg := map[string]any{"text": text}
	if quoteMessageID != "" {
		msg["quote_message_id"] = quoteMessageID
	}
	return map[string]any{
		"recipient": map[string]any{"user_id": userID},
		"message":   msg,
	}
}

// buildMediaAttachmentBody is the template/media shape for image+gif sends.
// mediaType is "image" or "gif".
func buildMediaAttachmentBody(userID, mediaType, attachmentID string) map[string]any {
	return map[string]any{
		"recipient": map[string]any{"user_id": userID},
		"message": map[string]any{
			"attachment": map[string]any{
				"type": "template",
				"payload": map[string]any{
					"template_type": "media",
					"elements": []map[string]any{{
						"media_type":    mediaType,
						"attachment_id": attachmentID,
					}},
				},
			},
		},
	}
}

// buildFileAttachmentBody is the plain type=file shape; files do NOT use
// the template/media wrapper.
func buildFileAttachmentBody(userID, attachmentID string) map[string]any {
	return map[string]any{
		"recipient": map[string]any{"user_id": userID},
		"message": map[string]any{
			"attachment": map[string]any{
				"type":    "file",
				"payload": map[string]any{"attachment_id": attachmentID},
			},
		},
	}
}

// SendFile uploads + sends a file. filename rides in the multipart
// "filename" field so Zalo preserves it for the recipient. MIME gating
// lives at the caller (channel.go dispatch).
func (c *Channel) SendFile(ctx context.Context, userID string, data []byte, filename string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("zalo_oa: refusing to send empty/zero-byte file %q", filename)
	}
	tok, err := c.uploadFile(ctx, data, filename)
	if err != nil {
		return "", err
	}
	mid, err := c.post(ctx, pathSendMessage, buildFileAttachmentBody(userID, tok))
	if err == nil {
		slog.Info("zalo_oa.sent", "type", "file", "message_id", mid, "oa_id", c.creds().OAID)
	}
	return mid, err
}

// post wraps apiPost with retry-once-on-auth: the first auth error triggers
// ForceRefresh + one retry. Other errors return immediately and flip health
// to Failed/Auth so the dashboard surfaces the reauth prompt promptly.
func (c *Channel) post(ctx context.Context, path string, body any) (string, error) {
	var lastErr error
	for attempt := range 2 {
		tok, err := c.tokens.Access(ctx)
		if err != nil {
			c.markAuthFailedIfNeeded(err)
			return "", err
		}
		raw, err := c.client.apiPost(ctx, path, body, tok)
		if err == nil {
			return parseMessageResponse(raw)
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.isAuth() && attempt == 0 {
			c.tokens.ForceRefresh()
			lastErr = err
			continue
		}
		c.markAuthFailedIfNeeded(err)
		return "", err
	}
	return "", lastErr
}

// parseMessageResponse pulls message_id from the standard envelope:
// {"error":0,"data":{"message_id":"...","recipient_id":"..."}}
func parseMessageResponse(raw json.RawMessage) (string, error) {
	var env struct {
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("zalo_oa: decode message response: %w", err)
	}
	return env.Data.MessageID, nil
}
