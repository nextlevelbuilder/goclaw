package cmd

import "github.com/nextlevelbuilder/goclaw/internal/agent"

// normalizeAgentOutboundContent keeps silent text out of user-facing messages
// while allowing attached media to continue through channel delivery.
func normalizeAgentOutboundContent(content string, mediaCount int) (string, bool) {
	isSilent := content == "" || agent.IsSilentReply(content)
	if !isSilent {
		return content, true
	}
	if mediaCount == 0 {
		return "", false
	}
	return "", true
}
