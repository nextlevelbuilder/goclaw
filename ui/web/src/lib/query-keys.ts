export const queryKeys = {
  apiKeys: {
    all: ["apiKeys"] as const,
  },
  providers: {
    all: ["providers"] as const,
    models: (providerId: string) => ["providers", providerId, "models"] as const,
    chatgptOAuthStatuses: (providerKeys: string[]) => ["providers", "chatgpt-oauth-statuses", ...providerKeys] as const,
    chatgptOAuthQuotas: (providerNames: string[]) => ["providers", "chatgpt-oauth-quotas", ...providerNames] as const,
    codexPoolActivity: (providerId: string, limit: number) => ["providers", providerId, "codex-pool-activity", limit] as const,
  },
  agents: {
    all: ["agents"] as const,
    detail: (id: string) => ["agents", id] as const,
    files: (agentKey: string) => ["agents", agentKey, "files"] as const,
    links: (agentId: string) => ["agents", agentId, "links"] as const,
    instances: (agentId: string) => ["agents", agentId, "instances"] as const,
    codexPoolActivity: (agentId: string, limit: number) => ["agents", agentId, "codex-pool-activity", limit] as const,
    systemPromptPreview: (agentKey: string, mode: string) => ["agents", agentKey, "system-prompt-preview", mode] as const,
  },
  sessions: {
    all: ["sessions"] as const,
    list: (params: Record<string, unknown>) => ["sessions", params] as const,
  },
  traces: {
    all: ["traces"] as const,
    list: (params: Record<string, unknown>) => ["traces", params] as const,
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
    search: (params: Record<string, unknown>) => ["contacts", "search", params] as const,
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
  teams: {
    all: ["teams"] as const,
    detail: (id: string) => ["teams", id] as const,
  },
  memory: {
    all: ["memory"] as const,
    list: (params: Record<string, unknown>) => ["memory", params] as const,
  },
  v3Flags: {
    detail: (agentId: string) => ["v3-flags", agentId] as const,
  },
  orchestration: {
    detail: (agentId: string) => ["orchestration", agentId] as const,
  },
  evolution: {
    metrics: (agentId: string, params: Record<string, unknown>) => ["evolution", "metrics", agentId, params] as const,
    suggestions: (agentId: string, params: Record<string, unknown>) => ["evolution", "suggestions", agentId, params] as const,
  },
  packages: {
    all: ["packages"] as const,
    runtimes: ["packages", "runtimes"] as const,
  },
  tenantUsers: {
    all: ["tenantUsers"] as const,
  },
  users: {
    all: ["users"] as const,
    search: (params: Record<string, unknown>) => ["users", "search", params] as const,
  },
  tenants: {
    all: ["tenants"] as const,
    detail: (tenantId: string) => ["tenants", tenantId] as const,
    users: (tenantId: string) => ["tenants", tenantId, "users"] as const,
  },
  vault: {
    all: ["vault"] as const,
    docs: (params: Record<string, unknown>) => ["vault", "docs", params] as const,
    links: (agentId: string, docId: string) => ["vault", "links", agentId, docId] as const,
  },
  episodic: {
    all: ["episodic"] as const,
    list: (agentId: string, params: Record<string, unknown>) => ["episodic", agentId, params] as const,
  },
  kg: {
    all: ["kg"] as const,
    list: (params: Record<string, unknown>) => ["kg", params] as const,
    stats: (agentId: string, userId?: string) => ["kg", "stats", agentId, userId] as const,
    graph: (agentId: string, userId?: string) => ["kg", "graph", agentId, userId] as const,
    dedup: (agentId: string, userId?: string) => ["kg", "dedup", agentId, userId] as const,
  },
};
