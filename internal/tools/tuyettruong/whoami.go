package tuyettruong

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// WhoAmITool resolves a (platform, platform_user_id) pair against the
// tuyettruong bot_identities table and returns the actor's role. Use it at
// the start of a conversation to decide between Admin Mode and Sales Mode.
//
// Goes direct to Supabase PostgREST (RLS allows public SELECT on
// bot_identities) — small lookup, no business logic, no need to hit Next.js.
type WhoAmITool struct {
	client *Client
}

func NewWhoAmITool(c *Client) *WhoAmITool { return &WhoAmITool{client: c} }

func (t *WhoAmITool) Name() string { return "tt_whoami" }
func (t *WhoAmITool) Description() string {
	return "Look up the role of a platform user (Telegram/Zalo) in the tuyettruong store. Call this ONCE at the start of a fresh conversation to know whether to enter Admin Mode (role=admin/staff) or Sales Mode (role=customer or unknown). Returns {role, label, found}."
}
func (t *WhoAmITool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platform": map[string]any{
				"type": "string",
				"enum": []string{"telegram", "zalo_personal", "zalo_oa"},
			},
			"platform_user_id": map[string]any{
				"type":        "string",
				"description": "Platform-native user id (e.g. Telegram chat_id, Zalo follower_id)",
			},
		},
		"required": []string{"platform", "platform_user_id"},
	}
}

func (t *WhoAmITool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	platform, _ := args["platform"].(string)
	userID, _ := args["platform_user_id"].(string)
	if platform == "" || userID == "" {
		return errorResult(fmt.Errorf("platform and platform_user_id required"))
	}

	path := fmt.Sprintf(
		"bot_identities?platform=eq.%s&platform_user_id=eq.%s&active=eq.true&select=role,label,platform_user_id&limit=1",
		url.QueryEscape(platform), url.QueryEscape(userID),
	)
	var rows []struct {
		Role           string `json:"role"`
		Label          string `json:"label"`
		PlatformUserID string `json:"platform_user_id"`
	}
	if err := t.client.SupabaseSelect(ctx, path, &rows); err != nil {
		return errorResult(err)
	}
	if len(rows) == 0 {
		return jsonResult(map[string]any{
			"found": false,
			"role":  "unknown",
			"note":  "Người này chưa được đăng ký trong bot_identities. Mặc định Sales Mode.",
		})
	}
	return jsonResult(map[string]any{
		"found": true,
		"role":  rows[0].Role,
		"label": rows[0].Label,
	})
}
