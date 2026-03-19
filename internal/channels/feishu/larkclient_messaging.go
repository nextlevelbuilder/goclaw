package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
)

// --- IM API: Messages ---

type SendMessageResp struct {
	MessageID string `json:"message_id"`
}

func (c *LarkClient) SendMessage(ctx context.Context, receiveIDType, receiveID, msgType, content string) (*SendMessageResp, error) {
	path := "/open-apis/im/v1/messages?receive_id_type=" + receiveIDType
	body := map[string]string{
		"receive_id": receiveID,
		"msg_type":   msgType,
		"content":    content,
	}
	resp, err := c.doJSON(ctx, "POST", path, body)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("send message: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var data SendMessageResp
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &data, nil
}

// --- IM API: Images ---

func (c *LarkClient) DownloadImage(ctx context.Context, imageKey string) ([]byte, error) {
	path := "/open-apis/im/v1/images/" + imageKey
	data, _, err := c.doDownload(ctx, path)
	return data, err
}

func (c *LarkClient) UploadImage(ctx context.Context, data io.Reader) (string, error) {
	resp, err := c.doMultipart(ctx, "/open-apis/im/v1/images",
		map[string]string{"image_type": "message"},
		"image", data, "image.png")
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("upload image: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var result struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.ImageKey, nil
}

// --- IM API: Files ---

func (c *LarkClient) UploadFile(ctx context.Context, data io.Reader, fileName, fileType string, durationMs int) (string, error) {
	fields := map[string]string{
		"file_type": fileType,
		"file_name": fileName,
	}
	if durationMs > 0 {
		fields["duration"] = strconv.Itoa(durationMs)
	}
	resp, err := c.doMultipart(ctx, "/open-apis/im/v1/files", fields, "file", data, fileName)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("upload file: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var result struct {
		FileKey string `json:"file_key"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.FileKey, nil
}

// --- IM API: Get Message ---

// GetMessageResp holds the response from GET /open-apis/im/v1/messages/{message_id}.
type GetMessageResp struct {
	Items []struct {
		MessageID   string `json:"message_id"`
		MsgType     string `json:"msg_type"`
		Body        struct {
			Content string `json:"content"`
		} `json:"body"`
		Sender struct {
			ID         string `json:"id"`
			IDType     string `json:"id_type"`
			SenderType string `json:"sender_type"`
		} `json:"sender"`
	} `json:"items"`
}

// GetMessage retrieves a message by ID.
// Lark API: GET /open-apis/im/v1/messages/{message_id}
func (c *LarkClient) GetMessage(ctx context.Context, messageID string) (*GetMessageResp, error) {
	path := fmt.Sprintf("/open-apis/im/v1/messages/%s", messageID)
	resp, err := c.doJSON(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("get message: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var data GetMessageResp
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("unmarshal get message: %w", err)
	}
	return &data, nil
}

// --- IM API: Message Resources ---

func (c *LarkClient) DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string) ([]byte, string, error) {
	path := fmt.Sprintf("/open-apis/im/v1/messages/%s/resources/%s?type=%s", messageID, fileKey, resourceType)
	return c.doDownload(ctx, path)
}

// --- CardKit API ---

func (c *LarkClient) CreateCard(ctx context.Context, cardType, data string) (string, error) {
	resp, err := c.doJSON(ctx, "POST", "/open-apis/cardkit/v1/cards", map[string]string{
		"type": cardType,
		"data": data,
	})
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("create card: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var result struct {
		CardID string `json:"card_id"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.CardID, nil
}

func (c *LarkClient) UpdateCardSettings(ctx context.Context, cardID, settings string, seq int, uuid string) error {
	path := "/open-apis/cardkit/v1/cards/" + cardID
	resp, err := c.doJSON(ctx, "PATCH", path, map[string]any{
		"settings": settings,
		"sequence": seq,
		"uuid":     uuid,
	})
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("update card settings: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *LarkClient) UpdateCardElement(ctx context.Context, cardID, elementID, content string, seq int, uuid string) error {
	path := fmt.Sprintf("/open-apis/cardkit/v1/cards/%s/elements/%s", cardID, elementID)
	resp, err := c.doJSON(ctx, "PATCH", path, map[string]any{
		"content":  content,
		"sequence": seq,
		"uuid":     uuid,
	})
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		slog.Debug("lark update card element failed", "code", resp.Code, "msg", resp.Msg)
		return fmt.Errorf("update card element: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// --- IM API: Reactions ---

// AddMessageReaction adds an emoji reaction to a message.
// Returns the reaction_id for later removal. emojiType: e.g. "Typing", "THUMBSUP".
// Lark API: POST /open-apis/im/v1/messages/{message_id}/reactions
func (c *LarkClient) AddMessageReaction(ctx context.Context, messageID, emojiType string) (string, error) {
	path := fmt.Sprintf("/open-apis/im/v1/messages/%s/reactions", messageID)
	body := map[string]any{
		"reaction_type": map[string]string{
			"emoji_type": emojiType,
		},
	}
	resp, err := c.doJSON(ctx, "POST", path, body)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("add reaction: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var result struct {
		ReactionID string `json:"reaction_id"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.ReactionID, nil
}

// DeleteMessageReaction removes a reaction from a message.
// Lark API: DELETE /open-apis/im/v1/messages/{message_id}/reactions/{reaction_id}
func (c *LarkClient) DeleteMessageReaction(ctx context.Context, messageID, reactionID string) error {
	path := fmt.Sprintf("/open-apis/im/v1/messages/%s/reactions/%s", messageID, reactionID)
	resp, err := c.doJSON(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("delete reaction: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// --- Bot API ---

// GetBotInfo fetches the bot's identity from /open-apis/bot/v3/info.
// Returns the bot's open_id which is needed for mention detection in groups.
func (c *LarkClient) GetBotInfo(ctx context.Context) (string, error) {
	resp, err := c.doJSON(ctx, "GET", "/open-apis/bot/v3/info", nil)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("get bot info: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var result struct {
		Bot struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.Bot.OpenID, nil
}

// --- IM API: Chat Members ---

// ChatMember represents a member of a Lark group chat.
type ChatMember struct {
	MemberID     string `json:"member_id"`
	MemberIDType string `json:"member_id_type"`
	Name         string `json:"name"`
	TenantKey    string `json:"tenant_key"`
}

// ListChatMembers returns all members of a group chat, handling pagination automatically.
// Lark API: GET /open-apis/im/v1/chats/{chat_id}/members
// Requires scope: im:chat.members:read
func (c *LarkClient) ListChatMembers(ctx context.Context, chatID string) ([]ChatMember, error) {
	var all []ChatMember
	pageToken := ""

	for {
		path := fmt.Sprintf("/open-apis/im/v1/chats/%s/members?member_id_type=open_id&page_size=100", url.PathEscape(chatID))
		if pageToken != "" {
			path += "&page_token=" + url.QueryEscape(pageToken)
		}

		resp, err := c.doJSON(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		if resp.Code != 0 {
			return nil, fmt.Errorf("list chat members: code=%d msg=%s", resp.Code, resp.Msg)
		}

		var result struct {
			Items     []ChatMember `json:"items"`
			PageToken string       `json:"page_token"`
			HasMore   bool         `json:"has_more"`
		}
		if err := json.Unmarshal(resp.Data, &result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}

		all = append(all, result.Items...)

		if !result.HasMore || result.PageToken == "" {
			break
		}
		pageToken = result.PageToken
	}

	return all, nil
}

// --- Contact API ---

func (c *LarkClient) GetUser(ctx context.Context, userID, userIDType string) (string, error) {
	path := fmt.Sprintf("/open-apis/contact/v3/users/%s?user_id_type=%s", userID, userIDType)
	resp, err := c.doJSON(ctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("get user: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var result struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.User.Name, nil
}
