// Package sessions — session key builder and parser.
//
// Session keys follow the TS OpenClaw canonical format:
//
//	agent:{agentKey}:{rest}
//
// IMPORTANT: {agentKey} is the human-readable agent key (e.g. "default", "my-agent"),
// NOT the agent's UUID. All Build*SessionKey functions expect agentKey, not UUID.
// This is critical for cache invalidation — the agent router cache uses agentKey
// as the lookup key, and InvalidateAgent() matches by agentKey suffix.
// Tenant isolation is handled separately via context (tenant_id in DB, prefix in cache).
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
//
// There is ONE session form that is deliberately NOT an agent session:
//
//	CLI chat:    cli:{connectionID}:{conversationID}
//
// It is a conversation with a connected coding CLI (see internal/clisession),
// which has no agent behind it at all — no loop, no provider, no system prompt.
// The "cli:" head is what makes that unmistakable: ParseSessionKey requires
// parts[0] == "agent", so it returns ("", "") for a CLI key and every predicate
// built on it (IsWSSession, IsSubagentSession, IsCronSession, IsTeamSession,
// IsHeartbeatSession) is therefore false. A CLI key can never be mistaken for
// an agent's, and an agent key can never be mistaken for a CLI one.
package sessions

import (
	"fmt"
	"strings"
	"time"
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

// BuildScopedThreadSessionKey builds a session key that includes a thread/topic ID.
// Supports string-based thread IDs (e.g. Slack timestamps).
//
//	agent:{agentId}:{channel}:{kind}:{chatID}:thread:{threadID}
func BuildScopedThreadSessionKey(agentID, channel string, kind PeerKind, chatID, threadID string) string {
	return fmt.Sprintf("agent:%s:%s:%s:%s:thread:%s", agentID, channel, kind, chatID, threadID)
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

// BuildScopedSessionKey builds a session key using fixed scoping:
//   - Groups: per-sender (full key with channel + group ID)
//   - DMs: per-channel-peer (channel + peer user ID)
func BuildScopedSessionKey(agentID, channel string, kind PeerKind, chatID string) string {
	return BuildSessionKey(agentID, channel, kind, chatID)
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

// BuildHeartbeatSessionKey builds the session key for a heartbeat run.
//
//	isolated=true:  agent:{agentId}:heartbeat:{unix_ms}
//	isolated=false: agent:{agentId}:heartbeat
func BuildHeartbeatSessionKey(agentID string, isolated bool) string {
	if isolated {
		return fmt.Sprintf("agent:%s:heartbeat:%d", agentID, time.Now().UnixMilli())
	}
	return fmt.Sprintf("agent:%s:heartbeat", agentID)
}

// IsHeartbeatSession checks if a session key indicates a heartbeat session.
func IsHeartbeatSession(key string) bool {
	_, rest := ParseSessionKey(key)
	return strings.HasPrefix(rest, "heartbeat")
}

// BuildWSSessionKey builds the canonical WS session key for a web conversation.
//
//	agent:{agentId}:ws:direct:{conversationId}
func BuildWSSessionKey(agentID, conversationID string) string {
	return BuildSessionKey(agentID, "ws", PeerDirect, conversationID)
}

// IsWSSession checks if a session key is a WS session (new or legacy format).
func IsWSSession(key string) bool {
	_, rest := ParseSessionKey(key)
	return strings.HasPrefix(rest, "ws:") || strings.HasPrefix(rest, "ws-")
}

// CLISessionPrefix heads every interactive-CLI session key. It is NOT "agent:"
// on purpose — see the package doc.
const CLISessionPrefix = "cli:"

// BuildCLISessionKey builds the session key for one conversation with a
// connected coding CLI (internal/clisession).
//
//	cli:{connectionID}:{conversationID}
//
// connectionID is the tenant catalogue row id (store.CLIConnection.ID), so the
// key alone says which CLI to talk to on a resumed conversation — there is no
// agent to parse it out of. conversationID is a fresh UUID per chat, which is
// what separates two simultaneous conversations with the SAME connection.
//
// The tenant/user are deliberately NOT in the key: the session row carries
// user_id and every read path is tenant-scoped, exactly as for a ws: session.
func BuildCLISessionKey(connectionID, conversationID string) string {
	return fmt.Sprintf("%s%s:%s", CLISessionPrefix, connectionID, conversationID)
}

// ParseCLISessionKey extracts the connection and conversation ids from a CLI
// session key. ok is false for anything else, INCLUDING a key that starts with
// "cli:" but has an empty id — a caller must never open a session against an
// empty connection id.
func ParseCLISessionKey(key string) (connectionID, conversationID string, ok bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 || parts[0] != "cli" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// IsCLISession reports whether a session key CLAIMS to be an interactive-CLI
// conversation.
//
// It is deliberately a prefix test rather than a full parse: it is the routing
// predicate, and a malformed "cli:…" key must be answered by the CLI handler
// (which explains what is wrong) instead of silently falling through to the
// agent path and starting an agent run in a CLI-looking session. ParseCLISessionKey
// is the strict check that handler then applies.
func IsCLISession(key string) bool {
	return strings.HasPrefix(key, CLISessionPrefix)
}

// PeerKindFromGroup returns PeerGroup if isGroup is true, PeerDirect otherwise.
func PeerKindFromGroup(isGroup bool) PeerKind {
	if isGroup {
		return PeerGroup
	}
	return PeerDirect
}
