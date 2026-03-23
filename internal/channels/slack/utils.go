package slack

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// HandleMessage overrides BaseChannel to allow messages when the chatID (Slack channel)
// is in the allowlist, enabling group-level allowlisting without requiring individual user IDs.
// This is Slack-specific: other channels only check senderID in BaseChannel.HandleMessage.
func (c *Channel) HandleMessage(senderID, chatID, content string, mediaPaths []string, metadata map[string]string, peerKind string) {
	// Allow if either the sender or the Slack channel ID is in the allowlist.
	if !c.IsAllowed(senderID) && !c.IsAllowed(chatID) {
		return
	}

	userID := senderID
	if idx := strings.IndexByte(senderID, '|'); idx > 0 {
		userID = senderID[:idx]
	}

	var mediaFiles []bus.MediaFile
	for _, p := range mediaPaths {
		mediaFiles = append(mediaFiles, bus.MediaFile{Path: p})
	}

	// Collect contact for processed messages (DM + group-mentioned).
	if cc := c.ContactCollector(); cc != nil {
		ctx := store.WithTenantID(context.Background(), c.TenantID())
		cc.EnsureContact(ctx, c.Type(), c.Name(), userID, userID, metadata["username"], "", peerKind)
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Media:    mediaFiles,
		PeerKind: peerKind,
		UserID:   userID,
		Metadata: metadata,
		AgentID:  c.AgentID(),
	})
}

// BlockReplyEnabled returns the per-channel block_reply override.
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// resolveDisplayName fetches and caches the Slack display name for a user ID.
func (c *Channel) resolveDisplayName(userID string) string {
	c.userCacheMu.RLock()
	cu, found := c.userCache[userID]
	c.userCacheMu.RUnlock()

	if found && time.Since(cu.fetchedAt) < userCacheTTL {
		return cu.displayName
	}

	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		slog.Debug("slack: failed to resolve user", "user_id", userID, "error", err)
		return userID
	}

	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}
	if name == "" {
		name = user.Name
	}

	c.userCacheMu.Lock()
	c.userCache[userID] = cachedUser{displayName: name, fetchedAt: time.Now()}
	c.userCacheMu.Unlock()

	return name
}

// nonRetryableAuthErrors matches Slack errors that indicate permanent auth failure.
var nonRetryableAuthErrors = regexp.MustCompile(
	`(?i)(invalid_auth|token_revoked|account_inactive|not_authed|team_not_found|missing_scope)`,
)

func isNonRetryableAuthError(errMsg string) bool {
	return nonRetryableAuthErrors.MatchString(errMsg)
}

// HealthProbe performs an auth.test call to verify the Slack connection is alive.
func (c *Channel) HealthProbe(ctx context.Context) (ok bool, elapsed time.Duration, err error) {
	if c.api == nil {
		return false, 0, fmt.Errorf("slack client not initialized (Start() not called)")
	}

	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()

	_, err = c.api.AuthTestContext(probeCtx)
	elapsed = time.Since(start)
	return err == nil, elapsed, err
}
