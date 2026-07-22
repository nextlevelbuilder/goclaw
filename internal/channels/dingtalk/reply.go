package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Message types accepted by the session webhook and by msgKey mapping.
const (
	msgTypeText     = "text"
	msgTypeMarkdown = "markdown"
)

// replyWebhook posts a reply to a message's session webhook.
//
// The session webhook is a pre-signed, per-message URL: it carries its own
// authorization, so no access token is attached. (The upstream connector sends
// one anyway; it is not required.) It expires — callers must consult
// webhookUsable before choosing this path.
//
// Media cannot be sent this way; media always goes through the proactive API.
func (c *Channel) replyWebhook(ctx context.Context, webhook, msgType, text string) error {
	if webhook == "" {
		return fmt.Errorf("dingtalk: no session webhook for reply")
	}

	var payload map[string]any
	switch msgType {
	case msgTypeMarkdown:
		payload = map[string]any{
			"msgtype":  msgTypeMarkdown,
			"markdown": map[string]string{"title": markdownTitle(text), "text": text},
		}
	default:
		payload = map[string]any{
			"msgtype": msgTypeText,
			"text":    map[string]string{"content": text},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dingtalk: encode webhook reply: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dingtalk: build webhook reply: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.http.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: post webhook reply: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk: webhook reply http %d: %s", resp.StatusCode, truncate(raw))
	}

	// Like the rest of the legacy surface, the webhook answers 200 with an
	// errcode body on failure.
	var envelope struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.ErrCode != 0 {
		return fmt.Errorf("dingtalk: webhook reply errcode=%d errmsg=%s", envelope.ErrCode, envelope.ErrMsg)
	}
	return nil
}

// replyWebhookText is the plain-text shorthand used by the pairing notice.
func (c *Channel) replyWebhookText(ctx context.Context, webhook, text string) error {
	return c.replyWebhook(ctx, webhook, msgTypeText, text)
}
