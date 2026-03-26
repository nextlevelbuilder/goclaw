package mattermost

// Mattermost supports standard Markdown natively — no conversion needed.
// This file contains utility functions for message formatting specific to Mattermost.

import (
	"fmt"
	"strings"
)

// formatMention creates a Mattermost @mention for a user.
func formatMention(username string) string {
	if username == "" {
		return ""
	}
	if strings.HasPrefix(username, "@") {
		return username
	}
	return "@" + username
}

// formatChannelMention creates a Mattermost ~channel mention.
func formatChannelMention(channelName string) string {
	if channelName == "" {
		return ""
	}
	if strings.HasPrefix(channelName, "~") {
		return channelName
	}
	return "~" + channelName
}

// formatQuote wraps text in Mattermost blockquote syntax.
func formatQuote(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

// formatSystemMessage formats a system notification message.
func formatSystemMessage(msg string) string {
	return fmt.Sprintf("__%s__", msg)
}

// isNonRetryableAuthError checks if an error message indicates a non-retryable auth failure.
func isNonRetryableAuthError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	nonRetryable := []string{
		"invalid_auth",
		"invalid or expired session",
		"token revoked",
		"unauthorized",
		"not_authed",
		"invalid_token",
	}
	for _, pattern := range nonRetryable {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
