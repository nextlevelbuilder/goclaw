package cmd

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// resolveAgentRoute determines which agent should handle a message
// based on config bindings. Priority: peer → channel → default.
// Matching TS resolve-route.ts binding resolution.
func resolveAgentRoute(cfg *config.Config, channel, chatID, peerKind string) string {
	for _, binding := range cfg.Bindings {
		match := binding.Match
		if match.Channel != channel {
			continue
		}

		// Peer-level match (most specific)
		if match.Peer != nil {
			if match.Peer.Kind == peerKind && match.Peer.ID == chatID {
				return config.NormalizeAgentID(binding.AgentID)
			}
			continue // has peer constraint but doesn't match — skip
		}

		// Channel-level match (least specific, no peer constraint)
		return config.NormalizeAgentID(binding.AgentID)
	}

	return cfg.ResolveDefaultAgentID()
}

// overrideSessionKeyFromLocalKey extracts topic/thread ID from the composite
// local_key and returns the correct session key for forum topics or DM threads.
// If localKey is empty or has no suffix, the original sessionKey is returned unchanged.
func overrideSessionKeyFromLocalKey(sessionKey, localKey, agentID, channel, chatID, peerKind string) string {
	if localKey == "" {
		return sessionKey
	}
	if idx := strings.Index(localKey, ":topic:"); idx > 0 && peerKind == string(sessions.PeerGroup) {
		var topicID int
		fmt.Sscanf(localKey[idx+7:], "%d", &topicID)
		if topicID > 0 {
			return sessions.BuildGroupTopicSessionKey(agentID, channel, chatID, topicID)
		}
	} else if idx := strings.Index(localKey, ":thread:"); idx > 0 && peerKind == string(sessions.PeerDirect) {
		var threadID int
		fmt.Sscanf(localKey[idx+8:], "%d", &threadID)
		if threadID > 0 {
			return sessions.BuildDMThreadSessionKey(agentID, channel, chatID, threadID)
		}
	}
	return sessionKey
}

// extractSessionMetadata builds a metadata map from channel InboundMessage metadata.
// Used to persist friendly names (display_name, username, chat_title) into sessions
// and user profiles so the web UI can show human-readable labels.
func extractSessionMetadata(msg bus.InboundMessage, peerKind string) map[string]string {
	meta := make(map[string]string)

	// Display name: prefer first_name (Telegram), fall back to display_name (Discord)
	if v := msg.Metadata["first_name"]; v != "" {
		meta["display_name"] = v
	} else if v := msg.Metadata["display_name"]; v != "" {
		meta["display_name"] = v
	}

	if v := msg.Metadata[tools.MetaUsername]; v != "" {
		meta[tools.MetaUsername] = v
	}
	if peerKind != "" {
		meta["peer_kind"] = peerKind
	}
	if v := msg.Metadata[tools.MetaChatTitle]; v != "" {
		meta[tools.MetaChatTitle] = v
	}

	if len(meta) == 0 {
		return nil
	}
	return meta
}

// buildAnnounceOutMeta builds outbound metadata for announce messages so that
// Send() can route replies to the correct forum topic or DM thread.
func buildAnnounceOutMeta(localKey string) map[string]string {
	if localKey == "" {
		return nil
	}
	meta := map[string]string{"local_key": localKey}
	if idx := strings.Index(localKey, ":topic:"); idx > 0 {
		meta[tools.MetaMessageThreadID] = localKey[idx+7:]
	} else if idx := strings.Index(localKey, ":thread:"); idx > 0 {
		meta[tools.MetaMessageThreadID] = localKey[idx+8:]
	}
	return meta
}

// mediaToMarkdown converts media results to markdown image/link syntax using the
// /v1/files/ HTTP endpoint. Used for WS channel where outbound media attachments
// are not supported (no channel handler). Returns empty string if no media.
// Uses absolute file paths with the /v1/files endpoint (auth-token protected).
// Generates relative URLs (/v1/files/...) so they work regardless of the server's
// external hostname — the browser resolves them from the current origin.
func mediaToMarkdown(media []agent.MediaResult, cfg *config.Config) string {
	if len(media) == 0 {
		return ""
	}

	var parts []string
	for _, mr := range media {
		cleanPath := filepath.Clean(mr.Path)
		// Strip leading "/" so URL path is /v1/files/app/.goclaw/...
		urlPath := strings.TrimPrefix(cleanPath, "/")
		if urlPath == "" {
			continue
		}
		// Store clean path only — no auth tokens in persisted session messages.
		// Frontend adds auth (Bearer header or ?ft= signed token) at render time.
		// Guard: if path is already a /v1/ URL (e.g. from mutated media), don't double-prefix.
		var fileURL string
		if strings.HasPrefix(urlPath, "v1/files/") || strings.HasPrefix(urlPath, "v1/media/") {
			fileURL = "/" + strings.SplitN(urlPath, "?", 2)[0] // strip any existing query params
		} else {
			fileURL = "/v1/files/" + urlPath
		}
		if strings.HasPrefix(mr.ContentType, "image/") {
			parts = append(parts, fmt.Sprintf("![image](%s)", fileURL))
		} else {
			parts = append(parts, fmt.Sprintf("[%s](%s)", filepath.Base(mr.Path), fileURL))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(parts, "\n")
}

// mediaToMarkdownFromPaths is like mediaToMarkdown but accepts raw file paths
// ([]string from bus.InboundMessage.Media) instead of []agent.MediaResult.
func mediaToMarkdownFromPaths(files []bus.MediaFile, cfg *config.Config) string {
	if len(files) == 0 {
		return ""
	}
	media := make([]agent.MediaResult, 0, len(files))
	for _, f := range files {
		ct := f.MimeType
		if ct == "" {
			ct = mime.TypeByExtension(filepath.Ext(f.Path))
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		media = append(media, agent.MediaResult{
			Path:        f.Path,
			ContentType: ct,
		})
	}
	return mediaToMarkdown(media, cfg)
}

// resolveChannelType returns the platform type for a channel instance name.
// Returns empty string if channelMgr is nil or channel name is empty.
func resolveChannelType(channelMgr *channels.Manager, name string) string {
	if channelMgr == nil || name == "" {
		return ""
	}
	return channelMgr.ChannelTypeForName(name)
}

// resolveSenderName extracts the sender display name from channel metadata.
// Checks "sender_name" (Feishu), "first_name" (Telegram), "push_name" (WhatsApp).
// Sanitizes to prevent prompt injection via newlines/control chars.
func resolveSenderName(msg bus.InboundMessage) string {
	for _, key := range []string{"sender_name", "first_name", "push_name", "display_name"} {
		if name := msg.Metadata[key]; name != "" {
			clean := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(strings.TrimSpace(name))
			if len([]rune(clean)) > 100 {
				clean = string([]rune(clean)[:100])
			}
			return clean
		}
	}
	return ""
}
