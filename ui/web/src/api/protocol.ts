// Wire format types matching Go pkg/protocol/ exactly.

export const PROTOCOL_VERSION = 3;

// --- Frame types ---

export interface RequestFrame {
  type: "req";
  id: string;
  method: string;
  params?: Record<string, unknown>;
}

export interface ResponseFrame {
  type: "res";
  id: string;
  ok: boolean;
  payload?: unknown;
  error?: ErrorShape;
}

export interface EventFrame {
  type: "event";
  event: string;
  payload?: unknown;
  seq?: number;
  stateVersion?: { presence: number; health: number };
}

export interface ErrorShape {
  code: string;
  message: string;
  details?: unknown;
  retryable?: boolean;
  retryAfterMs?: number;
}

// --- RPC method names (from pkg/protocol/methods.go) ---

// Phase 1 - CRITICAL
export const Methods = {
  // System
  CONNECT: "connect",
  HEALTH: "health",
  STATUS: "status",

  // Agent
  AGENT: "agent",
  AGENT_WAIT: "agent.wait",
  AGENT_IDENTITY_GET: "agent.identity.get",

  // Chat
  CHAT_SEND: "chat.send",
  CHAT_HISTORY: "chat.history",
  CHAT_ABORT: "chat.abort",
  CHAT_INJECT: "chat.inject",

  // Agents management
  AGENTS_LIST: "agents.list",
  AGENTS_CREATE: "agents.create",
  AGENTS_UPDATE: "agents.update",
  AGENTS_DELETE: "agents.delete",
  AGENTS_FILES_LIST: "agents.files.list",
  AGENTS_FILES_GET: "agents.files.get",
  AGENTS_FILES_SET: "agents.files.set",

  // Config
  CONFIG_GET: "config.get",
  CONFIG_APPLY: "config.apply",
  CONFIG_PATCH: "config.patch",
  CONFIG_SCHEMA: "config.schema",

  // Sessions
  SESSIONS_LIST: "sessions.list",
  SESSIONS_PREVIEW: "sessions.preview",
  SESSIONS_PATCH: "sessions.patch",
  SESSIONS_DELETE: "sessions.delete",
  SESSIONS_RESET: "sessions.reset",

  // Phase 2 - NEEDED
  SKILLS_LIST: "skills.list",
  SKILLS_GET: "skills.get",
  SKILLS_UPDATE: "skills.update",

  CRON_LIST: "cron.list",
  CRON_CREATE: "cron.create",
  CRON_UPDATE: "cron.update",
  CRON_DELETE: "cron.delete",
  CRON_TOGGLE: "cron.toggle",
  CRON_STATUS: "cron.status",
  CRON_RUN: "cron.run",
  CRON_RUNS: "cron.runs",

  CHANNELS_LIST: "channels.list",
  CHANNELS_STATUS: "channels.status",
  CHANNELS_TOGGLE: "channels.toggle",

  // Channel instances (managed mode)
  CHANNEL_INSTANCES_LIST: "channels.instances.list",
  CHANNEL_INSTANCES_CREATE: "channels.instances.create",
  CHANNEL_INSTANCES_UPDATE: "channels.instances.update",
  CHANNEL_INSTANCES_DELETE: "channels.instances.delete",

  PAIRING_REQUEST: "device.pair.request",
  PAIRING_APPROVE: "device.pair.approve",
  PAIRING_DENY: "device.pair.deny",
  PAIRING_LIST: "device.pair.list",
  PAIRING_REVOKE: "device.pair.revoke",

  BROWSER_PAIRING_STATUS: "browser.pairing.status",

  APPROVALS_LIST: "exec.approval.list",
  APPROVALS_APPROVE: "exec.approval.approve",
  APPROVALS_DENY: "exec.approval.deny",

  USAGE_GET: "usage.get",
  USAGE_SUMMARY: "usage.summary",

  QUOTA_USAGE: "quota.usage",

  SEND: "send",

  // Agent links (delegation)
  AGENTS_LINKS_LIST: "agents.links.list",
  AGENTS_LINKS_CREATE: "agents.links.create",
  AGENTS_LINKS_UPDATE: "agents.links.update",
  AGENTS_LINKS_DELETE: "agents.links.delete",

  // Agent teams (managed mode)
  TEAMS_LIST: "teams.list",
  TEAMS_CREATE: "teams.create",
  TEAMS_GET: "teams.get",
  TEAMS_DELETE: "teams.delete",
  TEAMS_TASK_LIST: "teams.tasks.list",
  TEAMS_MEMBERS_ADD: "teams.members.add",
  TEAMS_MEMBERS_REMOVE: "teams.members.remove",
  TEAMS_UPDATE: "teams.update",
  TEAMS_KNOWN_USERS: "teams.known_users",

  // Delegation history (managed mode)
  DELEGATIONS_LIST: "delegations.list",
  DELEGATIONS_GET: "delegations.get",

  // Phase 3+ - NICE TO HAVE
  LOGS_TAIL: "logs.tail",
  HEARTBEAT: "heartbeat",
} as const;

// --- Event names (from pkg/protocol/events.go) ---

export const Events = {
  AGENT: "agent",
  CHAT: "chat",
  HEALTH: "health",
  CRON: "cron",
  EXEC_APPROVAL_REQUESTED: "exec.approval.requested",
  EXEC_APPROVAL_RESOLVED: "exec.approval.resolved",
  PRESENCE: "presence",
  TICK: "tick",
  SHUTDOWN: "shutdown",
  NODE_PAIR_REQUESTED: "node.pair.requested",
  NODE_PAIR_RESOLVED: "node.pair.resolved",
  DEVICE_PAIR_REQUESTED: "device.pair.requested",
  DEVICE_PAIR_RESOLVED: "device.pair.resolved",
  VOICEWAKE_CHANGED: "voicewake.changed",
  CONNECT_CHALLENGE: "connect.challenge",
  HEARTBEAT: "heartbeat",
  TALK_MODE: "talk.mode",
  HANDOFF: "handoff",
} as const;

// Agent event subtypes (in payload.type)
export const AgentEventTypes = {
  RUN_STARTED: "run.started",
  RUN_COMPLETED: "run.completed",
  RUN_FAILED: "run.failed",
  TOOL_CALL: "tool.call",
  TOOL_RESULT: "tool.result",
  BLOCK_REPLY: "block.reply",
} as const;

// Chat event subtypes (in payload.type)
export const ChatEventTypes = {
  CHUNK: "chunk",
  MESSAGE: "message",
  THINKING: "thinking",
} as const;
