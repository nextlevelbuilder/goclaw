export const queryKeys = {
  apiKeys: {
    all: ["apiKeys"] as const,
  },
  providers: {
    all: ["providers"] as const,
    models: (providerId: string) => ["providers", providerId, "models"] as const,
  },
  agents: {
    all: ["agents"] as const,
    detail: (id: string) => ["agents", id] as const,
    files: (agentKey: string) => ["agents", agentKey, "files"] as const,
    links: (agentId: string) => ["agents", agentId, "links"] as const,
    instances: (agentId: string) => ["agents", agentId, "instances"] as const,
  },
  sessions: {
    all: ["sessions"] as const,
    list: (params: Record<string, unknown>) => ["sessions", params] as const,
  },
  traces: {
    all: ["traces"] as const,
    list: (params: Record<string, unknown>) => ["traces", params] as const,
  },
  customTools: {
    all: ["customTools"] as const,
    list: (params: Record<string, unknown>) => ["customTools", params] as const,
  },
  cliCredentials: {
    all: ["cliCredentials"] as const,
  },
  mcp: {
    all: ["mcp"] as const,
  },
  channels: {
    all: ["channels"] as const,
    list: (params: Record<string, unknown>) => ["channels", params] as const,
    detail: (id: string) => ["channels", "detail", id] as const,
  },
  contacts: {
    all: ["contacts"] as const,
    list: (params: Record<string, unknown>) => ["contacts", params] as const,
    resolve: (ids: string) => ["contacts", "resolve", ids] as const,
  },
  skills: {
    all: ["skills"] as const,
    agentGrants: (agentId: string) => ["skills", "agent", agentId] as const,
    runtimes: ["skills", "runtimes"] as const,
  },
  cron: {
    all: ["cron"] as const,
  },
  builtinTools: {
    all: ["builtinTools"] as const,
  },
  config: {
    all: ["config"] as const,
  },
  tts: {
    all: ["tts"] as const,
  },
  usage: {
    all: ["usage"] as const,
    records: (params: Record<string, unknown>) => ["usage", "records", params] as const,
  },
  delegations: {
    all: ["delegations"] as const,
    list: (params: Record<string, unknown>) => ["delegations", params] as const,
  },
  teams: {
    all: ["teams"] as const,
    detail: (id: string) => ["teams", id] as const,
  },
  memory: {
    all: ["memory"] as const,
    list: (params: Record<string, unknown>) => ["memory", params] as const,
  },
  kg: {
    all: ["kg"] as const,
    list: (params: Record<string, unknown>) => ["kg", params] as const,
    stats: (agentId: string, userId?: string) => ["kg", "stats", agentId, userId] as const,
    graph: (agentId: string, userId?: string) => ["kg", "graph", agentId, userId] as const,
  },
};
