package tools

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// isSessionInScope checks whether a target session key falls within
// the current execution scope. Group-scoped runs can only access
// sessions belonging to the same group, unless share_sessions is enabled.
func isSessionInScope(ctx context.Context, targetKey, currentKey string) bool {
	// Shared sessions mode — all sessions visible.
	if store.IsSharedSessions(ctx) {
		return true
	}

	// Always allow own session.
	if targetKey == currentKey {
		return true
	}

	// Extract group chat ID from user context.
	userID := store.UserIDFromContext(ctx)
	chatID := extractGroupChatID(userID)
	if chatID == "" {
		return true // DM or non-group scope — no restriction.
	}

	// Allow sessions containing this group's chat ID.
	// Colon-bounded match prevents partial numeric collisions.
	// Matches patterns like:
	//   agent:X:channel:group:{chatID}
	//   agent:X:channel:group:{chatID}:topic:N
	marker := ":" + chatID
	return strings.HasSuffix(targetKey, marker) ||
		strings.Contains(targetKey, marker+":")
}

// extractGroupChatID extracts the chat ID from a group-scoped userID.
// Format: "group:channel:chatID" -> "chatID".
// Returns "" for non-group users (DM, guild, etc.).
func extractGroupChatID(userID string) string {
	if !strings.HasPrefix(userID, "group:") {
		return ""
	}
	parts := strings.SplitN(userID, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}
