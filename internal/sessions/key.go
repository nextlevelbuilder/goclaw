// Package sessions — session key builder and parser.
//
// Session keys follow the TS OpenClaw canonical format:
//
//	agent:{agentId}:{rest}
//
// Where {rest} depends on the session type:
//
//	DM:          {channel}:direct:{peerId}
//	Group:       {channel}:group:{groupId}
//	Forum topic: {channel}:group:{groupId}:topic:{topicId}
//	Subagent:    subagent:{label}
//	Cron:        cron:{jobId}
//
// Examples:
//
//	agent:default:telegram:direct:386246614
//	agent:default:telegram:group:-100123456
//	agent:default:telegram:group:-100123456:topic:99
//	agent:default:subagent:my-task
//	agent:default:cron:reminder-job-id
package sessions

import (
	"fmt"
	"strings"
)

// PeerKind distinguishes DM from group conversations.
type PeerKind string

const (
	PeerDirect PeerKind = "direct"
	PeerGroup  PeerKind = "group"
)

// BuildSessionKey builds the canonical agent session key for a channel conversation.
//
//	DM:    agent:{agentId}:{channel}:direct:{peerID}
//	Group: agent:{agentId}:{channel}:group:{chatID}
func BuildSessionKey(agentID, channel string, kind PeerKind, chatID string) string {
	return fmt.Sprintf("agent:%s:%s:%s:%s", agentID, channel, kind, chatID)
}

// BuildGroupTopicSessionKey builds the session key for a forum group topic.
// TS ref: buildTelegramGroupPeerId() in src/telegram/bot/helpers.ts
//
//	agent:{agentId}:{channel}:group:{chatID}:topic:{topicID}
func BuildGroupTopicSessionKey(agentID, channel, chatID string, topicID int) string {
	return fmt.Sprintf("agent:%s:%s:group:%s:topic:%d", agentID, channel, chatID, topicID)
}

// BuildDMThreadSessionKey builds the session key for a DM thread (topic in private chat).
// Preserves message_thread_id for session isolation within the same DM.
//
//	agent:{agentId}:{channel}:direct:{peerID}:thread:{threadID}
func BuildDMThreadSessionKey(agentID, channel, peerID string, threadID int) string {
	return fmt.Sprintf("agent:%s:%s:direct:%s:thread:%d", agentID, channel, peerID, threadID)
}

// BuildSubagentSessionKey builds the session key for a subagent.
//
//	agent:{agentId}:subagent:{label}
func BuildSubagentSessionKey(agentID, label string) string {
	return fmt.Sprintf("agent:%s:subagent:%s", agentID, label)
}

// BuildTeamSessionKey builds an isolated session key for team task execution.
// Scoped per agent + team + chatID (user), matching workspace isolation.
// All tasks from the same user within the same team share one session per member agent.
//
//	agent:{agentId}:team:{teamId}:{chatId}
func BuildTeamSessionKey(agentID, teamID, chatID string) string {
	return fmt.Sprintf("agent:%s:team:%s:%s", agentID, teamID, chatID)
}

// IsTeamSession checks if a session key indicates a team session.
func IsTeamSession(key string) bool {
	_, rest := ParseSessionKey(key)
	return strings.HasPrefix(rest, "team:")
}

// BuildCronSessionKey builds the session key for a cron job.
// Each cron job gets one persistent session (all runs share the same history).
//
//	agent:{agentId}:cron:{jobID}
//
// Guards against double-prefixing: if jobID is already a canonical session key
// (e.g. "agent:X:..."), only the rest part is used.
func BuildCronSessionKey(agentID, jobID string) string {
	if _, rest := ParseSessionKey(jobID); rest != "" {
		jobID = rest
	}
	return fmt.Sprintf("agent:%s:cron:%s", agentID, jobID)
}

// BuildAgentMainSessionKey builds the shared "main" session key for an agent.
// Used when dm_scope="main" — all DMs share one session per agent.
// Matching TS buildAgentMainSessionKey().
//
//	agent:{agentId}:{mainKey}
func BuildAgentMainSessionKey(agentID, mainKey string) string {
	if mainKey == "" {
		mainKey = "main"
	}
	return fmt.Sprintf("agent:%s:%s", agentID, mainKey)
}

// BuildScopedSessionKey builds session key based on scope config.
// Matching TS src/routing/session-key.ts buildAgentPeerSessionKey().
//
// scope:
//   - "global"     → "global"
//   - "per-sender"  → depends on dmScope (default)
//
// dmScope (for DMs only — groups always use full key):
//   - "main"                     → agent:{agentId}:{mainKey}
//   - "per-peer"                 → agent:{agentId}:direct:{peerId}
//   - "per-channel-peer"         → agent:{agentId}:{channel}:direct:{peerId}  (default)
//   - "per-account-channel-peer" → agent:{agentId}:{channel}:{accountId}:direct:{peerId}
func BuildScopedSessionKey(agentID, channel string, kind PeerKind, chatID, scope, dmScope, mainKey string) string {
	// Global scope: one session for everything
	if scope == "global" {
		return "global"
	}

	// Groups always use full key (matching TS)
	if kind == PeerGroup {
		return BuildSessionKey(agentID, channel, kind, chatID)
	}

	// DM scope modes
	switch dmScope {
	case "main":
		return BuildAgentMainSessionKey(agentID, mainKey)
	case "per-peer":
		return fmt.Sprintf("agent:%s:direct:%s", agentID, chatID)
	case "per-account-channel-peer":
		// accountId not yet wired — falls through to per-channel-peer behavior
		return BuildSessionKey(agentID, channel, kind, chatID)
	default: // "per-channel-peer" or empty
		return BuildSessionKey(agentID, channel, kind, chatID)
	}
}

// ParseSessionKey extracts the agentID and rest from a canonical session key.
// Returns ("", "") if the key is not in the expected format.
func ParseSessionKey(key string) (agentID, rest string) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 || parts[0] != "agent" {
		return "", ""
	}
	return parts[1], parts[2]
}

// IsSubagentSession checks if a session key indicates a subagent session.
func IsSubagentSession(key string) bool {
	_, rest := ParseSessionKey(key)
	return strings.HasPrefix(strings.ToLower(rest), "subagent:")
}

// IsCronSession checks if a session key indicates a cron session.
func IsCronSession(key string) bool {
	_, rest := ParseSessionKey(key)
	return strings.HasPrefix(strings.ToLower(rest), "cron:")
}

// PeerKindFromGroup returns PeerGroup if isGroup is true, PeerDirect otherwise.
func PeerKindFromGroup(isGroup bool) PeerKind {
	if isGroup {
		return PeerGroup
	}
	return PeerDirect
}
