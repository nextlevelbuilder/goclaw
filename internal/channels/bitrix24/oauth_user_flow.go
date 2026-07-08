package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// sendOAuthInvite delivers the "please re-authorize" message: a private DM
// (dialogId=userID, NOT chatID) with fields.message (short hint) + a single
// fields.keyboard button whose LINK is the authorize URL (design.md §6 — a
// button, not a raw link in text). If the trigger came from a group chat, a
// separate plain-text hint is left in the original chatID so the user knows
// to check their DMs — the hint never carries the URL. If the DM send itself
// fails (e.g. the bot cannot message this user), falls back to sending the
// SAME message+keyboard into chatID instead of losing the invite entirely.
//
// Debounced 5min/user via its own map (oauthInviteMu/oauthInviteDebounce on
// Channel) — deliberately independent of notifyUserOfMCPIssueOnce's debounce
// (provisioner.go) so the two notice types never suppress each other.
func (c *Channel) sendOAuthInvite(ctx context.Context, userID, chatID string, isGroup bool, url string) {
	if !c.tryAcquireOAuthInviteNotify(userID) {
		return
	}

	fields := map[string]any{
		"message": oauthInviteMessage,
		"keyboard": []map[string]any{
			{"TEXT": oauthInviteButtonText, "LINK": url},
		},
	}
	sendTo := func(dialogID string) error {
		_, err := c.Client().Call(ctx, "imbot.v2.Chat.Message.send", map[string]any{
			"botId":    c.BotID(),
			"dialogId": dialogID,
			"fields":   fields,
		})
		return err
	}

	if err := sendTo(userID); err != nil {
		slog.Warn("bitrix24 mcp: oauth invite DM failed, falling back to original dialog",
			"channel", c.Name(), "user", userID, "chat_id", chatID, "err", err)
		if fbErr := sendTo(chatID); fbErr != nil {
			slog.Warn("bitrix24 mcp: oauth invite fallback also failed",
				"channel", c.Name(), "user", userID, "chat_id", chatID, "err", fbErr)
		}
		return
	}
	if isGroup {
		if err := c.sendChunk(ctx, chatID, oauthInviteGroupHintMessage, sendOptions{visibility: VisibilityPublic}); err != nil {
			slog.Debug("bitrix24 mcp: oauth invite group hint failed",
				"channel", c.Name(), "user", userID, "chat_id", chatID, "err", err)
		}
	}
}

// tryAcquireOAuthInviteNotify mirrors tryAcquireMCPProvision's atomic
// check-and-set shape (provisioner.go) but guards a DIFFERENT debounce map —
// this one gates the "please re-authorize" DM, not the underlying auto-onboard
// attempt. Returns true when the caller acquired the slot (not debounced).
func (c *Channel) tryAcquireOAuthInviteNotify(userID string) bool {
	c.oauthInviteMu.Lock()
	defer c.oauthInviteMu.Unlock()
	if c.oauthInviteDebounce == nil {
		c.oauthInviteDebounce = make(map[string]time.Time)
	}
	if ts, ok := c.oauthInviteDebounce[userID]; ok && time.Since(ts) < mcpUserNotifyDebounceTTL {
		return false
	}
	c.oauthInviteDebounce[userID] = time.Now()
	return true
}

// UserOnboardResult is what HandleUserOAuthCallback returns so the HTTP
// handler (webhook.go, handleUserOAuthCallback) can render the right outcome
// page. Outcome is one of "success", "declined", "identity_mismatch".
type UserOnboardResult struct {
	Outcome string
}

// HandleUserOAuthCallback finishes a per-user OAuth re-authorization: it
// exchanges the authorization code, validates the response actually belongs
// to the Bitrix user the DM invite targeted, then mints/refreshes MCP
// credentials via the SAME autoOnboard + SetUserCredentials sequence
// provisionIfMissing already uses (provisioner.go) — no separate mint logic,
// no api_key churn (design.md §12).
//
// payload is the ALREADY-DECODED + HMAC-verified state (decoding happens in
// the HTTP handler, which needs the payload's BotID to resolve which Channel
// to call this on in the first place — see handleUserOAuthCallback).
func (c *Channel) HandleUserOAuthCallback(ctx context.Context, code string, payload *oauthStatePayload) (*UserOnboardResult, error) {
	portal := c.Portal()
	if portal == nil {
		return nil, errors.New("bitrix24 oauth callback: portal not available")
	}

	tr, err := portal.ExchangeUserAuthCode(ctx, code)
	if err != nil {
		slog.Warn("bitrix24 oauth callback: exchange failed",
			"channel", c.Name(), "user_id", payload.UserID, "err", err)
		return nil, fmt.Errorf("bitrix24 oauth callback: exchange: %w", err)
	}

	// Identity check: the Bitrix account that just approved MUST be the same
	// one the DM invite was built for. Without this, user X's invite link
	// could be completed by user Y (e.g. forwarded, or X asks a colleague to
	// click it), silently attaching Y's Bitrix identity to X's MCP row.
	gotUserID := strconv.FormatInt(tr.UserID, 10)
	if gotUserID != payload.UserID {
		slog.Warn("bitrix24 oauth callback: identity mismatch — authorized as a different Bitrix user",
			"channel", c.Name(), "expected_user_id", payload.UserID, "got_user_id", gotUserID)
		return &UserOnboardResult{Outcome: "identity_mismatch"}, nil
	}

	if c.mcpStore == nil || c.mcpClient == nil || c.mcpServerID == uuid.Nil {
		return nil, errors.New("bitrix24 oauth callback: mcp provisioning not configured for this channel")
	}

	resp, err := c.mcpClient.autoOnboard(ctx, autoOnboardRequest{
		Domain:       tr.Domain,
		BitrixUserID: payload.UserID,
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresIn:    int(tr.ExpiresIn),
	})
	if err != nil {
		return nil, fmt.Errorf("bitrix24 oauth callback: auto-onboard: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	creds := store.MCPUserCredentials{
		APIKey: resp.APIKey,
		Env: map[string]string{
			"BITRIX_DOMAIN":        tr.Domain,
			"BITRIX_ACCESS_TOKEN":  tr.AccessToken,
			"BITRIX_REFRESH_TOKEN": tr.RefreshToken,
			"BITRIX_EXPIRES_AT":    expiresAt,
		},
	}
	if err := c.mcpStore.SetUserCredentials(ctx, c.mcpServerID, payload.UserID, creds); err != nil {
		return nil, fmt.Errorf("bitrix24 oauth callback: persist credentials: %w", err)
	}

	slog.Info("bitrix24 oauth callback: user re-authorized",
		"channel", c.Name(), "user_id", payload.UserID, "created", resp.Created)
	return &UserOnboardResult{Outcome: "success"}, nil
}
