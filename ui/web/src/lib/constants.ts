export const ROUTES = {
  LOGIN: "/login",
  OVERVIEW: "/overview",
  CHAT: "/chat",
  CHAT_SESSION: "/chat/:sessionKey",
  AGENTS: "/agents",
  AGENT_DETAIL: "/agents/:id",
  SESSIONS: "/sessions",
  SESSION_DETAIL: "/sessions/:key",
  SKILLS: "/skills",
  SKILL_DETAIL: "/skills/:id",
  CRON: "/cron",
  CRON_DETAIL: "/cron/:id",
  CONFIG: "/config",
  TRACES: "/traces",
  TRACE_DETAIL: "/traces/:id",
  EVENTS: "/events",
  DELEGATIONS: "/delegations",
  USAGE: "/usage",
  CHANNELS: "/channels",
  CHANNEL_DETAIL: "/channels/:id",
  CONTACTS: "/contacts",
  APPROVALS: "/approvals",
  NODES: "/nodes",
  LOGS: "/logs",
  PROVIDERS: "/providers",
  TEAMS: "/teams",
  TEAM_DETAIL: "/teams/:id",
  CUSTOM_TOOLS: "/custom-tools",
  BUILTIN_TOOLS: "/builtin-tools",
  MCP: "/mcp",
  TTS: "/tts",
  STORAGE: "/storage",
  PENDING_MESSAGES: "/pending-messages",
  MEMORY: "/memory",
  KNOWLEDGE_GRAPH: "/knowledge-graph",
  ACTIVITY: "/activity",
  SETUP: "/setup",
} as const;

export const LOCAL_STORAGE_KEYS = {
  TOKEN: "goclaw:token",
  USER_ID: "goclaw:userId",
  SENDER_ID: "goclaw:senderID",
  THEME: "goclaw:theme",
  SIDEBAR_COLLAPSED: "goclaw:sidebarCollapsed",
  LANGUAGE: "goclaw:language",
} as const;

export const SUPPORTED_LANGUAGES = ["en", "vi", "zh"] as const;
export type Language = (typeof SUPPORTED_LANGUAGES)[number];

export const LANGUAGE_LABELS: Record<Language, string> = {
  en: "English",
  vi: "Tiếng Việt",
  zh: "中文",
};
