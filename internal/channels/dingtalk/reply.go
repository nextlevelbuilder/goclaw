package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// replyWebhookText posts a plain-text reply to a message's session webhook.
//
// The session webhook is a pre-signed, per-message URL: it carries its own
// authorization, so no access token is attached. It also expires — callers that
// might run long must check the expiry and fall back to the proactive OpenAPI.
//
// This is the minimum needed to answer an unpaired sender in phase 3. The full
// outbound path (chunking, markdown, proactive fallback, media) is phase 4.
func (c *Channel) replyWebhookText(ctx context.Context, webhook, text string) error {
	if webhook == "" {
		return fmt.Errorf("dingtalk: no session webhook for reply")
	}

	payload := map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
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
