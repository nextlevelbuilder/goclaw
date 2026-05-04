package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// apiBase is the Zalo Bot API root; var so tests can override.
var apiBase = "https://bot-api.zaloplatforms.com"

func (c *Channel) callAPI(method string, body any) (json.RawMessage, error) {
	return c.callAPIWith(context.Background(), c.client, method, body)
}

func (c *Channel) callAPIWith(ctx context.Context, client *http.Client, method string, body any) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/bot%s/%s", apiBase, c.token, method)

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		// *url.Error embeds the full URL including the bot token; scrub it.
		return nil, fmt.Errorf("api call %s: %s", method, strings.ReplaceAll(err.Error(), c.token, "<redacted>"))
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp zaloAPIResponse
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !apiResp.OK {
		return nil, formatAPIError(apiResp.ErrorCode, apiResp.Description)
	}

	return apiResp.Result, nil
}

func (c *Channel) getMe() (*zaloBotInfo, error) {
	result, err := c.callAPI("getMe", nil)
	if err != nil {
		return nil, err
	}

	var info zaloBotInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("unmarshal bot info: %w", err)
	}
	return &info, nil
}

func (c *Channel) deleteWebhook() error {
	_, err := c.callAPI("deleteWebhook", nil)
	return err
}

func (c *Channel) getUpdates(timeout int) ([]zaloUpdate, error) {
	params := map[string]any{
		"timeout": timeout,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second+pollTimeoutHeadroom)
	defer cancel()

	result, err := c.callAPIWith(ctx, c.pollClient, "getUpdates", params)
	if err != nil {
		return nil, err
	}

	if len(result) > 0 && result[0] == '[' {
		var batch []zaloUpdate
		if err := json.Unmarshal(result, &batch); err != nil {
			return nil, fmt.Errorf("unmarshal updates: %w", err)
		}
		out := batch[:0]
		for _, u := range batch {
			if u.EventName != "" {
				out = append(out, u)
			}
		}
		return out, nil
	}
	var update zaloUpdate
	if err := json.Unmarshal(result, &update); err != nil {
		return nil, fmt.Errorf("unmarshal updates: %w", err)
	}
	if update.EventName == "" {
		return nil, nil
	}
	return []zaloUpdate{update}, nil
}

func (c *Channel) sendMessage(chatID, text string) error {
	params := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}

	_, err := c.callAPI("sendMessage", params)
	return err
}

func (c *Channel) sendPhoto(chatID, photoURL, caption string) error {
	params := map[string]any{
		"chat_id": chatID,
		"photo":   photoURL,
	}
	if caption != "" {
		params["caption"] = caption
	}

	_, err := c.callAPI("sendPhoto", params)
	return err
}

func (c *Channel) sendChatAction(chatID, action string) error {
	_, err := c.callAPI("sendChatAction", map[string]any{
		"chat_id": chatID,
		"action":  action,
	})
	return err
}
