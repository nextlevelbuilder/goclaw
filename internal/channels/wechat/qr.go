package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GetBotQRCodeReq is the getBotQRCode request body.
type GetBotQRCodeReq struct {
	BotType  string    `json:"bot_type,omitempty"`
	BaseInfo *BaseInfo `json:"base_info,omitempty"`
}

// GetBotQRCodeResp is the getBotQRCode response.
type GetBotQRCodeResp struct {
	QRCode           string `json:"qrcode,omitempty"`
	QRCodeImgContent string `json:"qrcode_img_content,omitempty"`
}

// GetQRCodeStatusReq is the getQRCodeStatus request body.
type GetQRCodeStatusReq struct {
	QRCode   string    `json:"qrcode,omitempty"`
	BaseInfo *BaseInfo `json:"base_info,omitempty"`
}

// GetQRCodeStatusResp is the getQRCodeStatus response.
type GetQRCodeStatusResp struct {
	Status      string `json:"status,omitempty"`
	BotToken    string `json:"bot_token,omitempty"`
	ILinkBotID  string `json:"ilink_bot_id,omitempty"`
	BaseURL     string `json:"baseurl,omitempty"`
	ILinkUserID string `json:"ilink_user_id,omitempty"`
	RedirectHost string `json:"redirect_host,omitempty"`
}

// GetBotQRCode fetches the login QR code.
func (c *APIClient) GetBotQRCode(ctx context.Context, botType string) (*GetBotQRCodeResp, error) {
	q := url.Values{}
	q.Set("bot_type", botType)

	respBody, err := c.apiGet(ctx, "ilink/bot/get_bot_qrcode", q, defaultAPITimeoutMs)
	if err != nil {
		return nil, err
	}
	var resp GetBotQRCodeResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal getBotQRCode: %w", err)
	}
	return &resp, nil
}

// GetQRCodeStatus long-polls the QR code scan status.
func (c *APIClient) GetQRCodeStatus(ctx context.Context, qrcode string, timeoutMs int) (*GetQRCodeStatusResp, error) {
	q := url.Values{}
	q.Set("qrcode", qrcode)

	respBody, err := c.apiGet(ctx, "ilink/bot/get_qrcode_status", q, timeoutMs)
	if err != nil {
		return nil, err
	}
	var resp GetQRCodeStatusResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal getQRCodeStatus: %w", err)
	}
	return &resp, nil
}
